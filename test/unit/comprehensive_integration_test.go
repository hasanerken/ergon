package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/mock"
)

// Task types for comprehensive integration test
type StandardTask struct {
	Message string `json:"message"`
}

func (StandardTask) Kind() string { return "standard_task" }

type DelayedTask struct {
	Data string `json:"data"`
}

func (DelayedTask) Kind() string { return "delayed_task" }

type ScheduledTask struct {
	Info string `json:"info"`
}

func (ScheduledTask) Kind() string { return "scheduled_task" }

type RecurringTask struct {
	Counter int `json:"counter"`
}

func (RecurringTask) Kind() string { return "recurring_task" }

type LongRunningTask struct {
	Duration int `json:"duration"`
}

func (LongRunningTask) Kind() string { return "long_running_task" }

type RetryableTask struct {
	ShouldFail bool `json:"should_fail"`
}

func (RetryableTask) Kind() string { return "retryable_task" }

// TestComprehensiveIntegration tests all main features of Ergon in one comprehensive test:
// - Standard task execution
// - Delayed tasks
// - Scheduled tasks
// - Recurring tasks (every hour simulation)
// - Task statistics
// - Task deletion
// - And more advanced features
func TestComprehensiveIntegration(t *testing.T) {
	ctx := context.Background()

	// Setup store
	store := mock.NewPostgresStore()
	defer store.Close()

	// Setup workers with execution tracking
	workers := ergon.NewWorkers()

	var (
		standardExecuted  bool
		delayedExecuted   bool
		scheduledExecuted bool
		recurringCount    int
	)

	// Standard task worker
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[StandardTask]) error {
		standardExecuted = true
		return nil
	})

	// Delayed task worker
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[DelayedTask]) error {
		delayedExecuted = true
		return nil
	})

	// Scheduled task worker
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[ScheduledTask]) error {
		scheduledExecuted = true
		return nil
	})

	// Recurring task worker (every hour simulation)
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[RecurringTask]) error {
		recurringCount++
		return nil
	})

	// Long-running task worker
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[LongRunningTask]) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	})

	// Retryable task worker
	var attemptCount int
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[RetryableTask]) error {
		attemptCount++
		if attemptCount < 3 {
			return errors.New("simulated failure")
		}
		return nil
	})

	// Create client, manager, and server
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})
	manager := ergon.NewManager(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				PollInterval: 50 * time.Millisecond,
			},
			"batch_queue": {
				MaxWorkers:   2,
				PollInterval: 50 * time.Millisecond,
			},
			"inspection_queue": {
				MaxWorkers:   2,
				PollInterval: 50 * time.Millisecond,
			},
			"manageable_queue": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Cleanup
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = server.Stop(shutdownCtx)
	}()

	t.Run("standard_task", func(t *testing.T) {
		// Enqueue a standard task
		task, err := ergon.Enqueue(client, ctx, StandardTask{Message: "Hello Ergon"})
		AssertNoError(t, err, "failed to enqueue standard task")

		// Wait for task to complete
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)
		if !standardExecuted {
			t.Error("standard task was not executed")
		}

		// Verify via inspector
		details, err := manager.Tasks().GetTaskDetails(ctx, task.ID)
		AssertNoError(t, err, "failed to get task details")
		if details.Task.State != ergon.StateCompleted {
			t.Errorf("expected state completed, got %s", details.Task.State)
		}
	})

	t.Run("delayed_task", func(t *testing.T) {
		// Enqueue a delayed task (executes after 200ms)
		task, err := ergon.Enqueue(client, ctx, DelayedTask{Data: "delayed"},
			ergon.WithDelay(200*time.Millisecond),
		)
		AssertNoError(t, err, "failed to enqueue delayed task")

		// Immediately check - should be scheduled, not completed
		retrieved, _ := store.GetTask(ctx, task.ID)
		if retrieved.State != ergon.StateScheduled {
			t.Errorf("expected state scheduled, got %s", retrieved.State)
		}

		// Wait for scheduler to move it and worker to execute
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)
		if !delayedExecuted {
			t.Error("delayed task was not executed")
		}
	})

	t.Run("scheduled_task", func(t *testing.T) {
		// Schedule a task for specific time (500ms from now)
		scheduledTime := time.Now().Add(500 * time.Millisecond)
		task, err := ergon.Enqueue(client, ctx, ScheduledTask{Info: "scheduled"},
			ergon.WithScheduledAt(scheduledTime),
		)
		AssertNoError(t, err, "failed to enqueue scheduled task")

		// Should be in scheduled state
		retrieved, _ := store.GetTask(ctx, task.ID)
		if retrieved.State != ergon.StateScheduled {
			t.Errorf("expected state scheduled, got %s", retrieved.State)
		}
		if retrieved.ScheduledAt == nil || retrieved.ScheduledAt.Before(time.Now()) {
			t.Error("scheduled time should be in the future")
		}

		// Wait for execution
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)
		if !scheduledExecuted {
			t.Error("scheduled task was not executed")
		}
	})

	t.Run("recurring_task_every_hour", func(t *testing.T) {
		// Enqueue recurring task (simulate every hour with short interval for testing)
		// In real usage: ergon.EveryHour() or ergon.WithInterval(time.Hour)
		task, err := ergon.Enqueue(client, ctx, RecurringTask{Counter: 1},
			ergon.WithInterval(300*time.Millisecond), // Use short interval for testing
		)
		AssertNoError(t, err, "failed to enqueue recurring task")

		// Wait for first execution
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)
		if recurringCount < 1 {
			t.Error("recurring task was not executed")
		}

		// Wait for second execution - server should auto-reschedule it
		// Scheduler runs every ~5 seconds by default, so wait for reschedule + execution
		time.Sleep(6 * time.Second)

		// Check that it was rescheduled and executed again
		if recurringCount < 2 {
			t.Errorf("expected recurring task to execute at least twice, got %d", recurringCount)
		}
	})

	t.Run("task_statistics", func(t *testing.T) {
		// Get overall statistics
		stats, err := manager.Stats().GetOverallStats(ctx)
		AssertNoError(t, err, "failed to get overall stats")

		if stats.TotalTasks == 0 {
			t.Error("expected total tasks > 0")
		}
		if stats.CompletedTasks == 0 {
			t.Error("expected completed tasks > 0")
		}

		// Get queue statistics
		queueStats, err := manager.Stats().GetQueueStats(ctx, "default")
		AssertNoError(t, err, "failed to get queue stats")

		if queueStats.PendingCount+queueStats.RunningCount+queueStats.CompletedCount == 0 {
			t.Error("expected queue tasks > 0")
		}

		// Get statistics by task kind
		kindStats, err := manager.Stats().GetKindStats(ctx, "default")
		AssertNoError(t, err, "failed to get kind stats")

		if kindStats == nil {
			t.Error("expected kind stats to be non-nil")
		}
	})

	t.Run("task_deletion", func(t *testing.T) {
		// Create a task to delete
		task, err := ergon.Enqueue(client, ctx, StandardTask{Message: "to be deleted"})
		AssertNoError(t, err, "failed to enqueue task for deletion")

		// Wait for completion
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)

		// Delete the task
		err = manager.Control().Delete(ctx, task.ID)
		AssertNoError(t, err, "failed to delete task")

		// Verify deletion
		_, err = store.GetTask(ctx, task.ID)
		if !errors.Is(err, ergon.ErrTaskNotFound) {
			t.Errorf("expected ErrTaskNotFound after deletion, got %v", err)
		}
	})

	t.Run("task_cancellation", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, LongRunningTask{Duration: 10})
		AssertNoError(t, err, "failed to enqueue long task")

		// Cancel it before it starts (cancel pending task)
		err = manager.Control().Cancel(ctx, task.ID)
		AssertNoError(t, err, "failed to cancel task")

		// Verify cancellation
		cancelled, err := store.GetTask(ctx, task.ID)
		AssertNoError(t, err, "failed to get task")
		if cancelled.State != ergon.StateCancelled {
			t.Errorf("expected state cancelled, got %s", cancelled.State)
		}
	})

	t.Run("batch_operations", func(t *testing.T) {
		// Create multiple tasks
		var taskIDs []string
		for i := 0; i < 5; i++ {
			task, err := ergon.Enqueue(client, ctx, StandardTask{Message: "batch task"},
				ergon.WithQueue("batch_queue"),
			)
			AssertNoError(t, err, "failed to enqueue batch task")
			taskIDs = append(taskIDs, task.ID)
		}

		// Wait for all to complete
		for _, taskID := range taskIDs {
			WaitForTaskState(t, store, taskID, ergon.StateCompleted, 5*time.Second)
		}

		// Use batch delete
		deleted, err := manager.Batch().DeleteByFilter(ctx, ergon.TaskFilter{
			Queue: "batch_queue",
			State: ergon.StateCompleted,
		})
		AssertNoError(t, err, "failed to batch delete")

		if deleted != 5 {
			t.Errorf("expected to delete 5 tasks, deleted %d", deleted)
		}
	})

	t.Run("task_inspection", func(t *testing.T) {
		// Create tasks with different priorities
		task1, err := ergon.Enqueue(client, ctx, StandardTask{Message: "high"},
			ergon.WithPriority(10),
			ergon.WithQueue("inspection_queue"),
		)
		AssertNoError(t, err, "failed to enqueue high priority task")

		task2, err := ergon.Enqueue(client, ctx, StandardTask{Message: "low"},
			ergon.WithPriority(1),
			ergon.WithQueue("inspection_queue"),
		)
		AssertNoError(t, err, "failed to enqueue low priority task")

		// Wait for completion
		WaitForTaskState(t, store, task1.ID, ergon.StateCompleted, 5*time.Second)
		WaitForTaskState(t, store, task2.ID, ergon.StateCompleted, 5*time.Second)

		// List tasks in queue
		taskList, err := manager.Tasks().ListTasks(ctx, ergon.ListOptions{
			Queue: "inspection_queue",
			State: ergon.StateCompleted,
		})
		AssertNoError(t, err, "failed to list tasks")

		if len(taskList.Tasks) < 2 {
			t.Errorf("expected at least 2 tasks, got %d", len(taskList.Tasks))
		}

		// Count tasks by state
		countMap, err := manager.Tasks().CountByState(ctx, "inspection_queue")
		AssertNoError(t, err, "failed to count tasks")

		if countMap[ergon.StateCompleted] == 0 {
			t.Error("expected completed task count > 0")
		}
	})

	t.Run("task_retry", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, RetryableTask{ShouldFail: true},
			ergon.WithMaxRetries(5),
		)
		AssertNoError(t, err, "failed to enqueue failing task")

		// Wait for successful retry
		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 10*time.Second)

		// Should have retried at least once
		retrieved, _ := store.GetTask(ctx, task.ID)
		if retrieved.Retried < 1 {
			t.Error("expected task to be retried at least once")
		}
		if attemptCount < 3 {
			t.Errorf("expected at least 3 attempts, got %d", attemptCount)
		}
	})

	t.Run("task_with_metadata", func(t *testing.T) {
		// Enqueue task with metadata
		task, err := ergon.Enqueue(client, ctx, StandardTask{Message: "with metadata"},
			ergon.WithMetadataMap(map[string]interface{}{
				"user_id":    12345,
				"request_id": "req-abc-123",
				"source":     "integration_test",
			}),
		)
		AssertNoError(t, err, "failed to enqueue task with metadata")

		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)

		// Verify metadata
		retrieved, err := store.GetTask(ctx, task.ID)
		AssertNoError(t, err, "failed to get task")

		if retrieved.Metadata["user_id"].(int) != 12345 {
			t.Error("metadata user_id mismatch")
		}
		if retrieved.Metadata["request_id"].(string) != "req-abc-123" {
			t.Error("metadata request_id mismatch")
		}
	})

	t.Run("queue_management", func(t *testing.T) {
		// Create tasks in a specific queue
		testQueue := "manageable_queue"
		task, err := ergon.Enqueue(client, ctx, StandardTask{Message: "queue test"},
			ergon.WithQueue(testQueue),
		)
		AssertNoError(t, err, "failed to enqueue task")

		WaitForTaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)

		// Get queue info
		queueInfo, err := store.GetQueueInfo(ctx, testQueue)
		AssertNoError(t, err, "failed to get queue info")

		if queueInfo.Name != testQueue {
			t.Errorf("expected queue name %s, got %s", testQueue, queueInfo.Name)
		}
		if queueInfo.CompletedCount == 0 {
			t.Error("expected completed count > 0")
		}
	})
}

// WaitForTaskState waits for a task to reach a specific state
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
