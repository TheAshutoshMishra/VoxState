// Package state is the authoritative in-memory engine for machines and
// their versioned state. It owns the current MachineState per machine and
// the rule that every meaningful change produces a new, immutable
// StateVersion (see docs/DOMAIN_MODEL.md and the "state-versioning rule"
// in CLAUDE.md).
//
// Storage is in-memory only (a single mutex-guarded map). The Store's
// public API is storage-agnostic on purpose — every method operates on
// machine IDs and domain values, never on a database connection or SQL —
// so a later milestone can back it with PostgreSQL without changing how
// callers use it.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"voxstate/backend/internal/events"
)

// ChangeSource identifies who/what is asserting a state change. It exists
// so a technician-reported change can be distinguished from a routine
// system/sensor update — see TechnicianReported in docs/EVENT_MODEL.md.
type ChangeSource string

const (
	SourceSystem     ChangeSource = "system"
	SourceTechnician ChangeSource = "technician"
)

// CreateMachineInput describes a new machine and its initial state.
type CreateMachineInput struct {
	// ID is optional. If empty, the store generates one.
	ID                string
	Name              string
	Type              string
	Location          string
	InitialStatus     Status // defaults to StatusRunning if empty
	InitialAttributes map[string]string
}

// StateChangeInput describes a requested change to a machine's state.
// Status and Attributes are patches: a nil Status leaves status unchanged,
// and only the keys present in Attributes are overwritten (others are
// carried forward from the current state).
type StateChangeInput struct {
	Status     *Status
	Attributes map[string]string
	// Source defaults to SourceSystem if empty. SourceTechnician also
	// emits a TechnicianReported event alongside MachineStateChanged.
	Source ChangeSource
	Note   string
}

type machineEntry struct {
	machine  Machine
	versions []MachineState // versions[0] is version 1; append-only
}

// Store is the in-memory state engine. It is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	machines map[string]*machineEntry
}

// NewStore creates an empty in-memory Store.
func NewStore() *Store {
	return &Store{machines: make(map[string]*machineEntry)}
}

// CreateMachine registers a new machine and establishes its StateVersion 1,
// returning the created Machine, its initial MachineState, and the
// MachineCreated event describing it.
func (s *Store) CreateMachine(input CreateMachineInput) (Machine, MachineState, events.Event, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Machine{}, MachineState{}, events.Event{}, ErrNameRequired
	}

	status := input.InitialStatus
	if status == "" {
		status = StatusRunning
	}
	if !status.Valid() {
		return Machine{}, MachineState{}, events.Event{}, ErrInvalidStatus
	}

	attrs := cloneAttributes(input.InitialAttributes)

	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = s.generateMachineID()
	} else if _, exists := s.machines[id]; exists {
		return Machine{}, MachineState{}, events.Event{}, ErrMachineAlreadyExists
	}

	now := time.Now().UTC()
	machine := Machine{
		ID:        id,
		Name:      name,
		Type:      strings.TrimSpace(input.Type),
		Location:  strings.TrimSpace(input.Location),
		CreatedAt: now,
	}

	ev := events.New(events.TypeMachineCreated, id, map[string]any{
		"name":   machine.Name,
		"status": string(status),
	})
	initialVersion := 1
	ev.ResultingStateVersion = &initialVersion

	st := MachineState{
		StateVersion: StateVersion{
			MachineID:       id,
			Version:         initialVersion,
			CreatedAt:       now,
			CausedByEventID: ev.ID,
		},
		Status:     status,
		Attributes: attrs,
	}

	s.machines[id] = &machineEntry{machine: machine, versions: []MachineState{st}}

	return machine, cloneState(st), ev, nil
}

// GetMachine returns a machine's static metadata.
func (s *Store) GetMachine(id string) (Machine, error) {
	if strings.TrimSpace(id) == "" {
		return Machine{}, ErrMachineIDRequired
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.machines[id]
	if !ok {
		return Machine{}, ErrMachineNotFound
	}
	return entry.machine, nil
}

// GetCurrentState returns the newest MachineState for a machine.
func (s *Store) GetCurrentState(machineID string) (MachineState, error) {
	if strings.TrimSpace(machineID) == "" {
		return MachineState{}, ErrMachineIDRequired
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.machines[machineID]
	if !ok {
		return MachineState{}, ErrMachineNotFound
	}
	return cloneState(entry.versions[len(entry.versions)-1]), nil
}

// GetStateVersion returns a specific, immutable historical MachineState.
// This is what lets a later caller (e.g. a diagnostic task) answer
// "exactly which machine state was current when this operation started?"
// even after the machine has moved on to newer versions.
func (s *Store) GetStateVersion(machineID string, version int) (MachineState, error) {
	if strings.TrimSpace(machineID) == "" {
		return MachineState{}, ErrMachineIDRequired
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.machines[machineID]
	if !ok {
		return MachineState{}, ErrMachineNotFound
	}
	if version < 1 || version > len(entry.versions) {
		return MachineState{}, ErrVersionNotFound
	}
	return cloneState(entry.versions[version-1]), nil
}

// ChangeState applies a state change to a machine. If the change actually
// alters status or attributes, it creates a new immutable StateVersion,
// preserves all previous versions untouched, and returns the events
// describing what happened (changed == true). If the requested change
// would leave the state identical to the current version, no new version
// is created and changed is false, per the no-op rule in the M2 spec.
func (s *Store) ChangeState(machineID string, input StateChangeInput) (result MachineState, changed bool, evs []events.Event, err error) {
	if strings.TrimSpace(machineID) == "" {
		return MachineState{}, false, nil, ErrMachineIDRequired
	}
	if input.Status != nil && !input.Status.Valid() {
		return MachineState{}, false, nil, ErrInvalidStatus
	}
	source := input.Source
	if source == "" {
		source = SourceSystem
	}
	if source != SourceSystem && source != SourceTechnician {
		return MachineState{}, false, nil, ErrInvalidSource
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.machines[machineID]
	if !ok {
		return MachineState{}, false, nil, ErrMachineNotFound
	}

	current := entry.versions[len(entry.versions)-1]

	newStatus := current.Status
	if input.Status != nil {
		newStatus = *input.Status
	}

	newAttrs := cloneAttributes(current.Attributes)
	for k, v := range input.Attributes {
		newAttrs[k] = v
	}

	if newStatus == current.Status && attributesEqual(newAttrs, current.Attributes) {
		return cloneState(current), false, nil, nil
	}

	now := time.Now().UTC()
	newVersion := current.Version + 1

	changeEv := events.New(events.TypeMachineStateChanged, machineID, map[string]any{
		"previous_status":  string(current.Status),
		"new_status":       string(newStatus),
		"previous_version": current.Version,
		"source":           string(source),
	})
	changeEv.ResultingStateVersion = &newVersion

	if source == SourceTechnician {
		techEv := events.New(events.TypeTechnicianReported, machineID, map[string]any{
			"note": input.Note,
		})
		evs = append(evs, techEv)
	}
	evs = append(evs, changeEv)

	newState := MachineState{
		StateVersion: StateVersion{
			MachineID:       machineID,
			Version:         newVersion,
			CreatedAt:       now,
			CausedByEventID: changeEv.ID,
		},
		Status:     newStatus,
		Attributes: newAttrs,
	}

	entry.versions = append(entry.versions, newState)

	return cloneState(newState), true, evs, nil
}

// generateMachineID must be called with s.mu held.
func (s *Store) generateMachineID() string {
	for i := 0; i < 5; i++ {
		id := "machine-" + randomHex(6)
		if _, exists := s.machines[id]; !exists {
			return id
		}
	}
	// Astronomically unlikely to be reached (5 collisions on a 48-bit
	// random suffix); fall back to a longer suffix rather than looping
	// forever.
	return "machine-" + randomHex(12)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func cloneAttributes(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

func attributesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// cloneState returns a copy of st with its Attributes map deep-copied, so
// callers can never mutate the Store's internal history through a value
// they were handed.
func cloneState(st MachineState) MachineState {
	st.Attributes = cloneAttributes(st.Attributes)
	return st
}
