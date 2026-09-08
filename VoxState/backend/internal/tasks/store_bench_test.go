// M9 benchmarking: task creation (version binding) and cancellation
// latency, isolated from any voice/agent/network overhead.
package tasks

import (
	"testing"

	"voxstate/backend/internal/state"
)

func BenchmarkCreateTask(b *testing.B) {
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		b.Fatalf("CreateMachine() error = %v", err)
	}
	ts := NewStore(ss, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"}); err != nil {
			b.Fatalf("CreateTask() error = %v", err)
		}
	}
}

// BenchmarkCancelTask_Pending measures CancelTask's own latency (the
// synchronous status-flip under lock, before any cancel() callback
// fires) for a task that never started — the cheapest, most common case.
func BenchmarkCancelTask_Pending(b *testing.B) {
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		b.Fatalf("CreateMachine() error = %v", err)
	}
	ts := NewStore(ss, nil)

	ids := make([]string, b.N)
	for i := range ids {
		task, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})
		if err != nil {
			b.Fatalf("CreateTask() error = %v", err)
		}
		ids[i] = task.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ts.CancelTask(ids[i]); err != nil {
			b.Fatalf("CancelTask() error = %v", err)
		}
	}
}
