package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// TestHelper provides utilities for integration testing
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

// TempDir returns a temporary directory
func (h *TestHelper) TempDir() string {
	return h.t.TempDir()
}

// NewBadgerStore creates a BadgerDB store for testing
func (h *TestHelper) NewBadgerStore() ergon.Store {
	tempDir := h.t.TempDir()
	store, err := badger.NewStore(filepath.Join(tempDir, "queue_data"))
	if err != nil {
		h.t.Fatalf("failed to create badger store: %v", err)
	}
	h.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// CreateTestWorkers creates test workers
func CreateTestWorkers() *ergon.Workers {
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return nil
	})
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[FailingTaskArgs]) error {
		return task.Args.Error
	})
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[SlowTaskArgs]) error {
		time.Sleep(task.Args.Duration)
		return nil
	})
	return workers
}

// Test task types
type TestTaskArgs struct {
	Message string `json:"message"`
}

func (TestTaskArgs) Kind() string { return "test_task" }

type FailingTaskArgs struct {
	Message string `json:"message"`
	Error   error  `json:"-"`
}

func (FailingTaskArgs) Kind() string { return "failing_task" }

type SlowTaskArgs struct {
	Duration time.Duration `json:"duration"`
}

func (SlowTaskArgs) Kind() string { return "slow_task" }

// WaitForTaskState waits for a task to reach expected state
func WaitForTaskState(t *testing.T, store ergon.Store, taskID string, expectedState ergon.TaskState, timeout time.Duration) *ergon.InternalTask {
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

// WaitForCondition waits for condition to be true
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
