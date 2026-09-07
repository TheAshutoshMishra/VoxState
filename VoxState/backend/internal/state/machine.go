package state

import "time"

// Machine is the long-lived, largely-static identity of a physical machine
// being monitored. Its current status/readings live in MachineState, not
// here — that split is what makes versioning possible (see
// docs/DOMAIN_MODEL.md: "Why StateVersion is its own entity").
type Machine struct {
	ID        string
	Name      string
	Type      string
	Location  string
	CreatedAt time.Time
}

// Status is the operational status carried by a MachineState snapshot.
type Status string

const (
	StatusRunning          Status = "running"
	StatusFault            Status = "fault"
	StatusUnderMaintenance Status = "under_maintenance"
	StatusStopped          Status = "stopped"
)

// Valid reports whether s is one of the known status values.
func (s Status) Valid() bool {
	switch s {
	case StatusRunning, StatusFault, StatusUnderMaintenance, StatusStopped:
		return true
	default:
		return false
	}
}
