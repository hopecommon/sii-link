package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorStopsAfterConsecutiveFailures(t *testing.T) {
	checkErr := errors.New("tunnel unavailable")
	var checks atomic.Int32
	monitor := Monitor{
		Interval:         time.Millisecond,
		FailureThreshold: 3,
		Check: func(context.Context) error {
			checks.Add(1)
			return checkErr
		},
	}

	err := monitor.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "3 consecutive") || !errors.Is(err, checkErr) {
		t.Fatalf("monitor error = %v, want threshold error wrapping check failure", err)
	}
	if checks.Load() != 3 {
		t.Fatalf("checks = %d, want 3", checks.Load())
	}
}

func TestMonitorSuccessResetsFailureCount(t *testing.T) {
	checkErr := errors.New("temporary failure")
	results := []error{checkErr, checkErr, nil, checkErr, checkErr, checkErr}
	var index atomic.Int32
	monitor := Monitor{
		Interval:         time.Millisecond,
		FailureThreshold: 3,
		Check: func(context.Context) error {
			current := int(index.Add(1)) - 1
			return results[current]
		},
	}

	err := monitor.Run(context.Background())
	if err == nil || index.Load() != 6 {
		t.Fatalf("monitor stopped after %d checks with %v, want reset then stop after 6 checks", index.Load(), err)
	}
}

func TestMonitorCancellationIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	monitor := Monitor{
		Interval: time.Hour,
		Check: func(context.Context) error {
			cancel()
			return nil
		},
	}
	if err := monitor.Run(ctx); err != nil {
		t.Fatalf("monitor cancellation error = %v, want nil", err)
	}
}

func TestMonitorRejectsInvalidConfiguration(t *testing.T) {
	tests := []Monitor{
		{Interval: 0, Check: func(context.Context) error { return nil }},
		{Interval: time.Second},
		{Interval: time.Second, FailureThreshold: -1, Check: func(context.Context) error { return nil }},
		{Interval: time.Second, FailureRetryInterval: -1, Check: func(context.Context) error { return nil }},
	}
	for _, monitor := range tests {
		if err := monitor.Run(context.Background()); err == nil {
			t.Fatal("invalid monitor configuration was accepted")
		}
	}
}

func TestMonitorRunsCheckOnExternalTrigger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trigger := make(chan struct{}, 1)
	checked := make(chan int, 2)
	var checks atomic.Int32
	monitor := Monitor{
		Interval: time.Hour,
		Trigger:  trigger,
		Check: func(context.Context) error {
			count := int(checks.Add(1))
			checked <- count
			return nil
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- monitor.Run(ctx)
	}()

	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("initial monitor check did not run")
	}
	trigger <- struct{}{}
	select {
	case count := <-checked:
		if count != 2 {
			t.Fatalf("triggered check count = %d, want 2", count)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("external trigger did not run an immediate check")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("monitor cancellation error: %v", err)
	}
}

func TestMonitorRetriesFailuresWithoutWaitingForIdleInterval(t *testing.T) {
	trigger := make(chan struct{}, 1)
	var checks atomic.Int32
	monitor := Monitor{
		Interval:             time.Hour,
		FailureRetryInterval: time.Millisecond,
		FailureThreshold:     3,
		Trigger:              trigger,
		Check: func(context.Context) error {
			if checks.Add(1) == 1 {
				return nil
			}
			return errors.New("tunnel unavailable")
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- monitor.Run(context.Background())
	}()

	deadline := time.After(time.Second)
	for checks.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("initial monitor check did not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	started := time.Now()
	trigger <- struct{}{}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "3 consecutive") {
			t.Fatalf("monitor error = %v, want failure threshold error", err)
		}
		if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
			t.Fatalf("failure retries took %s, want recovery cadence instead of idle interval", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor waited for the idle interval after a triggered failure")
	}
}

func TestMonitorDisablesClosedTrigger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	trigger := make(chan struct{})
	var checks atomic.Int32
	monitor := Monitor{
		Interval: time.Hour,
		Trigger:  trigger,
		Check: func(context.Context) error {
			checks.Add(1)
			return nil
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- monitor.Run(ctx)
	}()

	deadline := time.After(time.Second)
	for checks.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("initial monitor check did not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(trigger)
	time.Sleep(20 * time.Millisecond)
	if got := checks.Load(); got != 1 {
		t.Fatalf("checks after trigger closed = %d, want 1", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("monitor cancellation error: %v", err)
	}
}

func TestMonitorDoesNotRapidlyRetryWhenFailureThresholdIsDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstCheck := make(chan struct{})
	var checks atomic.Int32
	monitor := Monitor{
		Interval:             time.Hour,
		FailureRetryInterval: time.Millisecond,
		FailureThreshold:     0,
		Check: func(context.Context) error {
			if checks.Add(1) == 1 {
				close(firstCheck)
			}
			return errors.New("tunnel unavailable")
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- monitor.Run(ctx)
	}()

	select {
	case <-firstCheck:
	case <-time.After(time.Second):
		t.Fatal("initial monitor check did not run")
	}
	time.Sleep(20 * time.Millisecond)
	if got := checks.Load(); got != 1 {
		t.Fatalf("checks with disabled threshold = %d, want 1 before idle interval", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("monitor cancellation error: %v", err)
	}
}
