package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/mock"
)

func TestIntegration_Mock_BasicWorkflow(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue task
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "test"})
	AssertNoError(t, err, "enqueue failed")

	// Verify task in store
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.State != ergon.StatePending {
		t.Errorf("expected state pending, got %s", retrieved.State)
	}

	// Dequeue task
	dequeued, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
	AssertNoError(t, err, "dequeue failed")

	if dequeued.ID != task.ID {
		t.Error("dequeued wrong task")
	}
	if dequeued.State != ergon.StateRunning {
		t.Errorf("expected state running, got %s", dequeued.State)
	}

	// Mark completed
	err = store.MarkCompleted(ctx, task.ID, []byte("result"))
	AssertNoError(t, err, "mark completed failed")

	// Verify completion
	completed, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if completed.State != ergon.StateCompleted {
		t.Errorf("expected state completed, got %s", completed.State)
	}
}

func TestIntegration_Mock_TaskStates(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Test pending -> running -> completed
	t.Run("success path", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "success"})
		AssertNoError(t, err, "enqueue failed")

		Assertergon.TaskState(t, store, task.ID, ergon.StatePending)

		err = store.MarkRunning(ctx, task.ID, "worker-1")
		AssertNoError(t, err, "mark running failed")
		Assertergon.TaskState(t, store, task.ID, ergon.StateRunning)

		err = store.MarkCompleted(ctx, task.ID, nil)
		AssertNoError(t, err, "mark completed failed")
		Assertergon.TaskState(t, store, task.ID, ergon.StateCompleted)
	})

	// Test pending -> running -> failed
	t.Run("failure path", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "failure"})
		AssertNoError(t, err, "enqueue failed")

		err = store.MarkRunning(ctx, task.ID, "worker-1")
		AssertNoError(t, err, "mark running failed")

		err = store.MarkFailed(ctx, task.ID, errors.New("test error"))
		AssertNoError(t, err, "mark failed failed")
		Assertergon.TaskState(t, store, task.ID, ergon.StateFailed)

		// Verify error message
		failed, err := store.GetTask(ctx, task.ID)
		AssertNoError(t, err, "get task failed")
		if failed.Error != "test error" {
			t.Errorf("expected error 'test error', got %s", failed.Error)
		}
	})

	// Test pending -> cancelled
	t.Run("cancellation", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "cancel"})
		AssertNoError(t, err, "enqueue failed")

		err = store.MarkCancelled(ctx, task.ID)
		AssertNoError(t, err, "mark cancelled failed")
		Assertergon.TaskState(t, store, task.ID, ergon.StateCancelled)
	})

	// Test retry path
	t.Run("retry path", func(t *testing.T) {
		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "retry"})
		AssertNoError(t, err, "enqueue failed")

		err = store.MarkRunning(ctx, task.ID, "worker-1")
		AssertNoError(t, err, "mark running failed")

		nextRetry := time.Now().Add(5 * time.Second)
		err = store.MarkRetrying(ctx, task.ID, nextRetry)
		AssertNoError(t, err, "mark retrying failed")

		retrieved, err := store.GetTask(ctx, task.ID)
		AssertNoError(t, err, "get task failed")

		if retrieved.State != ergon.StateRetrying {
			t.Errorf("expected state retrying, got %s", retrieved.State)
		}
		if retrieved.Retried != 1 {
			t.Errorf("expected retried count 1, got %d", retrieved.Retried)
		}
	})
}

func TestIntegration_Mock_QueuePriority(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue tasks with different priorities
	task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "low"}, ergon.WithPriority(1))
	AssertNoError(t, err, "enqueue low failed")

	task2, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "high"}, ergon.WithPriority(10))
	AssertNoError(t, err, "enqueue high failed")

	task3, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "medium"}, ergon.WithPriority(5))
	AssertNoError(t, err, "enqueue medium failed")

	// Dequeue should get highest priority first
	dequeued1, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
	AssertNoError(t, err, "dequeue 1 failed")

	if dequeued1.ID != task2.ID {
		t.Error("expected high priority task first")
	}

	dequeued2, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
	AssertNoError(t, err, "dequeue 2 failed")

	if dequeued2.ID != task3.ID {
		t.Error("expected medium priority task second")
	}

	dequeued3, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
	AssertNoError(t, err, "dequeue 3 failed")

	if dequeued3.ID != task1.ID {
		t.Error("expected low priority task last")
	}
}

func TestIntegration_Mock_Scheduling(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Schedule task for future
	future := time.Now().Add(1 * time.Hour)
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "future"},
		ergon.WithScheduledAt(future),
	)
	AssertNoError(t, err, "enqueue failed")

	Assertergon.TaskState(t, store, task.ID, ergon.StateScheduled)

	// Should not be dequeued yet
	_, err = store.Dequeue(ctx, []string{"default"}, "worker-1")
	if !errors.Is(err, ergon.ErrTaskNotFound) {
		t.Error("scheduled task should not be dequeued")
	}

	// Move scheduled to available
	count, err := store.MoveScheduledToAvailable(ctx, time.Now().Add(2*time.Hour))
	AssertNoError(t, err, "move scheduled failed")

	if count != 1 {
		t.Errorf("expected 1 task moved, got %d", count)
	}

	// Now should be dequeued
	dequeued, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
	AssertNoError(t, err, "dequeue failed after schedule move")

	if dequeued.ID != task.ID {
		t.Error("wrong task dequeued")
	}
}

func TestIntegration_Mock_ListTasks(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue various tasks
	_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "1"}, ergon.WithQueue("queue1"))
	AssertNoError(t, err, "enqueue 1 failed")

	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "2"}, ergon.WithQueue("queue2"))
	AssertNoError(t, err, "enqueue 2 failed")

	_, err = ergon.Enqueue(client, ctx, FailingTaskArgs{Message: "3"}, ergon.WithQueue("queue1"))
	AssertNoError(t, err, "enqueue 3 failed")

	// List all tasks
	allTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "list all failed")

	if len(allTasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(allTasks))
	}

	// List by queue
	queue1Tasks, err := store.ListTasks(ctx, &ergon.TaskFilter{Queue: "queue1"})
	AssertNoError(t, err, "list queue1 failed")

	if len(queue1Tasks) != 2 {
		t.Errorf("expected 2 tasks in queue1, got %d", len(queue1Tasks))
	}

	// List by kind
	testTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{Kind: "test_task"})
	AssertNoError(t, err, "list by kind failed")

	if len(testTasks) != 2 {
		t.Errorf("expected 2 test_task tasks, got %d", len(testTasks))
	}

	// List by state
	pendingTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StatePending})
	AssertNoError(t, err, "list pending failed")

	if len(pendingTasks) != 3 {
		t.Errorf("expected 3 pending tasks, got %d", len(pendingTasks))
	}
}

func TestIntegration_Mock_CountTasks(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue tasks
	for i := 0; i < 5; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "test"})
		AssertNoError(t, err, "enqueue failed")
	}

	// Count all
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != 5 {
		t.Errorf("expected 5 tasks, got %d", count)
	}

	// Mark some completed
	tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{})
	_ = store.MarkCompleted(ctx, tasks[0].ID, nil)
	_ = store.MarkCompleted(ctx, tasks[1].ID, nil)

	// Count pending
	pendingCount, err := store.CountTasks(ctx, &ergon.TaskFilter{State: ergon.StatePending})
	AssertNoError(t, err, "count pending failed")

	if pendingCount != 3 {
		t.Errorf("expected 3 pending tasks, got %d", pendingCount)
	}

	// Count completed
	completedCount, err := store.CountTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertNoError(t, err, "count completed failed")

	if completedCount != 2 {
		t.Errorf("expected 2 completed tasks, got %d", completedCount)
	}
}

func TestIntegration_Mock_QueueInfo(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue tasks to different queues
	_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "1"}, ergon.WithQueue("queue1"))
	AssertNoError(t, err, "enqueue 1 failed")

	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "2"}, ergon.WithQueue("queue1"))
	AssertNoError(t, err, "enqueue 2 failed")

	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "3"}, ergon.WithQueue("queue2"))
	AssertNoError(t, err, "enqueue 3 failed")

	// Get queue info
	info, err := store.GetQueueInfo(ctx, "queue1")
	AssertNoError(t, err, "get queue info failed")

	if info.Name != "queue1" {
		t.Errorf("expected queue name 'queue1', got %s", info.Name)
	}
	if info.PendingCount != 2 {
		t.Errorf("expected 2 pending tasks, got %d", info.PendingCount)
	}

	// List all queues
	queues, err := store.ListQueues(ctx)
	AssertNoError(t, err, "list queues failed")

	if len(queues) != 2 {
		t.Errorf("expected 2 queues, got %d", len(queues))
	}
}

func TestIntegration_Mock_UpdateTask(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "update"})
	AssertNoError(t, err, "enqueue failed")

	// Update priority
	newPriority := 20
	err = store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{
		Priority: &newPriority,
	})
	AssertNoError(t, err, "update priority failed")

	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.Priority != 20 {
		t.Errorf("expected priority 20, got %d", retrieved.Priority)
	}

	// Update metadata
	err = store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{
		Metadata: map[string]interface{}{
			"updated": true,
		},
	})
	AssertNoError(t, err, "update metadata failed")

	retrieved, err = store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.Metadata["updated"] != true {
		t.Error("metadata not updated")
	}
}

func TestIntegration_Mock_DeleteTask(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "delete"})
	AssertNoError(t, err, "enqueue failed")

	// Delete task
	err = store.DeleteTask(ctx, task.ID)
	AssertNoError(t, err, "delete failed")

	// Verify deleted
	_, err = store.GetTask(ctx, task.ID)
	if !errors.Is(err, ergon.ErrTaskNotFound) {
		t.Errorf("expected ergon.ErrTaskNotFound, got %v", err)
	}

	// Count should be 0
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != 0 {
		t.Errorf("expected 0 tasks, got %d", count)
	}
}

func TestIntegration_Mock_RecoverStuckTasks(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "stuck"})
	AssertNoError(t, err, "enqueue failed")

	// Mark as running with old start time
	err = store.MarkRunning(ctx, task.ID, "worker-1")
	AssertNoError(t, err, "mark running failed")

	// Manually set start time to past
	retrieved, _ := store.GetTask(ctx, task.ID)
	oldTime := time.Now().Add(-2 * time.Hour)
	retrieved.StartedAt = &oldTime
	store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{})

	// Recover stuck tasks
	count, err := store.RecoverStuckTasks(ctx, 1*time.Hour)
	AssertNoError(t, err, "recover failed")

	if count != 1 {
		t.Errorf("expected 1 recovered task, got %d", count)
	}

	// Verify task is pending again
	Assertergon.TaskState(t, store, task.ID, ergon.StatePending)
}

func TestIntegration_Mock_Groups(t *testing.T) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Add tasks to group
	task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "group1"}, ergon.WithGroup("group:1"))
	AssertNoError(t, err, "enqueue 1 failed")

	task2, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "group2"}, ergon.WithGroup("group:1"))
	AssertNoError(t, err, "enqueue 2 failed")

	// Get group tasks
	groupTasks, err := store.GetGroup(ctx, "group:1")
	AssertNoError(t, err, "get group failed")

	if len(groupTasks) != 2 {
		t.Errorf("expected 2 tasks in group, got %d", len(groupTasks))
	}

	// Verify tasks are in aggregating state
	Assertergon.TaskState(t, store, task1.ID, ergon.StateAggregating)
	Assertergon.TaskState(t, store, task2.ID, ergon.StateAggregating)

	// Consume group
	consumed, err := store.ConsumeGroup(ctx, "group:1")
	AssertNoError(t, err, "consume group failed")

	if len(consumed) != 2 {
		t.Errorf("expected 2 consumed tasks, got %d", len(consumed))
	}

	// Verify group is empty
	groupTasks, err = store.GetGroup(ctx, "group:1")
	AssertNoError(t, err, "get group failed")

	if len(groupTasks) != 0 {
		t.Errorf("expected empty group, got %d tasks", len(groupTasks))
	}
}
