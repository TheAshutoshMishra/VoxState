package voice

import (
	"fmt"
	"sort"
	"strings"

	"voxstate/backend/internal/agent"
)

// TextResponse converts an agent.Result into the text VoxState should
// speak, or "" if nothing should be spoken. This is the M6 equivalent of
// M5's "the Agent never consumes a stale payload" guarantee (see
// agent.Result's doc comment) — it is the ONLY function in this package
// permitted to read result.Payload, and it structurally cannot leak a
// rejected result's payload into speech: the payload read happens
// exclusively inside the OutcomeAccepted case, and formatRejection below
// takes only a reason string, not an agent.Result or a payload map, as
// its parameter. A future maintainer cannot thread Payload into the
// rejected branch without changing that function's signature — a
// visible, reviewable diff — which is the same "make misuse a type
// error, not a documentation violation" discipline agent.Result and
// policy.ToolResult already use.
//
// In practice agent.Run never sets Payload on a rejected Result anyway
// (see agent/agent.go), so this is defense in depth, not the only thing
// standing between a stale value and a spoken response — but it is
// exercised directly, independent of that upstream guarantee, by
// response_test.go's TestTextResponseRejectedNeverIncludesPayloadContent.
func TextResponse(result agent.Result) string {
	switch result.Outcome {
	case agent.OutcomeAccepted:
		return formatAcceptedPayload(result.SelectedTool, result.Payload)
	case agent.OutcomeRejected:
		return formatRejection(result.RejectionReason)
	default:
		return ""
	}
}

// formatAcceptedPayload is unexported and only ever called from
// TextResponse's Accepted case above — it is not a general-purpose
// "format a payload" helper reachable from anywhere else in this
// package, deliberately, so every read of an agent.Result's Payload in
// this file happens at exactly one call site.
func formatAcceptedPayload(toolName string, payload map[string]any) string {
	if len(payload) == 0 {
		return fmt.Sprintf("%s completed with no readings to report.", toolName)
	}

	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s is %v", k, payload[k]))
	}

	return fmt.Sprintf("%s result: %s.", toolName, strings.Join(parts, ", "))
}

// formatRejection never touches an agent.Result or a payload map — its
// parameter list is a plain string, so it cannot leak payload content
// even if TextResponse's switch above were edited carelessly.
func formatRejection(reason string) string {
	if reason == "" {
		return "That result is no longer valid against the current state. Let me check again."
	}
	return fmt.Sprintf("That result is no longer valid against the current state (%s). Let me check again.", reason)
}
