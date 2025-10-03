package unit_test

import (
	"context"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
)

// CreateTestWorkers creates test workers
func CreateTestWorkers() *ergon.Workers {
	workers := ergon.NewWorkers()
	ergon.Addergon.WorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return nil
	})
	ergon.Addergon.WorkerFunc(workers, func(ctx context.Context, task *ergon.Task[FailingTaskArgs]) error {
		return task.Args.Error
	})
	ergon.Addergon.WorkerFunc(workers, func(ctx context.Context, task *ergon.Task[SlowTaskArgs]) error {
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

// Assertergon.TaskState checks if a task is in the expected state
func Assertergon.TaskState(t *testing.T, store ergon.Store, taskID string, expectedState ergon.TaskState) {
	task, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("failed to get task %s: %v", taskID, err)
	}
	if task.State != expectedState {
		t.Fatalf("expected task %s to be in state %s, got %s", taskID, expectedState, task.State)
	}
}

// AssertTaskCount checks if the number of tasks matches expected
func AssertTaskCount(t *testing.T, store ergon.Store, filter *ergon.TaskFilter, expected int) {
	count, err := store.CountTasks(context.Background(), filter)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d tasks, got %d", expected, count)
	}
}
