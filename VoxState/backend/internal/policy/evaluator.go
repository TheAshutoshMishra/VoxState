// Package policy is the safety boundary that decides whether a
// DiagnosticTask's ToolResult is still valid before it is allowed to
// influence anything downstream (Agent reasoning / spoken response, once
// those exist in M5+). Its single responsibility, per
// docs/ARCHITECTURE.md's Policy layer section, is answering: "is the
// state version this result was produced against still the current
// version?"
//
// The rule is exact equality, not "not older than":
//
//	result.BoundVersion == machine.CurrentVersion  -> ACCEPT
//	result.BoundVersion != machine.CurrentVersion  -> REJECT (older OR newer)
//
// policy depends on state (to read a machine's current version) and, only
// for the SimulateToolResult demo/test helper, on tasks (to copy a
// completed task's bound version into a ToolResult). state must never
// depend on policy — see docs/decisions/001-modular-monolith.md and
// CLAUDE.md for the required dependency direction (api -> policy -> state,
// tasks -> state).
package policy

import (
	"fmt"
	"strings"

	"voxstate/backend/internal/events"
	"voxstate/backend/internal/state"
)

// Evaluator checks ToolResults against a machine's current StateVersion.
// It holds no mutable state of its own beyond a reference to the
// state.Store that remains the single source of truth.
type Evaluator struct {
	state *state.Store
}

// NewEvaluator creates an Evaluator backed by stateStore.
func NewEvaluator(stateStore *state.Store) *Evaluator {
	return &Evaluator{state: stateStore}
}

// Evaluate is the policy boundary: it decides, once, whether result is
// still valid against the machine's current state version. It never
// mutates machine state, a task's bound version, or the result itself —
// evaluation is purely observational.
//
// Consistency boundary: the returned Decision reflects the machine's
// current version at the moment Evaluate reads it. internal/state.Store
// guards its version history with a mutex (see state.Store.GetCurrentState),
// so that read is atomic — it cannot observe a torn or partial version,
// and nothing about the comparison itself is racy. What Evaluate does NOT
// guarantee is that the machine is still at that version by the time a
// caller acts on an ACCEPTED Decision: the machine can move to a new
// version immediately after Evaluate returns. This is an intentional
// point-in-time gate, not a distributed transaction — holding the state
// store locked across "evaluate + consume" would be a much stronger (and
// here, unwarranted) guarantee that docs/ARCHITECTURE.md's stale-result
// flow does not call for. Callers (the Agent, in M5+) must call Evaluate
// as close as possible to the point of actually consuming a result rather
// than caching a Decision and trusting it indefinitely.
//
// No state.Store API extension was needed to make this atomic: the only
// store access Evaluate performs is the single GetCurrentState read
// above, and the comparison against result.BoundVersion happens entirely
// against the value already captured by that read — there is no window
// between "read" and "compare" where a concurrent state change could
// affect the outcome of this call.
func (e *Evaluator) Evaluate(result ToolResult) (Decision, error) {
	machineID := strings.TrimSpace(result.MachineID)
	if machineID == "" {
		return Decision{}, ErrMachineIDRequired
	}
	if result.BoundVersion < 1 {
		return Decision{}, ErrInvalidBoundVersion
	}

	current, err := e.state.GetCurrentState(machineID)
	if err != nil {
		return Decision{}, err
	}

	if result.BoundVersion == current.Version {
		return Decision{
			Outcome:        Accepted,
			Result:         result,
			CurrentVersion: current.Version,
		}, nil
	}

	reason := staleReason(result.BoundVersion, current.Version)

	ev := events.New(events.TypeToolResultRejected, machineID, map[string]any{
		"task_id":         result.TaskID,
		"result_id":       result.ID,
		"result_version":  result.BoundVersion,
		"current_version": current.Version,
		"reason":          reason,
	})

	return Decision{
		Outcome:        Rejected,
		Result:         result,
		CurrentVersion: current.Version,
		Reason:         reason,
		Event:          ev,
	}, nil
}

func staleReason(resultVersion, currentVersion int) string {
	if resultVersion < currentVersion {
		return fmt.Sprintf(
			"result bound to state version %d, but machine is now at version %d: result is stale",
			resultVersion, currentVersion)
	}
	return fmt.Sprintf(
		"result bound to state version %d, but machine is at version %d: result version is ahead of current and invalid",
		resultVersion, currentVersion)
}
