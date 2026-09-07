// Package tasks is the in-memory task engine: it creates DiagnosticTasks
// bound to a specific machine StateVersion and manages their lifecycle
// (PENDING -> RUNNING -> COMPLETED, with CANCELLED reachable from either
// PENDING or RUNNING). It depends on internal/state to read a machine's
// current version at task-creation time — state never depends on tasks,
// keeping the dependency direction api -> tasks -> state.
//
// M3 does not implement stale-result rejection: a task's BoundVersion is
// recorded once and never changed, even after the machine moves on to a
// newer version. Deciding what to do about that mismatch is the Policy
// layer's job (M4), not this package's.
package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"voxstate/backend/internal/events"
	"voxstate/backend/internal/state"
)

// CreateTaskInput describes a task to create.
type CreateTaskInput struct {
	MachineID string
	// ToolName is a label for what kind of diagnostic this task
	// represents. M3 does not execute real tools, so this is metadata
	// only — it defaults to "diagnostic" if empty.
	ToolName string
}

type taskEntry struct {
	task   DiagnosticTask
	cancel context.CancelFunc // set once Start succeeds; nil until then
}

// Store is the in-memory task engine. It is safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	tasks  map[string]*taskEntry
	state  *state.Store
	logger *slog.Logger
}

// NewStore creates an in-memory task Store. stateStore is the single
// source of truth for machine/version lookups; logger is optional (nil is
// safe) and, if provided, receives one structured log line per task
// lifecycle event produced — this is M3's substitute for a persistent
// event log, which does not exist yet.
func NewStore(stateStore *state.Store, logger *slog.Logger) *Store {
	return &Store{
		tasks:  make(map[string]*taskEntry),
		state:  stateStore,
		logger: logger,
	}
}

// CreateTask creates a new DiagnosticTask bound to the machine's current
// StateVersion at the moment of the call. The version is read once, from
// internal/state, and never re-checked or updated for the lifetime of the
// task — that immutability is the invariant M4's stale-result policy
// depends on.
func (s *Store) CreateTask(input CreateTaskInput) (DiagnosticTask, events.Event, error) {
	machineID := strings.TrimSpace(input.MachineID)
	if machineID == "" {
		return DiagnosticTask{}, events.Event{}, ErrMachineIDRequired
	}

	current, err := s.state.GetCurrentState(machineID)
	if err != nil {
		// Propagates state.ErrMachineNotFound / state.ErrMachineIDRequired
		// unchanged — tasks does not redefine machine-lookup errors.
		return DiagnosticTask{}, events.Event{}, err
	}

	toolName := strings.TrimSpace(input.ToolName)
	if toolName == "" {
		toolName = "diagnostic"
	}

	s.mu.Lock()
	id := s.generateTaskID()
	task := DiagnosticTask{
		ID:           id,
		MachineID:    machineID,
		ToolName:     toolName,
		BoundVersion: current.Version,
		Status:       StatusPending,
		CreatedAt:    time.Now().UTC(),
	}
	s.tasks[id] = &taskEntry{task: task}
	s.mu.Unlock()

	ev := events.New(events.TypeDiagnosticStarted, machineID, map[string]any{
		"task_id":       id,
		"bound_version": task.BoundVersion,
		"tool_name":     toolName,
	})
	s.logEvent(ev)

	return task, ev, nil
}

// GetTask returns a task by ID.
func (s *Store) GetTask(id string) (DiagnosticTask, error) {
	if strings.TrimSpace(id) == "" {
		return DiagnosticTask{}, ErrTaskIDRequired
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	te, ok := s.tasks[id]
	if !ok {
		return DiagnosticTask{}, ErrTaskNotFound
	}
	return te.task, nil
}

// ListTasksForMachine returns all tasks created for a machine, oldest
// first. An unknown machine ID simply yields an empty slice.
func (s *Store) ListTasksForMachine(machineID string) ([]DiagnosticTask, error) {
	if strings.TrimSpace(machineID) == "" {
		return nil, ErrMachineIDRequired
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []DiagnosticTask
	for _, te := range s.tasks {
		if te.task.MachineID == machineID {
			out = append(out, te.task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// StartTask transitions a PENDING task to RUNNING and executes work
// asynchronously, deriving a cancellable context that CancelTask controls.
// It returns immediately with the RUNNING snapshot; work continues in the
// background.
func (s *Store) StartTask(id string, work Work) (DiagnosticTask, error) {
	if strings.TrimSpace(id) == "" {
		return DiagnosticTask{}, ErrTaskIDRequired
	}
	if work == nil {
		return DiagnosticTask{}, ErrWorkRequired
	}

	s.mu.Lock()
	te, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return DiagnosticTask{}, ErrTaskNotFound
	}
	if te.task.Status != StatusPending {
		status := te.task.Status
		s.mu.Unlock()
		return DiagnosticTask{}, fmt.Errorf("%w: cannot start a task in status %s", ErrInvalidTransition, status)
	}

	ctx, cancel := context.WithCancel(context.Background())
	te.cancel = cancel
	te.task.Status = StatusRunning
	te.task.StartedAt = time.Now().UTC()
	snapshot := te.task
	s.mu.Unlock()

	go s.runAndFinish(id, ctx, work)

	return snapshot, nil
}

// CancelTask moves a PENDING or RUNNING task to CANCELLED and, if the
// task was RUNNING, cancels its context so the in-flight Work observes
// ctx.Done() and stops. Cancelling an already-terminal task returns the
// current snapshot alongside a sentinel error identifying which terminal
// state it was already in, rather than silently succeeding — callers need
// to be able to distinguish "you cancelled it" from "it was already done".
func (s *Store) CancelTask(id string) (DiagnosticTask, events.Event, error) {
	if strings.TrimSpace(id) == "" {
		return DiagnosticTask{}, events.Event{}, ErrTaskIDRequired
	}

	s.mu.Lock()
	te, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return DiagnosticTask{}, events.Event{}, ErrTaskNotFound
	}

	switch te.task.Status {
	case StatusCompleted:
		snapshot := te.task
		s.mu.Unlock()
		return snapshot, events.Event{}, ErrTaskAlreadyCompleted
	case StatusCancelled:
		snapshot := te.task
		s.mu.Unlock()
		return snapshot, events.Event{}, ErrTaskAlreadyCancelled
	}

	previousStatus := te.task.Status
	te.task.Status = StatusCancelled
	te.task.FinishedAt = time.Now().UTC()
	cancel := te.cancel
	snapshot := te.task
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	ev := events.New(events.TypeDiagnosticCancelled, snapshot.MachineID, map[string]any{
		"task_id":         snapshot.ID,
		"bound_version":   snapshot.BoundVersion,
		"previous_status": string(previousStatus),
	})
	s.logEvent(ev)

	return snapshot, ev, nil
}

// runAndFinish executes work and reports its outcome. It runs in its own
// goroutine, started by StartTask.
func (s *Store) runAndFinish(id string, ctx context.Context, work Work) {
	err := work(ctx)
	s.finishTask(id, err)
}

// finishTask completes a task, but only if it is still RUNNING — this is
// what makes the CompleteTask-vs-CancelTask race safe: whichever of
// CancelTask or finishTask acquires the lock first while the task is
// still RUNNING wins, and the other becomes a no-op against an
// already-terminal task. A task can therefore never observe
// COMPLETED -> CANCELLED or CANCELLED -> COMPLETED.
func (s *Store) finishTask(id string, workErr error) {
	s.mu.Lock()
	te, ok := s.tasks[id]
	if !ok || te.task.Status != StatusRunning {
		s.mu.Unlock()
		return
	}
	te.task.Status = StatusCompleted
	te.task.FinishedAt = time.Now().UTC()
	snapshot := te.task
	s.mu.Unlock()

	payload := map[string]any{
		"task_id":       snapshot.ID,
		"bound_version": snapshot.BoundVersion,
		"tool_name":     snapshot.ToolName,
	}
	if workErr != nil && !errors.Is(workErr, context.Canceled) {
		payload["work_error"] = workErr.Error()
	}

	ev := events.New(events.TypeDiagnosticCompleted, snapshot.MachineID, payload)
	s.logEvent(ev)
}

func (s *Store) logEvent(ev events.Event) {
	if s.logger == nil {
		return
	}
	s.logger.Info("task lifecycle event",
		"event_type", ev.Type,
		"event_id", ev.ID,
		"machine_id", ev.MachineID,
		"payload", ev.Payload,
	)
}

func (s *Store) generateTaskID() string {
	for i := 0; i < 5; i++ {
		id := "task-" + randomHex(6)
		if _, exists := s.tasks[id]; !exists {
			return id
		}
	}
	return "task-" + randomHex(12)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
