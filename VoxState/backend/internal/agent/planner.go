package agent

import (
	"context"
	"strings"

	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tools"
)

// Planner decides which tool an Instruction should dispatch to. This is
// the narrow abstraction M5 introduces so a real LLM/reasoning provider
// can be wired in later without changing Agent.Run's orchestration logic
// — only the Planner implementation would need to change. M5 provides
// only a deterministic implementation (KeywordPlanner); no LLM SDK, API
// key, or network call exists anywhere in this package. See CLAUDE.md's
// M5 entry for why that's a deliberate scope decision, not an oversight.
type Planner interface {
	Plan(ctx context.Context, instruction Instruction, current state.MachineState) (Plan, error)
}

// KeywordPlanner is a deterministic Planner: it matches substrings in an
// Instruction's Text against a fixed, ordered list of keyword->tool
// rules. No randomness, no external calls — the same Instruction always
// produces the same Plan, which is what makes M5's tests reproducible.
type KeywordPlanner struct {
	rules []keywordRule
}

type keywordRule struct {
	keyword  string
	toolName string
}

// NewKeywordPlanner returns a Planner that recognizes the two demo tools
// (vibration_scan, temperature_scan) by keyword. current is accepted to
// satisfy the Planner interface but unused by this implementation —
// a future, smarter Planner could use it (e.g. to prefer a tool relevant
// to the machine's current status); this one deliberately does not.
func NewKeywordPlanner() *KeywordPlanner {
	return &KeywordPlanner{
		rules: []keywordRule{
			{keyword: "vibration", toolName: tools.NameVibrationScan},
			{keyword: "temperature", toolName: tools.NameTemperatureScan},
			{keyword: "temp", toolName: tools.NameTemperatureScan},
		},
	}
}

func (p *KeywordPlanner) Plan(_ context.Context, instruction Instruction, _ state.MachineState) (Plan, error) {
	text := strings.ToLower(instruction.Text)
	for _, r := range p.rules {
		if strings.Contains(text, r.keyword) {
			return Plan{
				ToolName: r.toolName,
				Reason:   "instruction mentions \"" + r.keyword + "\"",
			}, nil
		}
	}
	return Plan{}, ErrNoToolMatched
}
