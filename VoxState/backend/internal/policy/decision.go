package policy

import "voxstate/backend/internal/events"

// Outcome is the result of a policy evaluation.
type Outcome string

const (
	Accepted Outcome = "ACCEPTED"
	Rejected Outcome = "REJECTED"
)

// Decision is the explicit, structured result of Evaluate. Callers must
// inspect Outcome (or call IsAccepted) rather than being handed a
// ToolResult and left to guess whether it's safe to use — that guesswork
// is exactly what allows stale results to leak into a response. There is
// deliberately no way to obtain a "probably fine" result out of Evaluate
// without going through this type.
type Decision struct {
	Outcome Outcome
	Result  ToolResult
	// CurrentVersion is the machine's state version at the moment of
	// evaluation — see the consistency-boundary note on Evaluate.
	CurrentVersion int
	// Reason is empty when Outcome == Accepted.
	Reason string
	// Event is the zero events.Event when Outcome == Accepted (M4 does
	// not define an "accepted" event in the catalog); it is a
	// TypeToolResultRejected event when Outcome == Rejected.
	Event events.Event
}

// IsAccepted reports whether the result is safe to consume.
func (d Decision) IsAccepted() bool {
	return d.Outcome == Accepted
}
