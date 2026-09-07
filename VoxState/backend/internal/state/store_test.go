package state

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"voxstate/backend/internal/events"
)

func TestCreateMachine_Success(t *testing.T) {
	s := NewStore()

	machine, st, ev, err := s.CreateMachine(CreateMachineInput{
		Name:              "Machine 17",
		Type:              "CNC mill",
		Location:          "Floor 2",
		InitialStatus:     StatusRunning,
		InitialAttributes: map[string]string{"temperature": "72", "pressure": "8"},
	})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}

	if machine.ID == "" {
		t.Error("machine.ID is empty, want a generated stable ID")
	}
	if machine.Name != "Machine 17" {
		t.Errorf("machine.Name = %q, want %q", machine.Name, "Machine 17")
	}
	if st.Version != 1 {
		t.Errorf("initial Version = %d, want 1", st.Version)
	}
	if st.Status != StatusRunning {
		t.Errorf("initial Status = %q, want %q", st.Status, StatusRunning)
	}
	if st.Attributes["temperature"] != "72" {
		t.Errorf("initial Attributes[temperature] = %q, want %q", st.Attributes["temperature"], "72")
	}
	if ev.Type != events.TypeMachineCreated {
		t.Errorf("event Type = %q, want %q", ev.Type, events.TypeMachineCreated)
	}
	if ev.ResultingStateVersion == nil || *ev.ResultingStateVersion != 1 {
		t.Errorf("event ResultingStateVersion = %v, want pointer to 1", ev.ResultingStateVersion)
	}
}

func TestCreateMachine_DefaultsStatusToRunning(t *testing.T) {
	s := NewStore()

	_, st, _, err := s.CreateMachine(CreateMachineInput{Name: "Machine 1"})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	if st.Status != StatusRunning {
		t.Errorf("Status = %q, want default %q", st.Status, StatusRunning)
	}
}

func TestCreateMachine_MissingName(t *testing.T) {
	s := NewStore()

	_, _, _, err := s.CreateMachine(CreateMachineInput{})
	if !errors.Is(err, ErrNameRequired) {
		t.Errorf("err = %v, want ErrNameRequired", err)
	}
}

func TestCreateMachine_InvalidInitialStatus(t *testing.T) {
	s := NewStore()

	_, _, _, err := s.CreateMachine(CreateMachineInput{Name: "M", InitialStatus: "not-a-status"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("err = %v, want ErrInvalidStatus", err)
	}
}

func TestCreateMachine_DuplicateExplicitID(t *testing.T) {
	s := NewStore()

	_, _, _, err := s.CreateMachine(CreateMachineInput{ID: "machine-17", Name: "Machine 17"})
	if err != nil {
		t.Fatalf("first CreateMachine() error = %v", err)
	}

	_, _, _, err = s.CreateMachine(CreateMachineInput{ID: "machine-17", Name: "Duplicate"})
	if !errors.Is(err, ErrMachineAlreadyExists) {
		t.Errorf("err = %v, want ErrMachineAlreadyExists", err)
	}
}

func TestGetMachine_NotFound(t *testing.T) {
	s := NewStore()

	_, err := s.GetMachine("does-not-exist")
	if !errors.Is(err, ErrMachineNotFound) {
		t.Errorf("err = %v, want ErrMachineNotFound", err)
	}
}

func TestGetMachine_MissingID(t *testing.T) {
	s := NewStore()

	_, err := s.GetMachine("")
	if !errors.Is(err, ErrMachineIDRequired) {
		t.Errorf("err = %v, want ErrMachineIDRequired", err)
	}
}

func TestChangeState_CreatesNewVersionAndUpdatesCurrent(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{
		ID:                "machine-17",
		Name:              "Machine 17",
		InitialAttributes: map[string]string{"vibration": "NORMAL", "motor": "RUNNING"},
	})

	newState, changed, evs, err := s.ChangeState("machine-17", StateChangeInput{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if newState.Version != 2 {
		t.Errorf("Version = %d, want 2", newState.Version)
	}
	if newState.Attributes["vibration"] != "CRITICAL" {
		t.Errorf("Attributes[vibration] = %q, want %q", newState.Attributes["vibration"], "CRITICAL")
	}
	if newState.Attributes["motor"] != "RUNNING" {
		t.Errorf("Attributes[motor] = %q, want carried-forward value %q", newState.Attributes["motor"], "RUNNING")
	}

	foundChanged := false
	for _, e := range evs {
		if e.Type == events.TypeMachineStateChanged {
			foundChanged = true
			if e.ResultingStateVersion == nil || *e.ResultingStateVersion != 2 {
				t.Errorf("MachineStateChanged.ResultingStateVersion = %v, want pointer to 2", e.ResultingStateVersion)
			}
		}
	}
	if !foundChanged {
		t.Error("expected a MachineStateChanged event, got none")
	}

	current, err := s.GetCurrentState("machine-17")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}
	if current.Version != 2 {
		t.Errorf("current.Version = %d, want 2", current.Version)
	}
}

func TestChangeState_VersionsIncreaseMonotonically(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{ID: "m1", Name: "M1"})

	wantVersions := []int{2, 3, 4}
	for i, want := range wantVersions {
		st, changed, _, err := s.ChangeState("m1", StateChangeInput{
			Attributes: map[string]string{"step": fmt.Sprintf("%d", i)},
		})
		if err != nil {
			t.Fatalf("ChangeState() #%d error = %v", i, err)
		}
		if !changed {
			t.Fatalf("ChangeState() #%d changed = false, want true", i)
		}
		if st.Version != want {
			t.Errorf("ChangeState() #%d Version = %d, want %d", i, st.Version, want)
		}
	}
}

func TestChangeState_PreviousVersionRemainsImmutable(t *testing.T) {
	s := NewStore()
	_, v1, _, _ := s.CreateMachine(CreateMachineInput{
		ID:                "m1",
		Name:              "M1",
		InitialAttributes: map[string]string{"vibration": "NORMAL"},
	})

	_, _, _, err := s.ChangeState("m1", StateChangeInput{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}

	gotV1, err := s.GetStateVersion("m1", 1)
	if err != nil {
		t.Fatalf("GetStateVersion(1) error = %v", err)
	}
	if gotV1.Attributes["vibration"] != "NORMAL" {
		t.Errorf("v1 Attributes[vibration] = %q, want unchanged %q", gotV1.Attributes["vibration"], "NORMAL")
	}
	if gotV1.Version != v1.Version {
		t.Errorf("v1 Version = %d, want %d", gotV1.Version, v1.Version)
	}

	// Mutating the returned map must not corrupt the store's history.
	gotV1.Attributes["vibration"] = "TAMPERED"
	gotV1Again, err := s.GetStateVersion("m1", 1)
	if err != nil {
		t.Fatalf("GetStateVersion(1) second call error = %v", err)
	}
	if gotV1Again.Attributes["vibration"] != "NORMAL" {
		t.Errorf("store history was mutated via a returned map copy: got %q", gotV1Again.Attributes["vibration"])
	}
}

func TestChangeState_NoOpDoesNotCreateNewVersion(t *testing.T) {
	s := NewStore()
	status := StatusRunning
	_, _, _, _ = s.CreateMachine(CreateMachineInput{
		ID:                "m1",
		Name:              "M1",
		InitialStatus:     status,
		InitialAttributes: map[string]string{"motor": "RUNNING"},
	})

	result, changed, evs, err := s.ChangeState("m1", StateChangeInput{
		Status:     &status,
		Attributes: map[string]string{"motor": "RUNNING"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if changed {
		t.Error("changed = true, want false for a no-op update")
	}
	if len(evs) != 0 {
		t.Errorf("len(evs) = %d, want 0 for a no-op update", len(evs))
	}
	if result.Version != 1 {
		t.Errorf("Version = %d, want unchanged 1", result.Version)
	}

	current, _ := s.GetCurrentState("m1")
	if current.Version != 1 {
		t.Errorf("current.Version = %d, want still 1 after no-op", current.Version)
	}
}

func TestChangeState_MachineNotFound(t *testing.T) {
	s := NewStore()

	_, _, _, err := s.ChangeState("does-not-exist", StateChangeInput{})
	if !errors.Is(err, ErrMachineNotFound) {
		t.Errorf("err = %v, want ErrMachineNotFound", err)
	}
}

func TestChangeState_InvalidStatus(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{ID: "m1", Name: "M1"})

	bad := Status("not-a-status")
	_, _, _, err := s.ChangeState("m1", StateChangeInput{Status: &bad})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("err = %v, want ErrInvalidStatus", err)
	}
}

func TestChangeState_InvalidSource(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{ID: "m1", Name: "M1"})

	_, _, _, err := s.ChangeState("m1", StateChangeInput{
		Attributes: map[string]string{"x": "y"},
		Source:     "not-a-source",
	})
	if !errors.Is(err, ErrInvalidSource) {
		t.Errorf("err = %v, want ErrInvalidSource", err)
	}
}

func TestChangeState_TechnicianReportEmitsBothEvents(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{
		ID:                "m1",
		Name:              "M1",
		InitialAttributes: map[string]string{"motor": "STOPPED"},
	})

	_, changed, evs, err := s.ChangeState("m1", StateChangeInput{
		Attributes: map[string]string{"motor": "RUNNING"},
		Source:     SourceTechnician,
		Note:       "Motor replaced",
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if len(evs) != 2 {
		t.Fatalf("len(evs) = %d, want 2 (TechnicianReported + MachineStateChanged)", len(evs))
	}
	if evs[0].Type != events.TypeTechnicianReported {
		t.Errorf("evs[0].Type = %q, want %q", evs[0].Type, events.TypeTechnicianReported)
	}
	if evs[0].Payload["note"] != "Motor replaced" {
		t.Errorf("evs[0].Payload[note] = %v, want %q", evs[0].Payload["note"], "Motor replaced")
	}
	if evs[1].Type != events.TypeMachineStateChanged {
		t.Errorf("evs[1].Type = %q, want %q", evs[1].Type, events.TypeMachineStateChanged)
	}
}

func TestGetStateVersion_OutOfRange(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{ID: "m1", Name: "M1"})

	_, err := s.GetStateVersion("m1", 99)
	if !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("err = %v, want ErrVersionNotFound", err)
	}

	_, err = s.GetStateVersion("m1", 0)
	if !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("err = %v, want ErrVersionNotFound", err)
	}
}

func TestChangeState_ConcurrentUpdatesProduceUniqueSequentialVersions(t *testing.T) {
	s := NewStore()
	_, _, _, _ = s.CreateMachine(CreateMachineInput{ID: "m1", Name: "M1"})

	const workers = 50
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine sets a unique attribute key so the change is
			// guaranteed to be a real diff regardless of interleaving —
			// this avoids flaky no-op races between concurrent workers.
			_, changed, _, err := s.ChangeState("m1", StateChangeInput{
				Attributes: map[string]string{fmt.Sprintf("worker_%d", i): "done"},
			})
			if err != nil {
				errCh <- err
				return
			}
			if !changed {
				errCh <- fmt.Errorf("worker %d: changed = false, want true", i)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	current, err := s.GetCurrentState("m1")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}
	if current.Version != workers+1 {
		t.Errorf("final Version = %d, want %d (1 initial + %d changes)", current.Version, workers+1, workers)
	}

	// Verify every version 1..final is present, unique, and immutable.
	seen := make(map[int]bool)
	for v := 1; v <= current.Version; v++ {
		st, err := s.GetStateVersion("m1", v)
		if err != nil {
			t.Fatalf("GetStateVersion(%d) error = %v", v, err)
		}
		if st.Version != v {
			t.Errorf("GetStateVersion(%d) returned Version = %d", v, st.Version)
		}
		if seen[st.Version] {
			t.Errorf("duplicate version detected: %d", st.Version)
		}
		seen[st.Version] = true
	}
}
