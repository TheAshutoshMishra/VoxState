// M9 benchmarking: the actual cost of the version-fencing decision
// itself — a single map lookup plus an integer comparison under a mutex
// (see Evaluate) — isolated from any tool/network/goroutine overhead.
package policy

import (
	"testing"

	"voxstate/backend/internal/state"
)

func BenchmarkEvaluate_Accepted(b *testing.B) {
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		b.Fatalf("CreateMachine() error = %v", err)
	}
	ev := NewEvaluator(ss)
	result := ToolResult{ID: "r1", TaskID: "t1", MachineID: "machine-17", BoundVersion: 1, Payload: map[string]any{"vibration": "NORMAL"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ev.Evaluate(result); err != nil {
			b.Fatalf("Evaluate() error = %v", err)
		}
	}
}

func BenchmarkEvaluate_RejectedStale(b *testing.B) {
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		b.Fatalf("CreateMachine() error = %v", err)
	}
	if _, _, _, err := ss.ChangeState("machine-17", state.StateChangeInput{Attributes: map[string]string{"probe": "1"}}); err != nil {
		b.Fatalf("ChangeState() error = %v", err)
	}
	ev := NewEvaluator(ss)
	result := ToolResult{ID: "r1", TaskID: "t1", MachineID: "machine-17", BoundVersion: 1, Payload: map[string]any{"vibration": "stale"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ev.Evaluate(result); err != nil {
			b.Fatalf("Evaluate() error = %v", err)
		}
	}
}
