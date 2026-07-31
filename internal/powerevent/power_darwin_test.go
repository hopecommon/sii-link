//go:build darwin

package powerevent

import (
	"context"
	"testing"
	"time"
)

func TestWatchRegistersAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	watcher, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch system power events: %v", err)
	}
	cancel()

	select {
	case err, ok := <-watcher.Done:
		if ok && err != nil {
			t.Fatalf("stop system power watcher: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("system power watcher did not stop after cancellation")
	}
}

func TestHandlePowerMessageEmitsWakeEvent(t *testing.T) {
	events := make(chan struct{}, 1)
	allowCalls := 0
	handlePowerMessage(kIOMessageSystemHasPoweredOn, 0, events, func(uintptr) {
		allowCalls++
	})

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("wake message did not emit an event")
	}
	if allowCalls != 0 {
		t.Fatalf("power-change acknowledgements = %d, want 0", allowCalls)
	}
}

func TestHandlePowerMessageAcknowledgesSleep(t *testing.T) {
	events := make(chan struct{}, 1)
	var arguments []uintptr
	allow := func(argument uintptr) {
		arguments = append(arguments, argument)
	}

	handlePowerMessage(kIOMessageCanSystemSleep, 11, events, allow)
	handlePowerMessage(kIOMessageSystemWillSleep, 22, events, allow)

	if len(arguments) != 2 || arguments[0] != 11 || arguments[1] != 22 {
		t.Fatalf("power-change acknowledgements = %v, want [11 22]", arguments)
	}
	select {
	case <-events:
		t.Fatal("sleep message unexpectedly emitted a wake event")
	default:
	}
}

func TestHandlePowerMessageCoalescesWakeEvents(t *testing.T) {
	events := make(chan struct{}, 1)
	handlePowerMessage(kIOMessageSystemHasPoweredOn, 0, events, func(uintptr) {})
	handlePowerMessage(kIOMessageSystemHasPoweredOn, 0, events, func(uintptr) {})

	if got := len(events); got != 1 {
		t.Fatalf("queued wake events = %d, want 1", got)
	}
}
