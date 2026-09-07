package state

import "errors"

var (
	ErrMachineIDRequired    = errors.New("state: machine id is required")
	ErrMachineAlreadyExists = errors.New("state: machine already exists")
	ErrMachineNotFound      = errors.New("state: machine not found")
	ErrNameRequired         = errors.New("state: machine name is required")
	ErrInvalidStatus        = errors.New("state: invalid machine status")
	ErrInvalidSource        = errors.New("state: invalid change source")
	ErrVersionNotFound      = errors.New("state: state version not found")
)
