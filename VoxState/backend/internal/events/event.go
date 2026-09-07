// Package events defines the domain event catalog described in
// docs/EVENT_MODEL.md. As of M2 this package only defines the shape of an
// event and a constructor — there is no store, dispatcher, or persistence
// yet. Other packages (state, and later tasks/tools/policy) construct
// events.Event values to describe what happened; append-only persistence
// is a later milestone.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Type identifies which kind of event occurred. Only the event types
// actually produced as of M2 are defined here; the remaining catalog
// entries in docs/EVENT_MODEL.md are added by the packages that produce
// them (tasks, tools, policy) as those milestones are implemented.
type Type string

const (
	// TypeMachineCreated fires when a machine is registered, establishing
	// StateVersion(1).
	TypeMachineCreated Type = "MachineCreated"

	// TypeMachineStateChanged fires whenever a state change produces a new
	// StateVersion. ResultingStateVersion is always set on this type.
	TypeMachineStateChanged Type = "MachineStateChanged"

	// TypeTechnicianReported fires when a state change is attributed to a
	// human report rather than an automated/system source. It accompanies
	// (does not replace) a TypeMachineStateChanged event.
	TypeTechnicianReported Type = "TechnicianReported"

	// TypeDiagnosticStarted fires when a DiagnosticTask is created and
	// bound to the machine's current StateVersion (M3).
	TypeDiagnosticStarted Type = "DiagnosticStarted"

	// TypeDiagnosticCancelled fires when a DiagnosticTask is cancelled
	// before reaching a COMPLETED state (M3).
	TypeDiagnosticCancelled Type = "DiagnosticCancelled"

	// TypeDiagnosticCompleted fires when a DiagnosticTask's work finishes
	// without having been cancelled (M3). Whether the resulting output is
	// stale is a policy-layer concern introduced in M4, not this event.
	TypeDiagnosticCompleted Type = "DiagnosticCompleted"

	// TypeToolResultRejected fires when the Policy layer determines a
	// ToolResult's BoundVersion no longer matches the machine's current
	// StateVersion and refuses to let it influence anything downstream
	// (M4). This is the mechanical enforcement point for the stale-result
	// rule described in CLAUDE.md and docs/ARCHITECTURE.md.
	TypeToolResultRejected Type = "ToolResultRejected"

	// TypeUserInterrupted fires when a VoiceSession detects the user
	// speaking over an in-progress turn (barge-in) and cancels it (M7).
	// Named and described in docs/EVENT_MODEL.md ahead of M7's
	// implementation; internal/voice.VoiceSession is its only producer.
	TypeUserInterrupted Type = "UserInterrupted"

	// TypeResponseInvalidated fires alongside TypeUserInterrupted: the
	// interrupted turn's response — whatever state it was in (still being
	// planned, mid-tool, mid-synthesis, or already queued for playback) —
	// is no longer authoritative and must never reach the user (M7).
	TypeResponseInvalidated Type = "ResponseInvalidated"
)

// Event is an in-memory domain value describing something that happened.
// It mirrors the common shape defined in docs/EVENT_MODEL.md. SessionID is
// left unset until VoiceSession exists (M6+).
type Event struct {
	ID                    string
	Type                  Type
	MachineID             string
	SessionID             string
	Payload               map[string]any
	ResultingStateVersion *int
	CreatedAt             time.Time
}

// New constructs an Event of the given type with a fresh ID and the
// current timestamp. ResultingStateVersion is left nil; callers set it
// explicitly when the event caused a state version bump.
func New(t Type, machineID string, payload map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      t,
		MachineID: machineID,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
}

func newID() string {
	b := make([]byte, 8)
	// crypto/rand.Read on the standard reader does not fail in practice;
	// or if it did, a low-entropy fallback ID is still preferable to a
	// panic for an in-memory M2 identifier.
	_, _ = rand.Read(b)
	return "evt-" + hex.EncodeToString(b)
}
