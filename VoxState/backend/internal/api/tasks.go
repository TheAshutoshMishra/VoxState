package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
)

// defaultSimulatedWorkDuration governs how long a started task "runs" for
// via the HTTP API before completing on its own if not cancelled first.
// It exists only so the M3 lifecycle (PENDING -> RUNNING -> COMPLETED, or
// -> CANCELLED) is demonstrable end-to-end without real diagnostic tools,
// which don't exist until a later milestone.
const defaultSimulatedWorkDuration = 10 * time.Second

type taskHandlers struct {
	store *tasks.Store
}

type createTaskRequest struct {
	ToolName string `json:"tool_name,omitempty"`
}

type taskResponse struct {
	ID           string `json:"id"`
	MachineID    string `json:"machine_id"`
	ToolName     string `json:"tool_name"`
	BoundVersion int    `json:"bound_version"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

type startTaskRequest struct {
	// DurationSeconds optionally overrides how long the simulated work
	// runs before completing on its own, so a demo can pick a window long
	// enough (or short enough) to cancel within. Defaults to
	// defaultSimulatedWorkDuration if zero.
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

func (h *taskHandlers) createTask(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("id")

	var req createTaskRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	task, ev, err := h.store.CreateTask(tasks.CreateTaskInput{
		MachineID: machineID,
		ToolName:  req.ToolName,
	})
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		Task  taskResponse  `json:"task"`
		Event eventResponse `json:"event"`
	}{
		Task:  toTaskResponse(task),
		Event: toEventResponse(ev),
	})
}

func (h *taskHandlers) getTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	task, err := h.store.GetTask(id)
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

func (h *taskHandlers) listTasksForMachine(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("id")

	list, err := h.store.ListTasksForMachine(machineID)
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	out := make([]taskResponse, 0, len(list))
	for _, task := range list {
		out = append(out, toTaskResponse(task))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *taskHandlers) startTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req startTaskRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	duration := defaultSimulatedWorkDuration
	if req.DurationSeconds > 0 {
		duration = time.Duration(req.DurationSeconds * float64(time.Second))
	}

	task, err := h.store.StartTask(id, tasks.SimulatedWork(duration))
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

func (h *taskHandlers) cancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	task, ev, err := h.store.CancelTask(id)
	if err != nil {
		writeError(w, statusForTaskErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Task  taskResponse  `json:"task"`
		Event eventResponse `json:"event"`
	}{
		Task:  toTaskResponse(task),
		Event: toEventResponse(ev),
	})
}

func toTaskResponse(t tasks.DiagnosticTask) taskResponse {
	resp := taskResponse{
		ID:           t.ID,
		MachineID:    t.MachineID,
		ToolName:     t.ToolName,
		BoundVersion: t.BoundVersion,
		Status:       string(t.Status),
		CreatedAt:    t.CreatedAt.Format(timeFormat),
	}
	if !t.StartedAt.IsZero() {
		resp.StartedAt = t.StartedAt.Format(timeFormat)
	}
	if !t.FinishedAt.IsZero() {
		resp.FinishedAt = t.FinishedAt.Format(timeFormat)
	}
	return resp
}

// statusForTaskErr maps tasks/state package sentinel errors surfaced
// through the tasks package to HTTP status codes.
func statusForTaskErr(err error) int {
	switch {
	case errors.Is(err, tasks.ErrTaskNotFound), errors.Is(err, state.ErrMachineNotFound):
		return http.StatusNotFound
	case errors.Is(err, tasks.ErrInvalidTransition),
		errors.Is(err, tasks.ErrTaskAlreadyCompleted),
		errors.Is(err, tasks.ErrTaskAlreadyCancelled):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
