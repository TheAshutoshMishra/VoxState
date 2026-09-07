package events

import "testing"

func TestNew_SetsFieldsAndUniqueIDs(t *testing.T) {
	a := New(TypeMachineCreated, "machine-1", map[string]any{"name": "Machine 17"})
	b := New(TypeMachineCreated, "machine-1", map[string]any{"name": "Machine 17"})

	if a.ID == "" {
		t.Error("ID is empty")
	}
	if a.ID == b.ID {
		t.Errorf("expected unique IDs, got same ID twice: %q", a.ID)
	}
	if a.Type != TypeMachineCreated {
		t.Errorf("Type = %q, want %q", a.Type, TypeMachineCreated)
	}
	if a.MachineID != "machine-1" {
		t.Errorf("MachineID = %q, want %q", a.MachineID, "machine-1")
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if a.ResultingStateVersion != nil {
		t.Error("ResultingStateVersion should be nil until explicitly set by the caller")
	}
}
