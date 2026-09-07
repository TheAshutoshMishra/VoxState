package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSimulateAndEvaluateResult_Accepted_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)

	// Task is bound to v1, machine is still at v1 — result should be accepted.
	rec := doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/result", simulateResultRequest{
		Payload: map[string]any{"vibration": "NORMAL"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body decisionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Outcome != "ACCEPTED" {
		t.Errorf("Outcome = %q, want %q", body.Outcome, "ACCEPTED")
	}
	if body.Result.BoundVersion != 1 {
		t.Errorf("Result.BoundVersion = %d, want 1", body.Result.BoundVersion)
	}
	if body.CurrentVersion != 1 {
		t.Errorf("CurrentVersion = %d, want 1", body.CurrentVersion)
	}
	if body.Event != nil {
		t.Errorf("Event = %+v, want nil for an accepted decision", body.Event)
	}
}

// TestSimulateAndEvaluateResult_Rejected_HTTP exercises the exact M4 core
// example end to end over the HTTP API: task bound to v1, machine moves
// to v2, simulated result still bound to v1 must be rejected.
func TestSimulateAndEvaluateResult_Rejected_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)
	if createBody.Task.BoundVersion != 1 {
		t.Fatalf("task BoundVersion = %d, want 1", createBody.Task.BoundVersion)
	}

	changeRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/state", changeStateRequest{Status: "fault"})
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change status = %d, body = %s", changeRec.Code, changeRec.Body.String())
	}

	rec := doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/result", simulateResultRequest{
		Payload: map[string]any{"vibration": "CRITICAL"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body decisionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Outcome != "REJECTED" {
		t.Fatalf("Outcome = %q, want %q", body.Outcome, "REJECTED")
	}
	if body.Result.BoundVersion != 1 {
		t.Errorf("Result.BoundVersion = %d, want 1 (the task's bound version, not current)", body.Result.BoundVersion)
	}
	if body.CurrentVersion != 2 {
		t.Errorf("CurrentVersion = %d, want 2", body.CurrentVersion)
	}
	if body.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
	if body.Event == nil {
		t.Fatal("Event is nil, want a ToolResultRejected event")
	}
	if body.Event.Type != "ToolResultRejected" {
		t.Errorf("Event.Type = %q, want %q", body.Event.Type, "ToolResultRejected")
	}
}

func TestSimulateAndEvaluateResult_TaskNotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/tasks/does-not-exist/result", simulateResultRequest{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
