package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateMachine_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{
		Name:       "Machine 17",
		Type:       "CNC mill",
		Location:   "Floor 2",
		Attributes: map[string]string{"temperature": "72"},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var body struct {
		Machine machineResponse `json:"machine"`
		State   stateResponse   `json:"state"`
		Event   eventResponse   `json:"event"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Machine.ID == "" {
		t.Error("Machine.ID is empty")
	}
	if body.Machine.Name != "Machine 17" {
		t.Errorf("Machine.Name = %q, want %q", body.Machine.Name, "Machine 17")
	}
	if body.State.Version != 1 {
		t.Errorf("State.Version = %d, want 1", body.State.Version)
	}
	if body.Event.Type != "MachineCreated" {
		t.Errorf("Event.Type = %q, want %q", body.Event.Type, "MachineCreated")
	}
}

func TestCreateMachine_MissingName_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetMachine_HTTP(t *testing.T) {
	router := newTestRouter()

	createRec := doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	rec := doJSON(t, router, http.MethodGet, "/machines/machine-17", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got machineResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "machine-17" {
		t.Errorf("ID = %q, want %q", got.ID, "machine-17")
	}
}

func TestGetMachine_NotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodGet, "/machines/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestChangeStateAndGetState_HTTP(t *testing.T) {
	router := newTestRouter()

	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{
		ID:         "machine-17",
		Name:       "Machine 17",
		Attributes: map[string]string{"vibration": "NORMAL"},
	})

	changeRec := doJSON(t, router, http.MethodPost, "/machines/machine-17/state", changeStateRequest{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	})
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change status = %d, want %d, body = %s", changeRec.Code, http.StatusOK, changeRec.Body.String())
	}

	var changeBody changeStateResponse
	if err := json.NewDecoder(changeRec.Body).Decode(&changeBody); err != nil {
		t.Fatalf("decode change response: %v", err)
	}
	if !changeBody.Changed {
		t.Error("Changed = false, want true")
	}
	if changeBody.State.Version != 2 {
		t.Errorf("State.Version = %d, want 2", changeBody.State.Version)
	}
	if len(changeBody.Events) == 0 {
		t.Error("Events is empty, want at least a MachineStateChanged event")
	}

	// Current state should now be v2.
	currentRec := doJSON(t, router, http.MethodGet, "/machines/machine-17/state", nil)
	var currentBody stateResponse
	if err := json.NewDecoder(currentRec.Body).Decode(&currentBody); err != nil {
		t.Fatalf("decode current state response: %v", err)
	}
	if currentBody.Version != 2 {
		t.Errorf("current Version = %d, want 2", currentBody.Version)
	}
	if currentBody.Attributes["vibration"] != "CRITICAL" {
		t.Errorf("current Attributes[vibration] = %q, want %q", currentBody.Attributes["vibration"], "CRITICAL")
	}

	// v1 must remain immutable and independently retrievable.
	v1Rec := doJSON(t, router, http.MethodGet, "/machines/machine-17/state?version=1", nil)
	if v1Rec.Code != http.StatusOK {
		t.Fatalf("v1 status = %d, want %d, body = %s", v1Rec.Code, http.StatusOK, v1Rec.Body.String())
	}
	var v1Body stateResponse
	if err := json.NewDecoder(v1Rec.Body).Decode(&v1Body); err != nil {
		t.Fatalf("decode v1 response: %v", err)
	}
	if v1Body.Version != 1 {
		t.Errorf("v1 Version = %d, want 1", v1Body.Version)
	}
	if v1Body.Attributes["vibration"] != "NORMAL" {
		t.Errorf("v1 Attributes[vibration] = %q, want unchanged %q", v1Body.Attributes["vibration"], "NORMAL")
	}
}

func TestChangeState_NotFound_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodPost, "/machines/does-not-exist/state", changeStateRequest{
		Attributes: map[string]string{"x": "y"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestChangeState_NoOp_HTTP(t *testing.T) {
	router := newTestRouter()

	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{
		ID:     "machine-17",
		Name:   "Machine 17",
		Status: "running",
	})

	rec := doJSON(t, router, http.MethodPost, "/machines/machine-17/state", changeStateRequest{
		Status: "running",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body changeStateResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Changed {
		t.Error("Changed = true, want false for a no-op status update")
	}
	if body.State.Version != 1 {
		t.Errorf("State.Version = %d, want unchanged 1", body.State.Version)
	}
}

func TestGetState_VersionOutOfRange_HTTP(t *testing.T) {
	router := newTestRouter()

	doJSON(t, router, http.MethodPost, "/machines", createMachineRequest{ID: "machine-17", Name: "Machine 17"})

	rec := doJSON(t, router, http.MethodGet, "/machines/machine-17/state?version=99", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
