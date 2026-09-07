package voice

import "errors"

var (
	ErrMachineIDRequired = errors.New("voice: machine id is required")
	ErrSessionIDRequired = errors.New("voice: session id is required")
	ErrSessionNotFound   = errors.New("voice: session not found")
	// ErrSessionAlreadyEnded is returned by Manager.End when a session's
	// EndedAt is already set — ending is a one-way transition, like a
	// tasks.DiagnosticTask reaching a terminal status.
	ErrSessionAlreadyEnded = errors.New("voice: session already ended")
)
