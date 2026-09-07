package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"voxstate/backend/internal/events"
	"voxstate/backend/internal/state"
)

// machineHandlers holds the dependencies for machine/state HTTP handlers.
// Handlers here only translate between HTTP and the state.Store — all
// validation and business logic lives in internal/state.
type machineHandlers struct {
	store *state.Store
}

// --- request/response DTOs (HTTP concerns, kept separate from domain types) ---

type createMachineRequest struct {
	ID         string            `json:"id,omitempty"`
	Name       string            `json:"name"`
	Type       string            `json:"type,omitempty"`
	Location   string            `json:"location,omitempty"`
	Status     string            `json:"status,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type machineResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Location  string `json:"location,omitempty"`
	CreatedAt string `json:"created_at"`
}

type stateResponse struct {
	MachineID       string            `json:"machine_id"`
	Version         int               `json:"version"`
	Status          string            `json:"status"`
	Attributes      map[string]string `json:"attributes,omitempty"`
	CreatedAt       string            `json:"created_at"`
	CausedByEventID string            `json:"caused_by_event_id,omitempty"`
}

type eventResponse struct {
	ID                    string         `json:"id"`
	Type                  string         `json:"type"`
	MachineID             string         `json:"machine_id,omitempty"`
	Payload               map[string]any `json:"payload,omitempty"`
	ResultingStateVersion *int           `json:"resulting_state_version,omitempty"`
	CreatedAt             string         `json:"created_at"`
}

type changeStateRequest struct {
	Status     string            `json:"status,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Source     string            `json:"source,omitempty"`
	Note       string            `json:"note,omitempty"`
}

type changeStateResponse struct {
	State   stateResponse   `json:"state"`
	Changed bool            `json:"changed"`
	Events  []eventResponse `json:"events,omitempty"`
}

// --- handlers ---

func (h *machineHandlers) createMachine(w http.ResponseWriter, r *http.Request) {
	var req createMachineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	machine, st, ev, err := h.store.CreateMachine(state.CreateMachineInput{
		ID:                req.ID,
		Name:              req.Name,
		Type:              req.Type,
		Location:          req.Location,
		InitialStatus:     state.Status(strings.ToLower(strings.TrimSpace(req.Status))),
		InitialAttributes: req.Attributes,
	})
	if err != nil {
		writeError(w, statusForStateErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		Machine machineResponse `json:"machine"`
		State   stateResponse   `json:"state"`
		Event   eventResponse   `json:"event"`
	}{
		Machine: toMachineResponse(machine),
		State:   toStateResponse(st),
		Event:   toEventResponse(ev),
	})
}

func (h *machineHandlers) getMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	machine, err := h.store.GetMachine(id)
	if err != nil {
		writeError(w, statusForStateErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toMachineResponse(machine))
}

// listMachines returns every registered machine. It exists for M8's
// frontend, which has no other way to discover which machine IDs exist —
// every other machine/state endpoint requires already knowing one.
func (h *machineHandlers) listMachines(w http.ResponseWriter, r *http.Request) {
	machines := h.store.ListMachines()

	out := make([]machineResponse, 0, len(machines))
	for _, m := range machines {
		out = append(out, toMachineResponse(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *machineHandlers) getState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	versionParam := r.URL.Query().Get("version")
	if versionParam == "" {
		st, err := h.store.GetCurrentState(id)
		if err != nil {
			writeError(w, statusForStateErr(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toStateResponse(st))
		return
	}

	version, err := strconv.Atoi(versionParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "version must be an integer")
		return
	}

	st, err := h.store.GetStateVersion(id, version)
	if err != nil {
		writeError(w, statusForStateErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toStateResponse(st))
}

func (h *machineHandlers) changeState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req changeStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	input := state.StateChangeInput{
		Attributes: req.Attributes,
		Source:     state.ChangeSource(strings.ToLower(strings.TrimSpace(req.Source))),
		Note:       req.Note,
	}
	if s := strings.ToLower(strings.TrimSpace(req.Status)); s != "" {
		status := state.Status(s)
		input.Status = &status
	}

	st, changed, evs, err := h.store.ChangeState(id, input)
	if err != nil {
		writeError(w, statusForStateErr(err), err.Error())
		return
	}

	eventResponses := make([]eventResponse, 0, len(evs))
	for _, e := range evs {
		eventResponses = append(eventResponses, toEventResponse(e))
	}

	writeJSON(w, http.StatusOK, changeStateResponse{
		State:   toStateResponse(st),
		Changed: changed,
		Events:  eventResponses,
	})
}

// --- mapping helpers ---

func toMachineResponse(m state.Machine) machineResponse {
	return machineResponse{
		ID:        m.ID,
		Name:      m.Name,
		Type:      m.Type,
		Location:  m.Location,
		CreatedAt: m.CreatedAt.Format(timeFormat),
	}
}

func toStateResponse(st state.MachineState) stateResponse {
	return stateResponse{
		MachineID:       st.MachineID,
		Version:         st.Version,
		Status:          string(st.Status),
		Attributes:      st.Attributes,
		CreatedAt:       st.CreatedAt.Format(timeFormat),
		CausedByEventID: st.CausedByEventID,
	}
}

func toEventResponse(e events.Event) eventResponse {
	return eventResponse{
		ID:                    e.ID,
		Type:                  string(e.Type),
		MachineID:             e.MachineID,
		Payload:               e.Payload,
		ResultingStateVersion: e.ResultingStateVersion,
		CreatedAt:             e.CreatedAt.Format(timeFormat),
	}
}

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// statusForStateErr maps state package sentinel errors to HTTP status
// codes. Anything unrecognized is treated as a 400 — the state package
// only returns validation/not-found/conflict errors as of M2.
func statusForStateErr(err error) int {
	switch {
	case errors.Is(err, state.ErrMachineNotFound), errors.Is(err, state.ErrVersionNotFound):
		return http.StatusNotFound
	case errors.Is(err, state.ErrMachineAlreadyExists):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
