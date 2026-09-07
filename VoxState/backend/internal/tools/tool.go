// Package tools represents diagnostic capabilities the Agent can invoke.
// It deliberately contains no Agent reasoning, no task lifecycle
// management, and no policy/staleness logic — a Tool just does work and
// returns what it found. It has zero dependencies on state, tasks, or
// policy: binding a tool's output to a specific task's BoundVersion (and
// deciding whether that binding is still valid) is the caller's
// responsibility (internal/agent, then internal/policy), not this
// package's.
package tools

import (
	"context"
	"time"
)

// RunInput is what a Tool needs to perform its (simulated) work. It is
// deliberately minimal — tools do not read machine state or task details
// themselves; the Agent already has that context and decides what to run.
type RunInput struct {
	MachineID string
}

// RunOutput is what a Tool produces. Payload is opaque diagnostic data;
// Duration is the only "execution metadata" genuinely useful at this
// scope (how long the simulated work took), not a general-purpose
// metadata bag.
type RunOutput struct {
	Payload  map[string]any
	Duration time.Duration
}

// Tool is the minimal abstraction the Agent depends on to request a
// diagnostic operation without being coupled to a specific
// implementation. Implementations must be context-aware: Run should
// check ctx.Done() during any simulated delay and return promptly on
// cancellation, exactly like tasks.Work (M3) — this package deliberately
// mirrors that pattern rather than inventing a second one.
//
// A Tool never decides whether its own result is still valid — that is
// exclusively internal/policy's responsibility once the Agent has turned
// this RunOutput into a policy.ToolResult.
type Tool interface {
	Name() string
	Run(ctx context.Context, input RunInput) (RunOutput, error)
}

// Registry is an immutable-after-construction lookup of Tools by name.
type Registry struct {
	byName map[string]Tool
}

// NewRegistry indexes the given tools by their Name().
func NewRegistry(tools ...Tool) Registry {
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name()] = t
	}
	return Registry{byName: byName}
}

// Get returns the tool registered under name, if any.
func (r Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}
