package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"voxstate/backend/internal/state"
	"voxstate/backend/internal/voice"
)

// voiceHandlers wires the M6 session-lifecycle endpoints. Like every
// other handler group in this package, it contains no orchestration
// logic itself — session lifecycle (provisioning a room, starting the
// bot's STT->agent->TTS loop) lives in internal/voice; this file only
// translates HTTP <-> voice.Manager calls.
type voiceHandlers struct {
	manager *voice.Manager
}

type startVoiceSessionRequest struct {
	UserID string `json:"user_id,omitempty"`
}

type voiceSessionResponse struct {
	SessionID     string `json:"session_id"`
	MachineID     string `json:"machine_id"`
	LiveKitRoomID string `json:"livekit_room_id"`
	LiveKitURL    string `json:"livekit_url"`
	LiveKitToken  string `json:"livekit_token"`
	UserID        string `json:"user_id,omitempty"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
}

// start mints a LiveKit room + access token bound to this machine and
// starts the bot's realtime session loop. See internal/voice.Manager.Start
// for the actual orchestration.
func (h *voiceHandlers) start(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("id")

	var req startVoiceSessionRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	info, err := h.manager.Start(r.Context(), machineID, req.UserID)
	if err != nil {
		writeError(w, statusForVoiceErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toVoiceSessionResponse(info))
}

// end stops session {id}'s realtime loop and tears down its room.
func (h *voiceHandlers) end(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	info, err := h.manager.End(r.Context(), id)
	if err != nil {
		writeError(w, statusForVoiceErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toVoiceSessionResponse(info))
}

// get returns session {id}'s current status.
func (h *voiceHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	info, err := h.manager.Get(id)
	if err != nil {
		writeError(w, statusForVoiceErr(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toVoiceSessionResponse(info))
}

func toVoiceSessionResponse(info voice.SessionInfo) voiceSessionResponse {
	resp := voiceSessionResponse{
		SessionID:     info.ID,
		MachineID:     info.MachineID,
		LiveKitRoomID: info.LiveKitRoomID,
		LiveKitURL:    info.LiveKitURL,
		LiveKitToken:  info.LiveKitToken,
		UserID:        info.UserID,
		StartedAt:     info.StartedAt.Format(timeFormat),
	}
	if info.EndedAt != nil {
		resp.EndedAt = info.EndedAt.Format(timeFormat)
	}
	return resp
}

// statusForVoiceErr maps voice package sentinel errors to HTTP status
// codes, mirroring statusForTaskErr's shape.
func statusForVoiceErr(err error) int {
	switch {
	case errors.Is(err, voice.ErrSessionNotFound), errors.Is(err, state.ErrMachineNotFound):
		return http.StatusNotFound
	case errors.Is(err, voice.ErrSessionAlreadyEnded):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
