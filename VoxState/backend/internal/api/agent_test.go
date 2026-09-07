package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAgentRun_Accepted_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	rec := doJSON(t, router, http.MethodPost, "/machines/machine-17/agent/run", runAgentRequest{
		Instruction: "please check vibration",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body agentResultResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SelectedTool != "vibration_scan" {
		t.Errorf("SelectedTool = %q, want %q", body.SelectedTool, "vibration_scan")
	}
	if body.Outcome != "ACCEPTED" {
		t.Errorf("Outcome = %q, want %q", body.Outcome, "ACCEPTED")
	}
	if body.BoundVersion != 1 || body.CurrentVersion != 1 {
		t.Errorf("BoundVersion/CurrentVersion = %d/%d, want 1/1", body.BoundVersion, body.CurrentVersion)
	}
	if body.Payload["vibration"] != "NORMAL" {
		t.Errorf("Payload[vibration] = %v, want %q", body.Payload["vibration"], "NORMAL")
	}
	if body.TaskID == "" {
		t.Error("TaskID is empty")
	}
}

func TestAgentRun_MachineNotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines/does-not-exist/agent/run", runAgentRequest{
		Instruction: "check vibration",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestAgentRun_NoToolMatched_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	rec := doJSON(t, router, http.MethodPost, "/machines/machine-17/agent/run", runAgentRequest{
		Instruction: "do something unrelated",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
