package tasks

import (
	"context"
	"time"
)

// SimulatedWork returns a Work implementation that performs no real
// diagnostic logic. It exists solely to demonstrate the Work/StartTask
// cancellation abstraction end-to-end (including over the HTTP API) until
// a real tool orchestrator (M5+) supplies actual diagnostic Work
// implementations through the same interface.
//
// It "runs" for duration d, checking ctx.Done() every tick so a
// cancellation is observed promptly rather than after the full duration.
func SimulatedWork(d time.Duration) Work {
	const tick = 100 * time.Millisecond

	return func(ctx context.Context) error {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			wait := tick
			if remaining := time.Until(deadline); remaining < wait {
				wait = remaining
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		return nil
	}
}
