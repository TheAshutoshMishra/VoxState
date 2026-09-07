package tasks

import "errors"

var (
	ErrMachineIDRequired    = errors.New("tasks: machine id is required")
	ErrTaskIDRequired       = errors.New("tasks: task id is required")
	ErrTaskNotFound         = errors.New("tasks: task not found")
	ErrInvalidTransition    = errors.New("tasks: invalid task lifecycle transition")
	ErrTaskAlreadyCompleted = errors.New("tasks: task already completed")
	ErrTaskAlreadyCancelled = errors.New("tasks: task already cancelled")
	ErrWorkRequired         = errors.New("tasks: work must not be nil")
)
