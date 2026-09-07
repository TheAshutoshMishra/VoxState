package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
)

// policyHandlers wires the M4 demo endpoint: given an existing task,
// simulate a ToolResult bound to that task's version and run it through
// the policy layer. This is the only place SimulateToolResult is used
// outside of tests — the correctness logic itself lives entirely in
// internal/policy, not here.
type policyHandlers struct {
	tasks     *tasks.Store
	evaluator *policy.Evaluator
}

type simulateResultRequest struct {
	Payload map[string]any `json:"payload,omitempty"`
}

type toolResultResponse struct {
	ID           string         `json:"id"`
	TaskID       string         `json:"task_id"`
	MachineID    string         `json:"machine_id"`
	BoundVersion int            `json:"bound_version"`
	Payload      map[string]any `json:"payload,omitempty"`
	CreatedAt    string         `json:"created_at"`
}

type decisionResponse struct {
	Outcome        string             `json:"outcome"`
	Result         toolResultResponse `json:"result"`
	CurrentVersion int                `json:"current_version"`
	Reason         string             `json:"reason,omitempty"`
	Event          *eventResponse     `json:"event,omitempty"`
}

// simulateAndEvaluateResult builds a simulated ToolResult for an existing
// task — copying that task's BoundVersion verbatim, exactly as a real
// tool orchestrator (M5+) would — and evaluates it through the policy
// layer. This is what makes the stale-result rejection flow demonstrable
// over HTTP: create a task, change the machine's state, then call this
// endpoint to see the result rejected.
func (h *policyHandlers) simulateAndEvaluateResult(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	var req simulateResultRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	task, err := h.tasks.GetTask(taskID)
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	result := policy.SimulateToolResult(task, req.Payload)

	decision, err := h.evaluator.Evaluate(result)
	if err != nil {
		writeError(w, statusForPolicyErr(err), err.Error())
		return
	}

	resp := decisionResponse{
		Outcome:        string(decision.Outcome),
		Result:         toToolResultResponse(decision.Result),
		CurrentVersion: decision.CurrentVersion,
		Reason:         decision.Reason,
	}
	if !decision.IsAccepted() {
		ev := toEventResponse(decision.Event)
		resp.Event = &ev
	}

	writeJSON(w, http.StatusOK, resp)
}

func toToolResultResponse(r policy.ToolResult) toolResultResponse {
	return toolResultResponse{
		ID:           r.ID,
		TaskID:       r.TaskID,
		MachineID:    r.MachineID,
		BoundVersion: r.BoundVersion,
		Payload:      r.Payload,
		CreatedAt:    r.CreatedAt.Format(timeFormat),
	}
}

func statusForPolicyErr(err error) int {
	switch {
	case errors.Is(err, state.ErrMachineNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}
