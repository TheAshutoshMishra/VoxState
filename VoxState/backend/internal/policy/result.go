package policy

import "time"

// ToolResult is the output of a completed DiagnosticTask, carrying the
// state version it was produced against. This corresponds to
// docs/DOMAIN_MODEL.md's ToolResult entity; BoundVersion here is that
// entity's produced_for_state_version field, renamed to match the
// terminology tasks.DiagnosticTask.BoundVersion already established —
// a ToolResult's version is always copied verbatim from the task that
// produced it (see SimulateToolResult), never read from current state.
//
// Deliberately absent: Accepted/RejectionReason fields. DOMAIN_MODEL.md
// describes those as "set by the Policy layer", which is safer modeled
// in Go as the return value of Evaluate (a Decision) than as mutable
// fields on this struct. A mutable bool a caller could set directly would
// make it trivial to bypass the policy check entirely — exactly the
// failure mode this milestone exists to prevent.
type ToolResult struct {
	ID           string
	TaskID       string
	MachineID    string
	BoundVersion int
	Payload      map[string]any
	CreatedAt    time.Time
}
