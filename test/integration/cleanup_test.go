package integration_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hasanerken/ergon"
)

// TestAutoCleanup_Badger tests automatic cleanup with BadgerDB
func TestAutoCleanup_Badger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Create workers that complete quickly
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return nil // Complete immediately
	})

	// Track cleanup callbacks
	var cleanupCount atomic.Int32
	var cleanupCallbackInvoked atomic.Bool

	// Create server with auto-cleanup enabled
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				PollInterval: 50 * time.Millisecond,
			},
		},
		EnableAutoCleanup:  true,
		CleanupInterval:    500 * time.Millisecond, // Cleanup every 500ms for testing
		CleanupRetention:   200 * time.Millisecond, // Keep tasks for 200ms
		OnTasksCleaned: func(ctx context.Context, count int, states []ergon.TaskState) {
			cleanupCallbackInvoked.Store(true)
			cleanupCount.Add(int32(count))
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

	// Enqueue several tasks
	taskCount := 5
	for i := 0; i < taskCount; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: "test task",
		})
		AssertNoError(t, err, "failed to enqueue task")
	}

	// Wait for tasks to complete
	time.Sleep(300 * time.Millisecond)

	// Verify tasks are completed
	filter := &ergon.TaskFilter{
		State: ergon.StateCompleted,
	}
	completedTasks, err := store.ListTasks(ctx, filter)
	AssertNoError(t, err, "failed to list completed tasks")
	AssertEquals(t, len(completedTasks), taskCount, "expected all tasks to be completed")

	// Wait for cleanup to run (retention is 200ms, cleanup interval is 500ms, so wait 800ms)
	time.Sleep(800 * time.Millisecond)

	// Verify tasks were cleaned up
	completedTasks, err = store.ListTasks(ctx, filter)
	AssertNoError(t, err, "failed to list completed tasks after cleanup")
	AssertEquals(t, len(completedTasks), 0, "expected all completed tasks to be cleaned up")

	// Verify callback was invoked
	AssertEquals(t, cleanupCallbackInvoked.Load(), true, "expected cleanup callback to be invoked")
	AssertEquals(t, int(cleanupCount.Load()), taskCount, "expected cleanup count to match task count")
}

// TestAutoCleanup_MultipleStates tests cleanup of failed and cancelled tasks
func TestAutoCleanup_MultipleStates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Create workers - all complete successfully for simplicity
	workers := ergon.NewWorkers()
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return nil
	})

	// Create server with auto-cleanup of multiple states
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
		EnableAutoCleanup:  true,
		CleanupInterval:    500 * time.Millisecond,
		CleanupRetention:   200 * time.Millisecond,
		CleanupStates:      []ergon.TaskState{ergon.StateCompleted, ergon.StateFailed, ergon.StateCancelled},
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

	// Enqueue multiple tasks
	taskCount := 5
	for i := 0; i < taskCount; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("test task %d", i),
		})
		AssertNoError(t, err, "failed to enqueue task")
	}

	// Wait for all tasks to complete
	time.Sleep(400 * time.Millisecond)

	// Verify all tasks completed
	completedCount, _ := store.CountTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertEquals(t, completedCount, taskCount, "expected all tasks to be completed")

	// Wait for cleanup to run
	time.Sleep(800 * time.Millisecond)

	// Verify all completed tasks were cleaned up
	completedCount, _ = store.CountTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertEquals(t, completedCount, 0, "expected completed tasks to be cleaned up")
}

// TestAutoCleanup_Disabled tests that cleanup doesn't run when disabled
func TestAutoCleanup_Disabled(t *testing.T) {
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

	// Create server WITHOUT auto-cleanup
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
		EnableAutoCleanup: false, // Disabled
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

	// Enqueue tasks
	taskCount := 3
	for i := 0; i < taskCount; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: "test task",
		})
		AssertNoError(t, err, "failed to enqueue task")
	}

	// Wait for tasks to complete
	time.Sleep(200 * time.Millisecond)

	// Verify tasks are completed
	completedTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertNoError(t, err, "failed to list completed tasks")
	AssertEquals(t, len(completedTasks), taskCount, "expected all tasks to be completed")

	// Wait a bit more - cleanup should NOT run
	time.Sleep(500 * time.Millisecond)

	// Verify tasks are still there
	completedTasks, err = store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertNoError(t, err, "failed to list completed tasks")
	AssertEquals(t, len(completedTasks), taskCount, "expected tasks to still exist (cleanup disabled)")
}

// TestAutoCleanup_CustomRetention tests custom retention period
func TestAutoCleanup_CustomRetention(t *testing.T) {
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

	// Create server with long retention period
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
		EnableAutoCleanup:  true,
		CleanupInterval:    200 * time.Millisecond,
		CleanupRetention:   1 * time.Hour, // Keep for 1 hour
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

	// Enqueue tasks
	taskCount := 3
	for i := 0; i < taskCount; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: "test task",
		})
		AssertNoError(t, err, "failed to enqueue task")
	}

	// Wait for tasks to complete
	time.Sleep(200 * time.Millisecond)

	// Wait for cleanup attempts
	time.Sleep(500 * time.Millisecond)

	// Verify tasks are NOT cleaned up (retention is 1 hour)
	completedTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted})
	AssertNoError(t, err, "failed to list completed tasks")
	AssertEquals(t, len(completedTasks), taskCount, "expected tasks to remain (within retention period)")
}
