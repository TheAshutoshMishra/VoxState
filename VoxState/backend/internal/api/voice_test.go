package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"voxstate/backend/internal/voice"
)

// The fakes below back every voice.Manager used in this package's tests
// (including newTestRouter, used by every other _test.go file in this
// package) — internal/api's tests never import voice/livekit, voice/rime,
// or voice/deepgram, matching the same "no third-party SDK in tests"
// convention voice's own tests follow.

type fakeRoomProvisioner struct{}

func (fakeRoomProvisioner) Provision(_ context.Context, sessionID string) (string, string, string, error) {
	return "room-" + sessionID, "wss://fake.livekit.local", "fake-token-" + sessionID, nil
}

func (fakeRoomProvisioner) Teardown(context.Context, string) error { return nil }

type fakeSTT struct{}

func (fakeSTT) Transcribe(context.Context, []int16, int) (string, error) { return "", nil }

func fakeSTTFactory() voice.STT { return fakeSTT{} }

type fakeTTS struct{}

func (fakeTTS) Synthesize(context.Context, string) ([]int16, int, error) { return nil, 0, nil }

func fakeTTSFactory() voice.TTS { return fakeTTS{} }

type fakeTransport struct {
	frames chan voice.AudioFrame
}

func (f *fakeTransport) Send(context.Context, []int16, int) error { return nil }
func (f *fakeTransport) Frames() <-chan voice.AudioFrame          { return f.frames }
func (f *fakeTransport) Close() error                             { close(f.frames); return nil }

func fakeTransportFactory(context.Context, string, string) (voice.Transport, error) {
	return &fakeTransport{frames: make(chan voice.AudioFrame)}, nil
}

func TestVoiceSessionStart_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	rec := doJSON(t, router, http.MethodPost, "/machines/machine-17/voice/sessions", startVoiceSessionRequest{UserID: "tester"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var body voiceSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if body.MachineID != "machine-17" {
		t.Errorf("MachineID = %q, want %q", body.MachineID, "machine-17")
	}
	if body.LiveKitRoomID == "" || body.LiveKitURL == "" || body.LiveKitToken == "" {
		t.Errorf("expected non-empty LiveKit connection fields, got %+v", body)
	}
}

func TestVoiceSessionStart_MachineNotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines/does-not-exist/voice/sessions", startVoiceSessionRequest{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestVoiceSessionGetAndEnd_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	startRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/voice/sessions", startVoiceSessionRequest{})
	var started voiceSessionResponse
	if err := json.NewDecoder(startRec.Body).Decode(&started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	getRec := doJSON(t, router, http.MethodGet, "/voice/sessions/"+started.SessionID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d, body = %s", getRec.Code, http.StatusOK, getRec.Body.String())
	}

	endRec := doJSON(t, router, http.MethodPost, "/voice/sessions/"+started.SessionID+"/end", nil)
	if endRec.Code != http.StatusOK {
		t.Fatalf("end status = %d, want %d, body = %s", endRec.Code, http.StatusOK, endRec.Body.String())
	}
	var ended voiceSessionResponse
	if err := json.NewDecoder(endRec.Body).Decode(&ended); err != nil {
		t.Fatalf("decode end response: %v", err)
	}
	if ended.EndedAt == "" {
		t.Error("EndedAt is empty after ending the session")
	}

	// Ending an already-ended session is a one-way transition, matching
	// tasks.CancelTask's terminal-state discipline.
	endAgainRec := doJSON(t, router, http.MethodPost, "/voice/sessions/"+started.SessionID+"/end", nil)
	if endAgainRec.Code != http.StatusConflict {
		t.Fatalf("second end status = %d, want %d, body = %s", endAgainRec.Code, http.StatusConflict, endAgainRec.Body.String())
	}
}

func TestVoiceSessionGet_NotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodGet, "/voice/sessions/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
