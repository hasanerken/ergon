package integration_test

import (
	"github.com/hasanerken/ergon"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hasanerken/ergon/store/badger"
)

func TestIntegration_Badger_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store := helper.NewBadgerStore()
	ctx := context.Background()

	// Create workers
	workers := ergon.NewWorkers()
	executed := make(chan string, 10)

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed <- task.Args.Message
		return nil
	})

	// Create client and server
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

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

	go func() {
		_ = server.Start(serverCtx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Enqueue task
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "hello"})
	AssertNoError(t, err, "enqueue failed")

	// Wait for execution
	select {
	case msg := <-executed:
		if msg != "hello" {
			t.Errorf("expected message 'hello', got %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task execution")
	}

	// Verify task completed
	completedTask := WaitForergon.TaskState(t, store, task.ID, ergon.StateCompleted, 2*time.Second)
	if completedTask.State != ergon.StateCompleted {
		t.Errorf("expected state completed, got %s", completedTask.State)
	}

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	err = server.Stop(shutdownCtx)
	AssertNoError(t, err, "server shutdown failed")
}

func TestIntegration_Badger_MultipleQueues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := ergon.NewWorkers()
	executed := make(chan string, 100)

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed <- task.Queue + ":" + task.Args.Message
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"high": {
				MaxWorkers:   2,
				Priority:     10,
				PollInterval: 50 * time.Millisecond,
			},
			"low": {
				MaxWorkers:   1,
				Priority:     1,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Enqueue tasks to different queues
	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "task1"}, ergon.WithQueue("high"))
	AssertNoError(t, err, "enqueue high failed")

	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "task2"}, ergon.WithQueue("low"))
	AssertNoError(t, err, "enqueue low failed")

	// Wait for both executions
	results := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case msg := <-executed:
			results[msg] = true
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for task execution")
		}
	}

	if !results["high:task1"] {
		t.Error("high priority task not executed")
	}
	if !results["low:task2"] {
		t.Error("low priority task not executed")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestIntegration_Badger_Retry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := ergon.NewWorkers()
	attempts := 0

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[FailingTaskArgs]) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
		DefaultRetries: 5,
		RetryDelayFunc: func(task *ergon.InternalTask, attempt int, err error) time.Duration {
			return 100 * time.Millisecond // Fast retry for testing
		},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Enqueue failing task
	task, err := ergon.Enqueue(client, ctx, FailingTaskArgs{Message: "test", Error: errors.New("fail")})
	AssertNoError(t, err, "enqueue failed")

	// Wait for task to complete after retries
	WaitForergon.TaskState(t, store, task.ID, ergon.StateCompleted, 5*time.Second)

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestIntegration_Badger_Scheduling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := ergon.NewWorkers()
	executed := make(chan time.Time, 10)

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed <- time.Now()
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Schedule task for 500ms in future
	scheduledTime := time.Now().Add(500 * time.Millisecond)
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "scheduled"},
		ergon.WithScheduledAt(scheduledTime),
	)
	AssertNoError(t, err, "enqueue failed")

	// Verify task is in scheduled state
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")
	if retrieved.State != ergon.StateScheduled {
		t.Errorf("expected state scheduled, got %s", retrieved.State)
	}

	// Wait for execution
	select {
	case executedAt := <-executed:
		// Should execute after scheduled time
		if executedAt.Before(scheduledTime) {
			t.Error("task executed before scheduled time")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for scheduled task execution")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestIntegration_Badger_Uniqueness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// First unique task should succeed
	task1, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "unique"},
		ergon.WithUnique(1*time.Hour),
	)
	AssertNoError(t, err, "first unique task failed")

	// Duplicate should fail
	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "unique"},
		ergon.WithUnique(1*time.Hour),
	)
	if !errors.Is(err, ergon.ErrDuplicateTask) {
		t.Errorf("expected ergon.ErrDuplicateTask, got %v", err)
	}

	// Different args should succeed
	task2, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "different"},
		ergon.WithUnique(1*time.Hour),
	)
	AssertNoError(t, err, "different unique task failed")

	if task1.ID == task2.ID {
		t.Error("different tasks should have different IDs")
	}
}

func TestIntegration_Badger_PauseResume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := ergon.NewWorkers()
	executed := make(chan string, 10)

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed <- task.Args.Message
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"pauseable": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Pause queue
	err = client.PauseQueue(ctx, "pauseable")
	AssertNoError(t, err, "pause failed")

	// Enqueue task while paused
	_, err = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "paused"}, ergon.WithQueue("pauseable"))
	AssertNoError(t, err, "enqueue failed")

	// Should not execute while paused
	select {
	case <-executed:
		t.Error("task should not execute while queue is paused")
	case <-time.After(500 * time.Millisecond):
		// Expected - task should not execute
	}

	// Resume queue
	err = client.ResumeQueue(ctx, "pauseable")
	AssertNoError(t, err, "resume failed")

	// Should execute after resume
	select {
	case msg := <-executed:
		if msg != "paused" {
			t.Errorf("expected message 'paused', got %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task execution after resume")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestIntegration_Badger_Middleware(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := CreateTestWorkers()

	middlewareCalled := false
	middleware := func(next ergon.WorkerFunc) ergon.WorkerFunc {
		return func(ctx context.Context, task *ergon.InternalTask) error {
			middlewareCalled = true
			return next(ctx, task)
		}
	}

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   1,
				PollInterval: 50 * time.Millisecond,
			},
		},
		Middleware: []ergon.MiddlewareFunc{middleware},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Enqueue task
	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "test"})
	AssertNoError(t, err, "enqueue failed")

	// Wait for completion
	WaitForergon.TaskState(t, store, task.ID, ergon.StateCompleted, 2*time.Second)

	if !middlewareCalled {
		t.Error("middleware was not called")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestIntegration_Badger_BatchEnqueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()


	ctx := context.Background()

	workers := ergon.NewWorkers()
	executed := make(chan string, 100)

	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed <- task.Args.Message
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   5,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = server.Start(serverCtx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Batch enqueue 10 tasks
	argsList := make([]TestTaskArgs, 10)
	for i := 0; i < 10; i++ {
		argsList[i] = TestTaskArgs{Message: string(rune('A' + i))}
	}

	tasks, err := ergon.EnqueueMany(client, ctx, argsList)
	AssertNoError(t, err, "batch enqueue failed")

	if len(tasks) != 10 {
		t.Errorf("expected 10 tasks, got %d", len(tasks))
	}

	// Wait for all executions
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		select {
		case msg := <-executed:
			results[msg] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for task %d", i)
		}
	}

	if len(results) != 10 {
		t.Errorf("expected 10 unique results, got %d", len(results))
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}
