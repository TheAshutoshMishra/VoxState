package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"voxstate/backend/internal/state"
)

// newTestStores builds a fresh state.Store + tasks.Store pair, with a
// machine already created at v1, for tests that don't care about setup.
func newTestStores(t *testing.T) (*state.Store, *Store) {
	t.Helper()
	ss := state.NewStore()
	_, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	return ss, NewStore(ss, nil)
}

// blockingWork is a small, fully deterministic fake worker for tests: it
// signals on started once running, then blocks until either the test
// tells it to finish (via finish) or its context is cancelled — whichever
// happens first. This is test infrastructure only; production code uses
// the Work abstraction with real (later, M5+) implementations.
func blockingWork(started chan<- struct{}, finish <-chan struct{}) Work {
	return func(ctx context.Context) error {
		close(started)
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func TestCreateTask_Success(t *testing.T) {
	_, ts := newTestStores(t)

	task, ev, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17", ToolName: "vibration_scan"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID == "" {
		t.Error("task.ID is empty")
	}
	if task.MachineID != "machine-17" {
		t.Errorf("MachineID = %q, want %q", task.MachineID, "machine-17")
	}
	if task.BoundVersion != 1 {
		t.Errorf("BoundVersion = %d, want 1", task.BoundVersion)
	}
	if task.Status != StatusPending {
		t.Errorf("Status = %q, want %q", task.Status, StatusPending)
	}
	if task.ToolName != "vibration_scan" {
		t.Errorf("ToolName = %q, want %q", task.ToolName, "vibration_scan")
	}
	if ev.Type != "DiagnosticStarted" {
		t.Errorf("event Type = %q, want %q", ev.Type, "DiagnosticStarted")
	}
}

func TestCreateTask_DefaultsToolName(t *testing.T) {
	_, ts := newTestStores(t)

	task, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ToolName != "diagnostic" {
		t.Errorf("ToolName = %q, want default %q", task.ToolName, "diagnostic")
	}
}

func TestCreateTask_MachineNotFound(t *testing.T) {
	ss := state.NewStore()
	ts := NewStore(ss, nil)

	_, _, err := ts.CreateTask(CreateTaskInput{MachineID: "does-not-exist"})
	if !errors.Is(err, state.ErrMachineNotFound) {
		t.Errorf("err = %v, want state.ErrMachineNotFound", err)
	}
}

func TestCreateTask_MissingMachineID(t *testing.T) {
	_, ts := newTestStores(t)

	_, _, err := ts.CreateTask(CreateTaskInput{})
	if !errors.Is(err, ErrMachineIDRequired) {
		t.Errorf("err = %v, want ErrMachineIDRequired", err)
	}
}

func TestCreateTask_BoundVersionSurvivesMachineChange(t *testing.T) {
	ss, ts := newTestStores(t)

	task, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.BoundVersion != 1 {
		t.Fatalf("BoundVersion = %d, want 1", task.BoundVersion)
	}

	status := state.StatusFault
	_, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Status:     &status,
		Attributes: map[string]string{"vibration": "CRITICAL"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("expected the machine state change to take effect")
	}

	got, err := ts.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.BoundVersion != 1 {
		t.Errorf("BoundVersion after machine moved to v2 = %d, want unchanged 1", got.BoundVersion)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	_, ts := newTestStores(t)

	_, err := ts.GetTask("does-not-exist")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestListTasksForMachine(t *testing.T) {
	_, ts := newTestStores(t)

	var created []string
	for i := 0; i < 3; i++ {
		task, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})
		if err != nil {
			t.Fatalf("CreateTask() #%d error = %v", i, err)
		}
		created = append(created, task.ID)
	}

	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	for i, task := range list {
		if task.ID != created[i] {
			t.Errorf("list[%d].ID = %q, want %q (creation order)", i, task.ID, created[i])
		}
	}
}

func TestStartTask_TransitionsToRunning(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)

	running, err := ts.StartTask(task.ID, blockingWork(started, finish))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if running.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", running.Status, StatusRunning)
	}
	if running.StartedAt.IsZero() {
		t.Error("StartedAt is zero, want set")
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("work never started")
	}
}

func TestStartTask_RejectsNonPending(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)

	if _, err := ts.StartTask(task.ID, blockingWork(started, finish)); err != nil {
		t.Fatalf("first StartTask() error = %v", err)
	}
	<-started

	_, err := ts.StartTask(task.ID, blockingWork(make(chan struct{}), make(chan struct{})))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestStartTask_NotFound(t *testing.T) {
	_, ts := newTestStores(t)

	_, err := ts.StartTask("does-not-exist", SimulatedWork(time.Millisecond))
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestStartTask_NilWork(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	_, err := ts.StartTask(task.ID, nil)
	if !errors.Is(err, ErrWorkRequired) {
		t.Errorf("err = %v, want ErrWorkRequired", err)
	}
}

func TestTaskCompletes_WhenWorkFinishesNormally(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	started := make(chan struct{})
	finish := make(chan struct{})

	if _, err := ts.StartTask(task.ID, blockingWork(started, finish)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	<-started
	close(finish)

	waitForStatus(t, ts, task.ID, StatusCompleted)
}

func TestCancelTask_BeforeExecution(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	cancelled, ev, err := ts.CancelTask(task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != StatusCancelled {
		t.Errorf("Status = %q, want %q", cancelled.Status, StatusCancelled)
	}
	if cancelled.FinishedAt.IsZero() {
		t.Error("FinishedAt is zero, want set")
	}
	if ev.Type != "DiagnosticCancelled" {
		t.Errorf("event Type = %q, want %q", ev.Type, "DiagnosticCancelled")
	}

	// A task cancelled while PENDING must never be startable afterwards.
	_, err = ts.StartTask(task.ID, SimulatedWork(time.Millisecond))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("StartTask() after cancel err = %v, want ErrInvalidTransition", err)
	}
}

func TestCancelTask_DuringExecution_PropagatesContext(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	var ctxErr error
	var mu sync.Mutex
	observedDone := make(chan struct{})

	work := func(ctx context.Context) error {
		<-ctx.Done()
		mu.Lock()
		ctxErr = ctx.Err()
		mu.Unlock()
		close(observedDone)
		return ctx.Err()
	}

	if _, err := ts.StartTask(task.ID, work); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	cancelled, _, err := ts.CancelTask(task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != StatusCancelled {
		t.Errorf("Status = %q, want %q", cancelled.Status, StatusCancelled)
	}

	select {
	case <-observedDone:
	case <-time.After(time.Second):
		t.Fatal("running work never observed context cancellation")
	}

	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(ctxErr, context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctxErr)
	}
}

func TestCancelTask_NotFound(t *testing.T) {
	_, ts := newTestStores(t)

	_, _, err := ts.CancelTask("does-not-exist")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestCancelTask_AlreadyCancelled(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	if _, _, err := ts.CancelTask(task.ID); err != nil {
		t.Fatalf("first CancelTask() error = %v", err)
	}

	_, _, err := ts.CancelTask(task.ID)
	if !errors.Is(err, ErrTaskAlreadyCancelled) {
		t.Errorf("err = %v, want ErrTaskAlreadyCancelled", err)
	}
}

func TestCancelTask_AlreadyCompleted(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	started := make(chan struct{})
	finish := make(chan struct{})
	if _, err := ts.StartTask(task.ID, blockingWork(started, finish)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	<-started
	close(finish)
	waitForStatus(t, ts, task.ID, StatusCompleted)

	_, _, err := ts.CancelTask(task.ID)
	if !errors.Is(err, ErrTaskAlreadyCompleted) {
		t.Errorf("err = %v, want ErrTaskAlreadyCompleted", err)
	}
}

// TestCompletionVsCancellationRace is the scenario the M3 spec calls out
// explicitly: goroutine A completes work at (roughly) the same instant
// goroutine B cancels it. Exactly one terminal state must win, and the
// race detector must find nothing (`go test -race`).
func TestCompletionVsCancellationRace(t *testing.T) {
	for i := 0; i < 200; i++ {
		_, ts := newTestStores(t)
		task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

		started := make(chan struct{})
		finish := make(chan struct{})
		if _, err := ts.StartTask(task.ID, blockingWork(started, finish)); err != nil {
			t.Fatalf("StartTask() error = %v", err)
		}
		<-started

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			close(finish) // let the work finish "at the same time" as cancel
		}()
		go func() {
			defer wg.Done()
			// Cancel may legitimately report "already completed" if
			// finishTask won the race first — that's a valid outcome,
			// not a test failure.
			_, _, err := ts.CancelTask(task.ID)
			if err != nil && !errors.Is(err, ErrTaskAlreadyCompleted) {
				t.Errorf("CancelTask() unexpected error = %v", err)
			}
		}()
		wg.Wait()

		final, err := waitForTerminal(t, ts, task.ID)
		if err != nil {
			t.Fatalf("waitForTerminal() error = %v", err)
		}
		if !final.Status.IsTerminal() {
			t.Fatalf("final status %q is not terminal", final.Status)
		}
	}
}

func TestConcurrentTaskCreation(t *testing.T) {
	_, ts := newTestStores(t)

	const workers = 50
	var wg sync.WaitGroup
	tasksCh := make(chan DiagnosticTask, workers)
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, _, err := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})
			if err != nil {
				errCh <- err
				return
			}
			tasksCh <- task
		}()
	}
	wg.Wait()
	close(tasksCh)
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	seen := make(map[string]bool)
	count := 0
	for task := range tasksCh {
		count++
		if task.ID == "" {
			t.Error("task.ID is empty")
		}
		if seen[task.ID] {
			t.Errorf("duplicate task ID: %s", task.ID)
		}
		seen[task.ID] = true
		if task.MachineID != "machine-17" {
			t.Errorf("MachineID = %q, want %q", task.MachineID, "machine-17")
		}
		if task.BoundVersion != 1 {
			t.Errorf("BoundVersion = %d, want 1 (machine never changed)", task.BoundVersion)
		}
	}
	if count != workers {
		t.Errorf("created %d tasks, want %d", count, workers)
	}

	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != workers {
		t.Errorf("ListTasksForMachine returned %d tasks, want %d", len(list), workers)
	}
}

func TestConcurrentCancellation_SingleWinnerNoRace(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)
	if _, err := ts.StartTask(task.ID, blockingWork(started, finish)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	<-started

	const attempts = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := ts.CancelTask(task.ID)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else if !errors.Is(err, ErrTaskAlreadyCancelled) {
				t.Errorf("unexpected error = %v", err)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("successful cancellations = %d, want exactly 1", successCount)
	}

	final, err := ts.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if final.Status != StatusCancelled {
		t.Errorf("final Status = %q, want %q", final.Status, StatusCancelled)
	}
}

func TestSimulatedWork_CompletesWhenNotCancelled(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	if _, err := ts.StartTask(task.ID, SimulatedWork(50*time.Millisecond)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	waitForStatus(t, ts, task.ID, StatusCompleted)
}

func TestSimulatedWork_StopsOnCancel(t *testing.T) {
	_, ts := newTestStores(t)
	task, _, _ := ts.CreateTask(CreateTaskInput{MachineID: "machine-17"})

	if _, err := ts.StartTask(task.ID, SimulatedWork(5*time.Second)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	start := time.Now()
	if _, _, err := ts.CancelTask(task.ID); err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("CancelTask() took %v, want well under the 5s work duration", elapsed)
	}

	got, err := ts.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("Status = %q, want %q", got.Status, StatusCancelled)
	}
}

// waitForStatus polls until a task reaches want or the test times out.
// Polling (not a fixed sleep) is used because task completion happens on
// a goroutine whose exact scheduling isn't controlled by the test.
func waitForStatus(t *testing.T, ts *Store, id string, want Status) DiagnosticTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := ts.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if task.Status == want {
			return task
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %s within timeout", id, want)
	return DiagnosticTask{}
}

func waitForTerminal(t *testing.T, ts *Store, id string) (DiagnosticTask, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := ts.GetTask(id)
		if err != nil {
			return DiagnosticTask{}, err
		}
		if task.Status.IsTerminal() {
			return task, nil
		}
		time.Sleep(1 * time.Millisecond)
	}
	return DiagnosticTask{}, fmt.Errorf("task %s did not reach a terminal status within timeout", id)
}
