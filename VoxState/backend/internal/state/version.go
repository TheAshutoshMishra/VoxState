package state

import "time"

// StateVersion identifies exactly which snapshot of a machine's state was
// current at a point in time. It is the mechanism the whole stale-result
// system (M4+) hinges on: a task binds to a StateVersion.Version, and that
// binding stays meaningful forever even after the machine moves on.
type StateVersion struct {
	MachineID       string
	Version         int // monotonically increasing per machine, starts at 1
	CreatedAt       time.Time
	CausedByEventID string // ID of the events.Event that produced this version
}

// MachineState is an immutable snapshot of what is true about a machine at
// a given StateVersion. Once appended to a machine's history it is never
// modified — a change in the world produces a new MachineState at a new
// StateVersion, never an edit to an existing one.
type MachineState struct {
	StateVersion
	Status     Status
	Attributes map[string]string
}
