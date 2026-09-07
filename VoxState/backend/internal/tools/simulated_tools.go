package tools

import (
	"context"
	"time"
)

// Tool name constants. These are the strings a Planner (internal/agent)
// selects by — keeping them here means the Agent and the tools it
// dispatches to can't drift out of sync on spelling.
const (
	NameVibrationScan   = "vibration_scan"
	NameTemperatureScan = "temperature_scan"
)

// These deterministic tools are NOT a simulation of a real industrial
// diagnostic system — they exist only to prove the Agent/Tool/Task/Policy
// architecture end to end (M5). Their payloads are fixed constants, never
// randomized, so tests are fully deterministic. The only configurable
// behavior is an artificial delay, which exists solely so tests (and the
// HTTP demo) can control the timing of "tool finishes" relative to a
// machine state change — exactly the M3/M4 pattern already established by
// tasks.SimulatedWork.

type vibrationScan struct {
	delay time.Duration
}

// NewVibrationScan returns a deterministic Tool that reports a fixed
// vibration reading. delay, if > 0, models work taking time before
// returning; it is fully cancellable via ctx.
func NewVibrationScan(delay time.Duration) Tool {
	return vibrationScan{delay: delay}
}

func (t vibrationScan) Name() string { return NameVibrationScan }

func (t vibrationScan) Run(ctx context.Context, _ RunInput) (RunOutput, error) {
	if err := simulateDelay(ctx, t.delay); err != nil {
		return RunOutput{}, err
	}
	return RunOutput{
		Payload:  map[string]any{"vibration": "NORMAL"},
		Duration: t.delay,
	}, nil
}

type temperatureScan struct {
	delay time.Duration
}

// NewTemperatureScan returns a deterministic Tool that reports a fixed
// temperature reading. Same delay/cancellation contract as
// NewVibrationScan.
func NewTemperatureScan(delay time.Duration) Tool {
	return temperatureScan{delay: delay}
}

func (t temperatureScan) Name() string { return NameTemperatureScan }

func (t temperatureScan) Run(ctx context.Context, _ RunInput) (RunOutput, error) {
	if err := simulateDelay(ctx, t.delay); err != nil {
		return RunOutput{}, err
	}
	return RunOutput{
		Payload:  map[string]any{"temperature_f": 72},
		Duration: t.delay,
	}, nil
}

// simulateDelay waits for d, checking ctx.Done() in small increments so
// cancellation is observed promptly rather than after the full delay —
// the same pattern tasks.SimulatedWork (M3) uses.
func simulateDelay(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	const tick = 20 * time.Millisecond

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
