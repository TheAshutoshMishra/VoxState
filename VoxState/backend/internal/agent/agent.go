// Package agent is the orchestration layer that ties state, tasks, tools,
// and policy together: given an instruction, it inspects current machine
// state, selects a tool, creates a task bound to that state's version,
// runs the tool, and gates the resulting ToolResult through
// policy.Evaluator before ever treating it as truth.
//
// The single rule this package exists to uphold, stated in CLAUDE.md and
// docs/POLICY.md: the Agent must never consume a ToolResult merely
// because a tool returned it. Every ToolResult passes through
// policy.Evaluator immediately before Run decides what to do with it, and
// Run never duplicates the M4 version-equality check itself — it only
// branches on the Decision policy already made. See Run's doc comment for
// the exact flow.
//
// Dependency direction: agent -> tools, tasks, policy -> state. agent
// never lets state or policy reach back into it — Planner and Tool are
// interfaces this package depends on, not implementations that depend on
// this package.
package agent

import (
	"context"
	"strings"

	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
)

// Agent orchestrates a single instruction end to end. It holds no mutable
// state of its own beyond references to the stores/evaluator/registry it
// was constructed with — a Result is a self-contained value, so nothing
// about calling Run concurrently for different instructions requires
// synchronization at this layer (the underlying stores already handle
// their own concurrency, per M2/M3).
type Agent struct {
	state     *state.Store
	tasks     *tasks.Store
	evaluator *policy.Evaluator
	registry  tools.Registry
	planner   Planner
}

// New constructs an Agent. planner may be nil, in which case a
// deterministic KeywordPlanner is used — the only Planner implementation
// M5 provides.
func New(stateStore *state.Store, taskStore *tasks.Store, evaluator *policy.Evaluator, registry tools.Registry, planner Planner) *Agent {
	if planner == nil {
		planner = NewKeywordPlanner()
	}
	return &Agent{
		state:     stateStore,
		tasks:     taskStore,
		evaluator: evaluator,
		registry:  registry,
		planner:   planner,
	}
}

// Run executes one instruction end to end:
//
//  1. read the machine's current MachineState (internal/state)
//  2. ask the Planner which tool to use
//  3. create a DiagnosticTask bound to that current version (internal/tasks)
//  4. start the tool's work through the task engine, so the M3
//     cancellation architecture (context propagation, task lifecycle) is
//     reused rather than reimplemented
//  5. build a policy.ToolResult from the task's BoundVersion — never the
//     machine's current version — and its output payload
//  6. evaluate that result through policy.Evaluator — the single
//     authority on whether it's still valid
//  7. only if the Decision is Accepted does Run copy the payload into the
//     returned Result; on Rejected, Result.Payload is left nil and
//     Result.ReplanRequired is set, but the stale payload never leaves
//     this function
//
// If ctx is cancelled while the tool is running, Run stops waiting and
// cancels the underlying task (via tasks.Store.CancelTask, not a second
// cancellation mechanism), then returns ctx.Err().
func (a *Agent) Run(ctx context.Context, instruction Instruction) (Result, error) {
	machineID := strings.TrimSpace(instruction.MachineID)
	if machineID == "" {
		return Result{}, ErrMachineIDRequired
	}

	current, err := a.state.GetCurrentState(machineID)
	if err != nil {
		return Result{}, err
	}

	plan, err := a.planner.Plan(ctx, instruction, current)
	if err != nil {
		return Result{}, err
	}

	tool, ok := a.registry.Get(plan.ToolName)
	if !ok {
		return Result{}, ErrToolNotRegistered
	}

	task, _, err := a.tasks.CreateTask(tasks.CreateTaskInput{
		MachineID: machineID,
		ToolName:  plan.ToolName,
	})
	if err != nil {
		return Result{}, err
	}

	output, err := a.runTool(ctx, task, tool)
	if err != nil {
		return Result{}, err
	}

	result := policy.SimulateToolResult(task, output.Payload)

	decision, err := a.evaluator.Evaluate(result)
	if err != nil {
		return Result{}, err
	}

	agentResult := Result{
		Instruction:    instruction,
		SelectedTool:   plan.ToolName,
		TaskID:         task.ID,
		BoundVersion:   task.BoundVersion,
		CurrentVersion: decision.CurrentVersion,
	}

	if decision.IsAccepted() {
		agentResult.Outcome = OutcomeAccepted
		agentResult.Payload = decision.Result.Payload
		return agentResult, nil
	}

	agentResult.Outcome = OutcomeRejected
	agentResult.RejectionReason = decision.Reason
	agentResult.ReplanRequired = true
	return agentResult, nil
}

// runTool starts tool's work through the task engine (tasks.StartTask)
// and waits for either a result or ctx cancellation. It reuses M3's
// cancellation architecture rather than propagating ctx directly into the
// tool: StartTask derives its own context internally and only cancels it
// via CancelTask, so that is the mechanism Run uses too when its own ctx
// is cancelled — there is exactly one cancellation path, not two racing
// implementations of it.
func (a *Agent) runTool(ctx context.Context, task tasks.DiagnosticTask, tool tools.Tool) (tools.RunOutput, error) {
	type outcome struct {
		output tools.RunOutput
		err    error
	}
	resultCh := make(chan outcome, 1)

	work := func(taskCtx context.Context) error {
		output, err := tool.Run(taskCtx, tools.RunInput{MachineID: task.MachineID})
		resultCh <- outcome{output: output, err: err}
		return err
	}

	if _, err := a.tasks.StartTask(task.ID, work); err != nil {
		return tools.RunOutput{}, err
	}

	select {
	case o := <-resultCh:
		return o.output, o.err
	case <-ctx.Done():
		// Best-effort: the task may already have finished between the
		// two select cases becoming ready, in which case CancelTask
		// returns an already-terminal error that we don't need to act
		// on — Run is already returning ctx.Err() regardless.
		_, _, _ = a.tasks.CancelTask(task.ID)
		return tools.RunOutput{}, ctx.Err()
	}
}
