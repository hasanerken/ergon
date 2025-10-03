package ergon

import (
	"context"
	"testing"
	"time"
)

// TestHelper provides utilities for testing
type TestHelper struct {
	t       *testing.T
	cleanup []func()
}

// NewTestHelper creates a new test helper
func NewTestHelper(t *testing.T) *TestHelper {
	return &TestHelper{
		t:       t,
		cleanup: make([]func(), 0),
	}
}

// Cleanup registers a cleanup function
func (h *TestHelper) Cleanup(fn func()) {
	h.cleanup = append(h.cleanup, fn)
}

// Close runs all cleanup functions
func (h *TestHelper) Close() {
	for i := len(h.cleanup) - 1; i >= 0; i-- {
		h.cleanup[i]()
	}
}

// TempDir returns a temporary directory for test use
func (h *TestHelper) TempDir() string {
	return h.t.TempDir()
}

// CreateTestWorkers creates a Workers registry with test workers
func CreateTestWorkers() *Workers {
	workers := NewWorkers()
	AddWorkerFunc(workers, func(ctx context.Context, task *Task[TestTaskArgs]) error {
		return nil
	})
	AddWorkerFunc(workers, func(ctx context.Context, task *Task[FailingTaskArgs]) error {
		return task.Args.Error
	})
	AddWorkerFunc(workers, func(ctx context.Context, task *Task[SlowTaskArgs]) error {
		time.Sleep(task.Args.Duration)
		return nil
	})
	return workers
}

// TestTaskArgs is a simple test task
type TestTaskArgs struct {
	Message string `json:"message"`
}

func (TestTaskArgs) Kind() string { return "test_task" }

// FailingTaskArgs is a task that can fail
type FailingTaskArgs struct {
	Message string `json:"message"`
	Error   error  `json:"-"`
}

func (FailingTaskArgs) Kind() string { return "failing_task" }

// SlowTaskArgs is a task that takes time
type SlowTaskArgs struct {
	Duration time.Duration `json:"duration"`
}

func (SlowTaskArgs) Kind() string { return "slow_task" }

// WaitForTaskState waits for a task to reach a specific state
func WaitForTaskState(t *testing.T, store Store, taskID string, expectedState TaskState, timeout time.Duration) *InternalTask {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for task %s to reach state %s", taskID, expectedState)
			return nil
		case <-ticker.C:
			task, err := store.GetTask(context.Background(), taskID)
			if err != nil {
				continue
			}
			if task.State == expectedState {
				return task
			}
		}
	}
}

// WaitForCondition waits for a condition to become true
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for condition: %s", message)
			return
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// AssertTaskState checks if a task is in the expected state
func AssertTaskState(t *testing.T, store Store, taskID string, expectedState TaskState) {
	task, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("failed to get task %s: %v", taskID, err)
	}
	if task.State != expectedState {
		t.Fatalf("expected task %s to be in state %s, got %s", taskID, expectedState, task.State)
	}
}

// AssertTaskCount checks if the number of tasks matches expected
func AssertTaskCount(t *testing.T, store Store, filter *TaskFilter, expected int) {
	count, err := store.CountTasks(context.Background(), filter)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d tasks, got %d", expected, count)
	}
}

// AssertNoError fails if error is not nil
func AssertNoError(t *testing.T, err error, message string) {
	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

// AssertError fails if error is nil
func AssertError(t *testing.T, err error, message string) {
	if err == nil {
		t.Fatalf("%s: expected error but got nil", message)
	}
}
