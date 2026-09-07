package policy

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"voxstate/backend/internal/tasks"
)

// SimulateToolResult builds a ToolResult for a task, copying the task's
// BoundVersion verbatim — never the machine's current version, which
// would destroy the exact stale-result detection this package exists to
// provide. It performs no real diagnostic logic: it exists only so M4's
// stale-result rejection is demonstrable and testable without real tools,
// which don't exist until a later milestone's tool orchestrator produces
// real ToolResults through this same shape.
func SimulateToolResult(task tasks.DiagnosticTask, payload map[string]any) ToolResult {
	return ToolResult{
		ID:           "result-" + randomHex(6),
		TaskID:       task.ID,
		MachineID:    task.MachineID,
		BoundVersion: task.BoundVersion,
		Payload:      payload,
		CreatedAt:    time.Now().UTC(),
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
