package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCreateTask_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	rec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{ToolName: "vibration_scan"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var body struct {
		Task  taskResponse  `json:"task"`
		Event eventResponse `json:"event"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Task.ID == "" {
		t.Error("Task.ID is empty")
	}
	if body.Task.MachineID != "machine-17" {
		t.Errorf("Task.MachineID = %q, want %q", body.Task.MachineID, "machine-17")
	}
	if body.Task.BoundVersion != 1 {
		t.Errorf("Task.BoundVersion = %d, want 1", body.Task.BoundVersion)
	}
	if body.Task.Status != "PENDING" {
		t.Errorf("Task.Status = %q, want %q", body.Task.Status, "PENDING")
	}
	if body.Event.Type != "DiagnosticStarted" {
		t.Errorf("Event.Type = %q, want %q", body.Event.Type, "DiagnosticStarted")
	}
}

func TestCreateTask_MachineNotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines/does-not-exist/tasks", createTaskRequest{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestTaskBoundVersion_SurvivesMachineStateChange_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)
	if createBody.Task.BoundVersion != 1 {
		t.Fatalf("BoundVersion = %d, want 1", createBody.Task.BoundVersion)
	}

	changeRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/state", changeStateRequest{
		Status: "fault",
	})
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change status = %d, body = %s", changeRec.Code, changeRec.Body.String())
	}

	getRec := doJSON(t, router, http.MethodGet, "/tasks/"+createBody.Task.ID, nil)
	var getBody taskResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if getBody.BoundVersion != 1 {
		t.Errorf("BoundVersion after machine moved on = %d, want unchanged 1", getBody.BoundVersion)
	}
}

func TestGetTask_NotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodGet, "/tasks/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestListTasksForMachine_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})
	doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})

	rec := doJSON(t, router, http.MethodGet, "/machines/machine-17/tasks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var list []taskResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
}

func TestStartAndCancelTask_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)
	taskID := createBody.Task.ID

	startRec := doJSON(t, router, http.MethodPost, "/tasks/"+taskID+"/start", startTaskRequest{DurationSeconds: 5})
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d, body = %s", startRec.Code, http.StatusOK, startRec.Body.String())
	}
	var startBody taskResponse
	_ = json.NewDecoder(startRec.Body).Decode(&startBody)
	if startBody.Status != "RUNNING" {
		t.Errorf("Status after start = %q, want %q", startBody.Status, "RUNNING")
	}
	if startBody.StartedAt == "" {
		t.Error("StartedAt is empty after start")
	}

	// Give the background goroutine a moment to actually be inside the
	// simulated work before we cancel it.
	time.Sleep(20 * time.Millisecond)

	cancelRec := doJSON(t, router, http.MethodPost, "/tasks/"+taskID+"/cancel", nil)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d, body = %s", cancelRec.Code, http.StatusOK, cancelRec.Body.String())
	}
	var cancelBody struct {
		Task  taskResponse  `json:"task"`
		Event eventResponse `json:"event"`
	}
	if err := json.NewDecoder(cancelRec.Body).Decode(&cancelBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cancelBody.Task.Status != "CANCELLED" {
		t.Errorf("Status after cancel = %q, want %q", cancelBody.Task.Status, "CANCELLED")
	}
	if cancelBody.Event.Type != "DiagnosticCancelled" {
		t.Errorf("Event.Type = %q, want %q", cancelBody.Event.Type, "DiagnosticCancelled")
	}
}

func TestCancelTask_AlreadyCancelled_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})
	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)

	firstRec := doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/cancel", nil)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first cancel status = %d, body = %s", firstRec.Code, firstRec.Body.String())
	}

	secondRec := doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/cancel", nil)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("second cancel status = %d, want %d, body = %s", secondRec.Code, http.StatusConflict, secondRec.Body.String())
	}
}

func TestStartTask_InvalidTransition_HTTP(t *testing.T) {
	router := newTestRouter()
	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})
	createRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/tasks", createTaskRequest{})
	var createBody struct {
		Task taskResponse `json:"task"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createBody)

	doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/cancel", nil)

	rec := doJSON(t, router, http.MethodPost, "/tasks/"+createBody.Task.ID+"/start", startTaskRequest{DurationSeconds: 1})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
