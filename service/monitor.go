package service

import (
	"context"
	"fmt"
	"time"
)

type Monitor struct {
	Interval             time.Duration
	FailureRetryInterval time.Duration
	FailureThreshold     int
	Trigger              <-chan struct{}
	Check                func(context.Context) error
	Observe              func(error)
}

func (m Monitor) Run(ctx context.Context) error {
	if m.Interval <= 0 {
		return fmt.Errorf("monitor interval must be positive")
	}
	if m.FailureThreshold < 0 {
		return fmt.Errorf("monitor failure threshold must not be negative")
	}
	if m.FailureRetryInterval < 0 {
		return fmt.Errorf("monitor failure retry interval must not be negative")
	}
	if m.Check == nil {
		return fmt.Errorf("monitor check is required")
	}

	consecutiveFailures := 0
	trigger := m.Trigger
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		case _, ok := <-trigger:
			if !ok {
				trigger = nil
				continue
			}
			if consecutiveFailures > 0 {
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		err := m.Check(ctx)
		if m.Observe != nil {
			m.Observe(err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
			if m.FailureThreshold > 0 && consecutiveFailures >= m.FailureThreshold {
				return fmt.Errorf("health check failed %d consecutive times: %w", consecutiveFailures, err)
			}
		}
		nextInterval := m.Interval
		if consecutiveFailures > 0 && m.FailureThreshold > 0 && m.FailureRetryInterval > 0 {
			nextInterval = m.FailureRetryInterval
		}
		timer.Reset(nextInterval)
	}
}
