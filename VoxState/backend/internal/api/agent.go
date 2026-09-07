package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/state"
)

// agentHandlers wires the M5 demo endpoint. It contains no orchestration
// logic itself — it only translates HTTP <-> agent.Instruction/Result,
// exactly like every other handler group in this package.
type agentHandlers struct {
	agent *agent.Agent
}

type runAgentRequest struct {
	Instruction string `json:"instruction"`
}

type agentResultResponse struct {
	SelectedTool    string         `json:"selected_tool"`
	TaskID          string         `json:"task_id"`
	BoundVersion    int            `json:"bound_version"`
	CurrentVersion  int            `json:"current_version"`
	Outcome         string         `json:"outcome"`
	Payload         map[string]any `json:"payload,omitempty"`
	RejectionReason string         `json:"rejection_reason,omitempty"`
	ReplanRequired  bool           `json:"replan_required,omitempty"`
}

// run demonstrates the full M5 flow over HTTP: instruction in, structured
// decision out. See internal/agent.Agent.Run for the actual orchestration
// (read state -> plan -> create task -> run tool -> policy gate).
func (h *agentHandlers) run(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("id")

	var req runAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	result, err := h.agent.Run(r.Context(), agent.Instruction{
		MachineID: machineID,
		Text:      req.Instruction,
	})
	if err != nil {
		writeError(w, statusForAgentErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentResultResponse{
		SelectedTool:    result.SelectedTool,
		TaskID:          result.TaskID,
		BoundVersion:    result.BoundVersion,
		CurrentVersion:  result.CurrentVersion,
		Outcome:         string(result.Outcome),
		Payload:         result.Payload,
		RejectionReason: result.RejectionReason,
		ReplanRequired:  result.ReplanRequired,
	})
}

func statusForAgentErr(err error) int {
	switch {
	case errors.Is(err, state.ErrMachineNotFound):
		return http.StatusNotFound
	case errors.Is(err, agent.ErrNoToolMatched):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadRequest
	}
}
