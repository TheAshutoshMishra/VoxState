package agent

import "errors"

var (
	ErrMachineIDRequired = errors.New("agent: machine id is required")
	// ErrNoToolMatched is returned by a Planner when no rule matches the
	// instruction text.
	ErrNoToolMatched = errors.New("agent: no tool matched the instruction")
	// ErrToolNotRegistered is returned when a Planner selects a tool name
	// that isn't present in the Agent's Registry — a configuration bug
	// (planner and registry disagreeing), not a user input error.
	ErrToolNotRegistered = errors.New("agent: selected tool is not registered")
)
