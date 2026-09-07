package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVibrationScan_DeterministicPayload(t *testing.T) {
	tool := NewVibrationScan(0)
	if tool.Name() != NameVibrationScan {
		t.Errorf("Name() = %q, want %q", tool.Name(), NameVibrationScan)
	}

	out, err := tool.Run(context.Background(), RunInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Payload["vibration"] != "NORMAL" {
		t.Errorf("Payload[vibration] = %v, want %q", out.Payload["vibration"], "NORMAL")
	}
}

func TestTemperatureScan_DeterministicPayload(t *testing.T) {
	tool := NewTemperatureScan(0)
	if tool.Name() != NameTemperatureScan {
		t.Errorf("Name() = %q, want %q", tool.Name(), NameTemperatureScan)
	}

	out, err := tool.Run(context.Background(), RunInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Payload["temperature_f"] != 72 {
		t.Errorf("Payload[temperature_f] = %v, want 72", out.Payload["temperature_f"])
	}
}

func TestTool_RunIsNotRandom(t *testing.T) {
	tool := NewVibrationScan(0)
	first, err := tool.Run(context.Background(), RunInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	second, err := tool.Run(context.Background(), RunInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if first.Payload["vibration"] != second.Payload["vibration"] {
		t.Errorf("two Run() calls returned different payloads: %v vs %v", first.Payload, second.Payload)
	}
}

func TestTool_RunRespectsDelay(t *testing.T) {
	tool := NewVibrationScan(30 * time.Millisecond)
	start := time.Now()
	if _, err := tool.Run(context.Background(), RunInput{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("Run() returned after %v, want roughly >= the configured delay", elapsed)
	}
}

func TestTool_RunCancelledPropagatesContext(t *testing.T) {
	tool := NewVibrationScan(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := tool.Run(ctx, RunInput{})
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("Run() took %v to observe cancellation, want well under the 5s delay", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

func TestRegistry_GetKnownAndUnknown(t *testing.T) {
	registry := NewRegistry(NewVibrationScan(0), NewTemperatureScan(0))

	tool, ok := registry.Get(NameVibrationScan)
	if !ok {
		t.Fatal("Get(NameVibrationScan) ok = false, want true")
	}
	if tool.Name() != NameVibrationScan {
		t.Errorf("Get(NameVibrationScan).Name() = %q, want %q", tool.Name(), NameVibrationScan)
	}

	if _, ok := registry.Get("does_not_exist"); ok {
		t.Error("Get(unknown) ok = true, want false")
	}
}
