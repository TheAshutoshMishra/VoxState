package tasks

import (
	"context"
	"time"
)

// Status is a DiagnosticTask's lifecycle state.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED"
)

// IsTerminal reports whether s is a terminal status — once reached, a task
// never leaves it (see docs/ROADMAP.md M3: COMPLETED and CANCELLED must
// each be a one-way door).
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// DiagnosticTask is a unit of work bound, once and permanently, to the
// machine StateVersion that was current when it was created. All fields
// are plain values (no pointers/maps) so a DiagnosticTask can be returned
// by copy with no risk of a caller mutating the Store's internal state —
// see internal/state's MachineState for the same reasoning.
//
// StartedAt/FinishedAt use the zero time.Time to mean "not yet reached"
// rather than a pointer, for the same reason.
type DiagnosticTask struct {
	ID           string
	MachineID    string
	ToolName     string
	BoundVersion int
	Status       Status
	CreatedAt    time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
}

// Work is the asynchronous unit of work a task executes once started. It
// is the abstraction M3 provides in place of real diagnostic tools — a
// later milestone's tool orchestrator supplies real Work implementations
// through the same interface.
//
// Contract: Work must observe ctx.Done() and return promptly when it
// fires. Returning nil signals the work finished normally; returning
// ctx.Err() (or any error) after the context was cancelled signals it
// stopped because of cancellation. Store.StartTask only transitions a
// task to COMPLETED if it is still RUNNING when Work returns — if
// CancelTask already won the race, the task stays CANCELLED regardless of
// what Work returns.
type Work func(ctx context.Context) error
