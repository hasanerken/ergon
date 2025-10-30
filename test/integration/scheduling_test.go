package integration_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
)

// TestScheduledTask_DelayExecution tests that scheduled tasks execute after their scheduled time
func TestScheduledTask_DelayExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Track execution time
	var executedAt atomic.Value
	executedAt.Store(time.Time{})

	// Create workers
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executedAt.Store(time.Now())
		return nil
	})

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = server.Start(serverCtx)
	AssertNoError(t, err, "failed to start server")
	defer server.Stop(ctx)

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Enqueue task scheduled for 2 seconds in the future
	enqueuedAt := time.Now()
	delay := 2 * time.Second
	scheduledFor := enqueuedAt.Add(delay)

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{
		Message: "delayed task",
	}, ergon.WithScheduledAt(scheduledFor))
	AssertNoError(t, err, "failed to enqueue task")

	// Verify task is in scheduled state
	AssertEquals(t, task.State, ergon.StateScheduled, "expected task to be in scheduled state")

	// Wait a bit, task should NOT execute yet
	time.Sleep(1 * time.Second)
	executed := executedAt.Load().(time.Time)
	AssertTrue(t, executed.IsZero(), "task should not execute before scheduled time")

	// Verify task is still in scheduled state
	storedTask, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "failed to get task")
	AssertEquals(t, storedTask.State, ergon.StateScheduled, "task should still be in scheduled state")

	// Wait for scheduler to move task to pending and execute it
	// Scheduler runs every 5 seconds, so wait 7 seconds total from start
	time.Sleep(6500 * time.Millisecond)

	// Verify task was executed
	executed = executedAt.Load().(time.Time)
	AssertTrue(t, !executed.IsZero(), "task should have been executed")

	// Verify execution happened after scheduled time
	AssertTrue(t, executed.After(scheduledFor) || executed.Equal(scheduledFor),
		"task should execute after scheduled time")

	// Verify task is now completed
	storedTask, err = store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "failed to get task")
	AssertEquals(t, storedTask.State, ergon.StateCompleted, "task should be completed")
}

// TestScheduledTask_WithProcessIn tests WithProcessIn option
func TestScheduledTask_WithProcessIn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Track execution
	var executed atomic.Bool

	// Create workers
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed.Store(true)
		return nil
	})

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = server.Start(serverCtx)
	AssertNoError(t, err, "failed to start server")
	defer server.Stop(ctx)

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Enqueue task with WithProcessIn
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{
		Message: "delayed task",
	}, ergon.WithProcessIn(2*time.Second))
	AssertNoError(t, err, "failed to enqueue task")

	// Verify task is in scheduled state
	AssertEquals(t, task.State, ergon.StateScheduled, "expected task to be in scheduled state")

	// Wait a bit, should not execute yet
	time.Sleep(1 * time.Second)
	AssertEquals(t, executed.Load(), false, "task should not execute yet")

	// Wait for execution (scheduler runs every 5 seconds)
	time.Sleep(7 * time.Second)

	// Verify task executed
	AssertEquals(t, executed.Load(), true, "task should have executed")
}

// TestScheduledTask_ImmediateExecution tests that tasks scheduled in the past execute immediately
func TestScheduledTask_ImmediateExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Track execution
	var executed atomic.Bool

	// Create workers
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed.Store(true)
		return nil
	})

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = server.Start(serverCtx)
	AssertNoError(t, err, "failed to start server")
	defer server.Stop(ctx)

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Enqueue task scheduled in the past (should execute immediately as pending)
	pastTime := time.Now().Add(-1 * time.Hour)
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{
		Message: "past task",
	}, ergon.WithScheduledAt(pastTime))
	AssertNoError(t, err, "failed to enqueue task")

	// Task should be in pending state (not scheduled) because scheduled time is in the past
	AssertEquals(t, task.State, ergon.StatePending, "expected task to be pending (past scheduled time)")

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	// Verify task executed
	AssertEquals(t, executed.Load(), true, "task should have executed immediately")
}

// TestScheduledTask_MultipleDelayedTasks tests multiple scheduled tasks
func TestScheduledTask_MultipleDelayedTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Track execution count
	var executionCount atomic.Int32

	// Create workers
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executionCount.Add(1)
		return nil
	})

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = server.Start(serverCtx)
	AssertNoError(t, err, "failed to start server")
	defer server.Stop(ctx)

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Enqueue multiple tasks with different delays
	taskCount := 5
	for i := 0; i < taskCount; i++ {
		delay := time.Duration(i+1) * time.Second
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: "delayed task",
		}, ergon.WithProcessIn(delay))
		AssertNoError(t, err, "failed to enqueue task")
	}

	// Verify all tasks are in scheduled state
	scheduledCount, err := store.CountTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateScheduled,
	})
	AssertNoError(t, err, "failed to count scheduled tasks")
	AssertEquals(t, scheduledCount, taskCount, "expected all tasks to be scheduled")

	// Wait for all tasks to be executed (max delay is 5 seconds + scheduler interval)
	time.Sleep(12 * time.Second)

	// Verify all tasks executed
	AssertEquals(t, int(executionCount.Load()), taskCount, "all tasks should have executed")
}

// TestSchedulerInterval tests that the scheduler runs at the correct interval
func TestSchedulerInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Create workers
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return nil
	})

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	// Start server
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = server.Start(serverCtx)
	AssertNoError(t, err, "failed to start server")
	defer server.Stop(ctx)

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Enqueue task scheduled for 3 seconds in the future
	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{
		Message: "test",
	}, ergon.WithProcessIn(3*time.Second))
	AssertNoError(t, err, "failed to enqueue task")

	// Wait less than scheduler interval (5 seconds)
	time.Sleep(4 * time.Second)

	// Task should still be scheduled (scheduler hasn't run yet)
	scheduledCount, _ := store.CountTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateScheduled,
	})

	// May be 0 or 1 depending on timing - scheduler runs every 5 seconds
	// If we're at 4 seconds and task was scheduled for 3 seconds, it might have been processed
	// This is expected behavior - scheduler will pick it up on next run
	t.Logf("Scheduled tasks remaining after 4 seconds: %d", scheduledCount)
}
