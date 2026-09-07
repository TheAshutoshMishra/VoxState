package policy

import "errors"

var (
	ErrMachineIDRequired   = errors.New("policy: machine id is required")
	ErrInvalidBoundVersion = errors.New("policy: bound version must be >= 1")
)
