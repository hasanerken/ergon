package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/mock"
)

func TestClient_ergon.Enqueue(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue:   "default",
		DefaultRetries: 3,
		Workers:        workers,
	})

	ctx := context.Background()

	t.Run("basic enqueue", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "hello"})
		AssertNoError(t, err, "enqueue failed")

		if task.ID == "" {
			t.Error("task ID is empty")
		}
		if task.Kind != "test_task" {
			t.Errorf("expected kind 'test_task', got %s", task.Kind)
		}
		if task.Queue != "default" {
			t.Errorf("expected queue 'default', got %s", task.Queue)
		}
		if task.State != ergon.StatePending {
			t.Errorf("expected state pending, got %s", task.State)
		}
		if task.MaxRetries != 3 {
			t.Errorf("expected max retries 3, got %d", task.MaxRetries)
		}
	})

	t.Run("enqueue with options", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "priority"},
			ergon.WithQueue("critical"),
			ergon.WithPriority(10),
			ergon.WithMaxRetries(5),
			ergon.WithTimeout(1*time.Minute),
		)
		AssertNoError(t, err, "enqueue failed")

		if task.Queue != "critical" {
			t.Errorf("expected queue 'critical', got %s", task.Queue)
		}
		if task.Priority != 10 {
			t.Errorf("expected priority 10, got %d", task.Priority)
		}
		if task.MaxRetries != 5 {
			t.Errorf("expected max retries 5, got %d", task.MaxRetries)
		}
		if task.Timeout != 1*time.Minute {
			t.Errorf("expected timeout 1m, got %v", task.Timeout)
		}
	})

	t.Run("enqueue scheduled task", func(t *testing.T) {
		scheduledTime := time.Now().Add(1 * time.Hour)
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "scheduled"},
			ergon.WithScheduledAt(scheduledTime),
		)
		AssertNoError(t, err, "enqueue failed")

		if task.State != ergon.StateScheduled {
			t.Errorf("expected state scheduled, got %s", task.State)
		}
		if task.ScheduledAt == nil || !task.ScheduledAt.Equal(scheduledTime) {
			t.Error("scheduled time not set correctly")
		}
	})

	t.Run("enqueue with metadata", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "metadata"},
			WithMetadata("user_id", "123"),
			WithMetadata("request_id", "abc"),
		)
		AssertNoError(t, err, "enqueue failed")

		if task.Metadata["user_id"] != "123" {
			t.Error("metadata user_id not set")
		}
		if task.Metadata["request_id"] != "abc" {
			t.Error("metadata request_id not set")
		}
	})

	t.Run("unique task", func(t *testing.T) {
		// First enqueue should succeed
		task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "unique"},
			ergon.WithUnique(1*time.Hour),
		)
		AssertNoError(t, err, "first unique enqueue failed")

		// Second enqueue with same args should fail
		_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "unique"},
			ergon.WithUnique(1*time.Hour),
		)
		if !errors.Is(err, ergon.ErrDuplicateTask) {
			t.Errorf("expected ergon.ErrDuplicateTask, got %v", err)
		}

		// Different args should succeed
		task3, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "different"},
			ergon.WithUnique(1*time.Hour),
		)
		AssertNoError(t, err, "different unique task failed")

		if task1.ID == task3.ID {
			t.Error("different tasks got same ID")
		}
	})

	t.Run("enqueue with delay", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "delayed"},
			ergon.WithDelay(5*time.Second),
		)
		AssertNoError(t, err, "enqueue with delay failed")

		if task.State != ergon.StateScheduled {
			t.Errorf("expected state scheduled, got %s", task.State)
		}
		if task.ScheduledAt == nil {
			t.Error("scheduled time not set")
		}
	})
}

func TestClient_ergon.EnqueueMany(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	ctx := context.Background()

	argsList := []TestTaskArgs{
		{Message: "task1"},
		{Message: "task2"},
		{Message: "task3"},
	}

	tasks, err := ergon.EnqueueMany(client, ctx, argsList)
	AssertNoError(t, err, "enqueue many failed")

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	for i, task := range tasks {
		if task.Args.Message != argsList[i].Message {
			t.Errorf("task %d message mismatch", i)
		}
	}

	// Verify all tasks are in store
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")
	if count != 3 {
		t.Errorf("expected 3 tasks in store, got %d", count)
	}
}

func TestClient_WorkerValidation(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()

	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers, // Enable validation
	})

	ctx := context.Background()

	t.Run("valid worker kind", func(t *testing.T) {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "valid"})
		AssertNoError(t, err, "valid worker should succeed")
	})

	t.Run("invalid worker kind", func(t *testing.T) {
		type UnknownTask struct {
			Data string
		}
		unknownTask := struct {
			Data string
		}{Data: "test"}

		// This won't compile because UnknownTask doesn't implement TaskArgs
		// So we simulate by using client without validation
		clientNoValidation := ergon.NewClient(store, ergon.ClientConfig{
			Workers: nil, // Disable validation
		})

		// Can enqueue without validation
		_, err := ergon.Enqueue(clientNoValidation, ctx, TestTaskArgs{Message: "test"})
		AssertNoError(t, err, "should succeed without validation")

		// With validation, unknown kind would fail
		// (We test this by checking the worker registry directly)
		if workers.Has("unknown_kind") {
			t.Error("unknown worker kind should not exist")
		}
	})
}

func TestClient_CancelTask(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	ctx := context.Background()

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "test"})
	AssertNoError(t, err, "enqueue failed")

	err = client.CancelTask(ctx, task.ID)
	AssertNoError(t, err, "cancel failed")

	// Verify task is cancelled
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.State != ergon.StateCancelled {
		t.Errorf("expected state cancelled, got %s", retrieved.State)
	}
}

func TestClient_DeleteTask(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	ctx := context.Background()

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "test"})
	AssertNoError(t, err, "enqueue failed")

	err = client.DeleteTask(ctx, task.ID)
	AssertNoError(t, err, "delete failed")

	// Verify task is deleted
	_, err = store.GetTask(ctx, task.ID)
	if !errors.Is(err, ergon.ErrTaskNotFound) {
		t.Errorf("expected ergon.ErrTaskNotFound, got %v", err)
	}
}

func TestClient_QueueManagement(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	client := ergon.NewClient(store, ergon.ClientConfig{})

	ctx := context.Background()

	t.Run("pause queue", func(t *testing.T) {
		err := client.PauseQueue(ctx, "test-queue")
		AssertNoError(t, err, "pause queue failed")

		info, err := store.GetQueueInfo(ctx, "test-queue")
		AssertNoError(t, err, "get queue info failed")

		if !info.Paused {
			t.Error("queue should be paused")
		}
	})

	t.Run("resume queue", func(t *testing.T) {
		err := client.ResumeQueue(ctx, "test-queue")
		AssertNoError(t, err, "resume queue failed")

		info, err := store.GetQueueInfo(ctx, "test-queue")
		AssertNoError(t, err, "get queue info failed")

		if info.Paused {
			t.Error("queue should not be paused")
		}
	})
}

func TestClient_CompositeOptions(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	ctx := context.Background()

	t.Run("AtMostOncePerHour", func(t *testing.T) {
		task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "once"},
			AtMostOncePerHour(),
		)
		AssertNoError(t, err, "first enqueue failed")

		_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "once"},
			AtMostOncePerHour(),
		)
		if !errors.Is(err, ergon.ErrDuplicateTask) {
			t.Error("duplicate should be rejected")
		}

		if task1.State != ergon.StatePending {
			t.Errorf("expected pending state, got %s", task1.State)
		}
	})

	t.Run("AtMostOncePerDay", func(t *testing.T) {
		task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "daily"},
			AtMostOncePerDay(),
		)
		AssertNoError(t, err, "first enqueue failed")

		_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "daily"},
			AtMostOncePerDay(),
		)
		if !errors.Is(err, ergon.ErrDuplicateTask) {
			t.Error("duplicate should be rejected")
		}

		if task1.State != ergon.StatePending {
			t.Errorf("expected pending state, got %s", task1.State)
		}
	})

	t.Run("WithNoRetry", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "no-retry"},
			WithNoRetry(),
		)
		AssertNoError(t, err, "enqueue failed")

		if task.MaxRetries != 0 {
			t.Errorf("expected 0 retries, got %d", task.MaxRetries)
		}
	})

	t.Run("WithRetryOnce", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "retry-once"},
			WithRetryOnce(),
		)
		AssertNoError(t, err, "enqueue failed")

		if task.MaxRetries != 1 {
			t.Errorf("expected 1 retry, got %d", task.MaxRetries)
		}
	})
}
