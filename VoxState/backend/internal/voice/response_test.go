package voice

import (
	"strings"
	"testing"

	"voxstate/backend/internal/agent"
)

func TestTextResponse_AcceptedIncludesPayload(t *testing.T) {
	result := agent.Result{
		Outcome:      agent.OutcomeAccepted,
		SelectedTool: "vibration_scan",
		Payload:      map[string]any{"vibration": "NORMAL"},
	}

	got := TextResponse(result)
	if !strings.Contains(got, "NORMAL") {
		t.Errorf("TextResponse() = %q, want it to mention the accepted payload", got)
	}
	if !strings.Contains(got, "vibration_scan") {
		t.Errorf("TextResponse() = %q, want it to mention the selected tool", got)
	}
}

func TestTextResponse_RejectedNeverIncludesPayloadContent(t *testing.T) {
	cases := []struct {
		name   string
		result agent.Result
	}{
		{
			name:   "real shape: Payload nil on reject",
			result: agent.Result{Outcome: agent.OutcomeRejected, RejectionReason: "result bound to state version 1, but machine is now at version 2: result is stale"},
		},
		{
			// agent.Run itself never produces this shape — Payload is
			// only ever assigned in Run's Accepted branch (see
			// agent/agent.go). This case exists to prove the guarantee
			// lives in TextResponse's own switch structure, not merely in
			// the fact that agent.Run happens to leave Payload nil on a
			// rejection.
			name: "defensive: Payload fabricated non-nil on reject",
			result: agent.Result{
				Outcome:         agent.OutcomeRejected,
				RejectionReason: "stale",
				Payload:         map[string]any{"vibration": "CRITICAL"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TextResponse(c.result)
			if strings.Contains(got, "CRITICAL") {
				t.Errorf("TextResponse() = %q, leaked payload content", got)
			}
		})
	}
}

func TestTextResponse_EmptyOnUnknownOutcome(t *testing.T) {
	got := TextResponse(agent.Result{})
	if got != "" {
		t.Errorf("TextResponse() = %q, want empty string for a zero-value Result", got)
	}
}

func TestTextResponse_RejectedWithEmptyReasonStillSaysSomething(t *testing.T) {
	got := TextResponse(agent.Result{Outcome: agent.OutcomeRejected})
	if got == "" {
		t.Error("TextResponse() = \"\", want a non-empty fallback message even with an empty RejectionReason")
	}
}
