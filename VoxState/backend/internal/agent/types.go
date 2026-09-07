package agent

// Instruction is a maintenance request the Agent is asked to act on. M5
// has no voice/LLM input — Text is a plain string a deterministic Planner
// matches keywords against (see planner.go). This is the seam a later
// milestone's transcript/LLM input would plug into.
type Instruction struct {
	MachineID string
	Text      string
}

// Plan is a Planner's decision about which tool to run. It carries a
// human-readable Reason (why this tool was chosen) purely for
// observability/debugging — nothing downstream depends on its contents.
type Plan struct {
	ToolName string
	Reason   string
}

// Outcome mirrors policy.Outcome at the Agent's level of abstraction, so
// callers of this package don't need to import internal/policy just to
// read a result.
type Outcome string

const (
	OutcomeAccepted Outcome = "ACCEPTED"
	OutcomeRejected Outcome = "REJECTED"
)

// Result is the Agent's structured, deterministic output — the M5
// substitute for "the agent's final response text" until a voice
// milestone exists to speak it. It is an immutable value: nothing about
// Run mutates a Result after returning it.
//
// Payload is set ONLY when Outcome == OutcomeAccepted. This is the
// concrete enforcement point for "the Agent must never consume a stale
// result": there is no field on Result that ever carries a rejected
// result's payload, so a caller cannot accidentally read stale diagnostic
// data out of a Result even by mistake — they would have to reach past
// this package into policy.Decision.Result directly, which Run does not
// expose.
type Result struct {
	Instruction Instruction

	SelectedTool string
	TaskID       string
	// BoundVersion is the task's bound state version, copied through
	// unchanged from tasks.DiagnosticTask.BoundVersion. Run never
	// rewrites it — doing so (e.g. "repairing" a result by setting it to
	// the machine's current version) would destroy the M4 stale-result
	// detection this whole package exists to respect.
	BoundVersion int
	// CurrentVersion is the machine's version at the moment policy
	// evaluated the result — see docs/POLICY.md's consistency-boundary
	// section for what this does and does not guarantee.
	CurrentVersion int

	Outcome Outcome

	// Payload holds the accepted diagnostic payload. Zero value (nil)
	// when Outcome == OutcomeRejected.
	Payload map[string]any

	// RejectionReason and ReplanRequired are set only when
	// Outcome == OutcomeRejected. ReplanRequired signals that the caller
	// (a future M6/M7 voice/interruption flow, or this milestone's tests)
	// should re-run the Agent against current state rather than trust
	// this Result's payload — M5 deliberately stops at signaling that,
	// per the milestone brief: it does not implement automatic
	// replanning (no auto-retry loop, no new task is silently created).
	RejectionReason string
	ReplanRequired  bool
}
