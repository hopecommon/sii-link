//go:build darwin

package powerevent

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

const (
	iokitPath          = "/System/Library/Frameworks/IOKit.framework/IOKit"
	coreFoundationPath = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"

	kIOMessageCanSystemSleep     uint32 = 0xe0000270
	kIOMessageSystemWillSleep    uint32 = 0xe0000280
	kIOMessageSystemHasPoweredOn uint32 = 0xe0000300
	kCFStringEncodingUTF8        uint32 = 0x08000100
)

type powerFunctions struct {
	ioRegisterForSystemPower       func(uintptr, *uintptr, uintptr, *uint32) uint32
	ioAllowPowerChange             func(uint32, uintptr) int32
	ioDeregisterForSystemPower     func(*uint32) int32
	ioNotificationPortGetRunSource func(uintptr) uintptr
	ioNotificationPortDestroy      func(uintptr)
	ioServiceClose                 func(uint32) int32
	cfRunLoopGetCurrent            func() uintptr
	cfRunLoopAddSource             func(uintptr, uintptr, uintptr)
	cfRunLoopRun                   func()
	cfRunLoopStop                  func(uintptr)
	cfStringCreateWithCString      func(uintptr, string, uint32) uintptr
	cfRelease                      func(uintptr)
	defaultRunLoopMode             uintptr
}

type watchStart struct {
	watcher Watcher
	err     error
}

func Watch(ctx context.Context) (Watcher, error) {
	if err := ctx.Err(); err != nil {
		return Watcher{}, err
	}
	started := make(chan watchStart, 1)
	go runWatcher(ctx, started)
	result := <-started
	return result.watcher, result.err
}

func runWatcher(ctx context.Context, started chan<- watchStart) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	functions, closeLibraries, err := loadPowerFunctions()
	if err != nil {
		started <- watchStart{err: err}
		return
	}

	events := make(chan struct{}, 1)
	done := make(chan error, 1)
	var runLoop uintptr
	var reportOnce sync.Once
	reportFailure := func(err error) {
		reportOnce.Do(func() {
			done <- err
			if runLoop != 0 {
				functions.cfRunLoopStop(runLoop)
			}
		})
	}

	var rootPort uint32
	callback := purego.NewCallback(func(_ uintptr, _ uint32, messageType uint32, messageArgument uintptr) {
		handlePowerMessage(messageType, messageArgument, events, func(argument uintptr) {
			if result := functions.ioAllowPowerChange(rootPort, argument); result != 0 {
				reportFailure(fmt.Errorf("acknowledge macOS power change: IOReturn %#x", uint32(result)))
			}
		})
	})
	var notificationPort uintptr
	var notifier uint32
	rootPort = functions.ioRegisterForSystemPower(0, &notificationPort, callback, &notifier)
	cleanup := func() error {
		var cleanupErrors []error
		if notifier != 0 {
			if result := functions.ioDeregisterForSystemPower(&notifier); result != 0 {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("deregister macOS power events: IOReturn %#x", uint32(result)))
			}
		}
		if rootPort != 0 {
			if result := functions.ioServiceClose(rootPort); result != 0 {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("close macOS power service: IOReturn %#x", uint32(result)))
			}
		}
		if notificationPort != 0 {
			functions.ioNotificationPortDestroy(notificationPort)
		}
		return errors.Join(cleanupErrors...)
	}
	if rootPort == 0 || notificationPort == 0 || notifier == 0 {
		started <- watchStart{err: errors.Join(fmt.Errorf("register for macOS system power events"), cleanup(), closeLibraries())}
		return
	}

	runLoopSource := functions.ioNotificationPortGetRunSource(notificationPort)
	if runLoopSource == 0 {
		cleanupErr := errors.Join(cleanup(), closeLibraries())
		started <- watchStart{err: errors.Join(fmt.Errorf("get macOS power-event run-loop source"), cleanupErr)}
		return
	}
	runLoop = functions.cfRunLoopGetCurrent()
	if runLoop == 0 {
		cleanupErr := errors.Join(cleanup(), closeLibraries())
		started <- watchStart{err: errors.Join(fmt.Errorf("get macOS power-event run loop"), cleanupErr)}
		return
	}
	functions.cfRunLoopAddSource(runLoop, runLoopSource, functions.defaultRunLoopMode)
	started <- watchStart{watcher: Watcher{Events: events, Done: done}}

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			functions.cfRunLoopStop(runLoop)
		case <-stopped:
		}
	}()
	functions.cfRunLoopRun()
	close(stopped)
	runtime.KeepAlive(callback)
	cleanupErr := errors.Join(cleanup(), closeLibraries())
	if ctx.Err() == nil {
		reportFailure(errors.Join(fmt.Errorf("macOS power-event run loop stopped unexpectedly"), cleanupErr))
	} else if cleanupErr != nil {
		reportFailure(cleanupErr)
	}
	close(events)
	close(done)
}

func handlePowerMessage(messageType uint32, messageArgument uintptr, events chan<- struct{}, allowPowerChange func(uintptr)) {
	switch messageType {
	case kIOMessageCanSystemSleep, kIOMessageSystemWillSleep:
		allowPowerChange(messageArgument)
	case kIOMessageSystemHasPoweredOn:
		select {
		case events <- struct{}{}:
		default:
		}
	}
}

func loadPowerFunctions() (powerFunctions, func() error, error) {
	iokit, err := purego.Dlopen(iokitPath, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return powerFunctions{}, func() error { return nil }, fmt.Errorf("load IOKit: %w", err)
	}
	coreFoundation, err := purego.Dlopen(coreFoundationPath, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		closeErr := purego.Dlclose(iokit)
		return powerFunctions{}, func() error { return nil }, errors.Join(fmt.Errorf("load CoreFoundation: %w", err), closeErr)
	}
	var defaultRunLoopMode uintptr
	var functions powerFunctions
	closeLibraries := func() error {
		if defaultRunLoopMode != 0 {
			functions.cfRelease(defaultRunLoopMode)
			defaultRunLoopMode = 0
		}
		return errors.Join(purego.Dlclose(coreFoundation), purego.Dlclose(iokit))
	}

	registrations := []struct {
		handle uintptr
		name   string
		target any
	}{
		{iokit, "IORegisterForSystemPower", &functions.ioRegisterForSystemPower},
		{iokit, "IOAllowPowerChange", &functions.ioAllowPowerChange},
		{iokit, "IODeregisterForSystemPower", &functions.ioDeregisterForSystemPower},
		{iokit, "IONotificationPortGetRunLoopSource", &functions.ioNotificationPortGetRunSource},
		{iokit, "IONotificationPortDestroy", &functions.ioNotificationPortDestroy},
		{iokit, "IOServiceClose", &functions.ioServiceClose},
		{coreFoundation, "CFRunLoopGetCurrent", &functions.cfRunLoopGetCurrent},
		{coreFoundation, "CFRunLoopAddSource", &functions.cfRunLoopAddSource},
		{coreFoundation, "CFRunLoopRun", &functions.cfRunLoopRun},
		{coreFoundation, "CFRunLoopStop", &functions.cfRunLoopStop},
		{coreFoundation, "CFStringCreateWithCString", &functions.cfStringCreateWithCString},
		{coreFoundation, "CFRelease", &functions.cfRelease},
	}
	for _, registration := range registrations {
		if err := registerFunction(registration.handle, registration.name, registration.target); err != nil {
			return powerFunctions{}, func() error { return nil }, errors.Join(err, closeLibraries())
		}
	}
	defaultRunLoopMode = functions.cfStringCreateWithCString(0, "kCFRunLoopDefaultMode", kCFStringEncodingUTF8)
	if defaultRunLoopMode == 0 {
		return powerFunctions{}, func() error { return nil }, errors.Join(fmt.Errorf("create kCFRunLoopDefaultMode"), closeLibraries())
	}
	functions.defaultRunLoopMode = defaultRunLoopMode
	return functions, closeLibraries, nil
}

func registerFunction(handle uintptr, name string, target any) error {
	symbol, err := purego.Dlsym(handle, name)
	if err != nil {
		return fmt.Errorf("load %s: %w", name, err)
	}
	purego.RegisterFunc(target, symbol)
	return nil
}
