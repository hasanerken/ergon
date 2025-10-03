package integration_test

import (
	"github.com/hasanerken/ergon"
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hasanerken/ergon/store/badger"
	"github.com/hasanerken/ergon/store/mock"
)

// Run with: go test -race -run Race

func TestRace_ConcurrentEnqueue(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numGoroutines = 50
	const tasksPerGoroutine = 20

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < tasksPerGoroutine; j++ {
				_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
					Message: fmt.Sprintf("task-%d-%d", id, j),
				})
				if err != nil {
					t.Errorf("enqueue failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify count
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	expected := numGoroutines * tasksPerGoroutine
	if count != expected {
		t.Errorf("expected %d tasks, got %d", expected, count)
	}
}

func TestRace_ConcurrentDequeue(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numTasks = 100
	const numWorkers = 10

	// Enqueue tasks
	for i := 0; i < numTasks; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("task-%d", i),
		})
		AssertNoError(t, err, "enqueue failed")
	}

	var wg sync.WaitGroup
	var dequeued atomic.Int64
	var failed atomic.Int64
	taskIDs := sync.Map{}

	// Concurrent dequeuers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				task, err := store.Dequeue(ctx, []string{"default"}, fmt.Sprintf("worker-%d", workerID))
				if err != nil {
					failed.Add(1)
					break
				}

				// Check for duplicate dequeue
				if _, exists := taskIDs.LoadOrergon.Store(task.ID, true); exists {
					t.Errorf("task %s dequeued multiple times!", task.ID)
				}

				dequeued.Add(1)

				// Mark completed
				_ = store.MarkCompleted(ctx, task.ID, nil)
			}
		}(i)
	}

	wg.Wait()

	if dequeued.Load() != int64(numTasks) {
		t.Errorf("expected %d dequeued tasks, got %d", numTasks, dequeued.Load())
	}
}

func TestRace_ConcurrentStateChanges(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "race"})
	AssertNoError(t, err, "enqueue failed")

	var wg sync.WaitGroup

	// Multiple goroutines trying to change state
	operations := []func(){
		func() { _ = store.MarkRunning(ctx, task.ID, "worker-1") },
		func() { _ = store.MarkRunning(ctx, task.ID, "worker-2") },
		func() { _ = store.MarkCompleted(ctx, task.ID, []byte("result")) },
		func() { _ = store.MarkCancelled(ctx, task.ID) },
		func() { _ = store.MarkRetrying(ctx, task.ID, time.Now().Add(1*time.Second)) },
	}

	for _, op := range operations {
		wg.Add(1)
		go func(operation func()) {
			defer wg.Done()
			operation()
		}(op)
	}

	wg.Wait()

	// Task should be in a valid state (no corruption)
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	validStates := []ergon.TaskState{ergon.StateRunning, ergon.StateCompleted, ergon.StateCancelled, ergon.StateRetrying}
	isValid := false
	for _, state := range validStates {
		if retrieved.State == state {
			isValid = true
			break
		}
	}

	if !isValid {
		t.Errorf("task in invalid state: %s", retrieved.State)
	}
}

func TestRace_WorkerRegistration(t *testing.T) {
	workers := ergon.NewWorkers()

	var wg sync.WaitGroup
	panics := atomic.Int32{}

	// Try to register same worker from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics.Add(1)
				}
			}()

			ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
				return nil
			})
		}()
	}

	wg.Wait()

	// Should have 9 panics (only first registration succeeds)
	if panics.Load() != 9 {
		t.Errorf("expected 9 panics, got %d", panics.Load())
	}

	// Verify worker is registered
	if !workers.Has("test_task") {
		t.Error("worker should be registered")
	}

	// Should only have one worker
	kinds := workers.Kinds()
	if len(kinds) != 1 {
		t.Errorf("expected 1 worker kind, got %d", len(kinds))
	}
}

func TestRace_QueuePauseResume(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	var wg sync.WaitGroup

	// Concurrent pause/resume
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			if id%2 == 0 {
				_ = store.PauseQueue(ctx, "test-queue")
			} else {
				_ = store.ResumeQueue(ctx, "test-queue")
			}
		}(i)
	}

	wg.Wait()

	// Queue should be in a valid state
	info, err := store.Getergon.QueueInfo(ctx, "test-queue")
	AssertNoError(t, err, "get queue info failed")

	// State should be either paused or not paused (not corrupted)
	if info.Name != "test-queue" {
		t.Error("queue state corrupted")
	}
}

func TestRace_ConcurrentUpdate(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "update"})
	AssertNoError(t, err, "enqueue failed")

	var wg sync.WaitGroup

	// Concurrent updates to different fields
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			priority := id * 10
			_ = store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{
				Priority: &priority,
			})
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			metadata := map[string]interface{}{
				fmt.Sprintf("key-%d", id): id,
			}
			_ = store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{
				Metadata: metadata,
			})
		}(i)
	}

	wg.Wait()

	// Task should still be retrievable and valid
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.ID != task.ID {
		t.Error("task corrupted")
	}
}

func TestRace_ConcurrentListAndModify(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Enqueue initial tasks
	for i := 0; i < 50; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("task-%d", i),
		})
		AssertNoError(t, err, "enqueue failed")
	}

	var wg sync.WaitGroup
	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-done:
					return
				default:
					_, _ = store.ListTasks(ctx, &ergon.TaskFilter{})
					_, _ = store.CountTasks(ctx, &ergon.TaskFilter{State: ergon.StatePending})
				}
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				select {
				case <-done:
					return
				default:
					_, _ = ergon.Enqueue(client, ctx, TestTaskArgs{
						Message: fmt.Sprintf("concurrent-%d-%d", id, j),
					})

					tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{Limit: 5})
					for _, task := range tasks {
						_ = store.MarkCompleted(ctx, task.ID, nil)
					}
				}
			}
		}(i)
	}

	// Let it run for a bit
	time.Sleep(2 * time.Second)
	close(done)

	wg.Wait()

	// ergon.Store should still be consistent
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count < 0 {
		t.Error("negative task count - store corrupted")
	}
}

func TestRace_ConcurrentGroupOperations(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const groupKey = "test-group"
	var wg sync.WaitGroup

	// Concurrent group additions
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
				Message: fmt.Sprintf("group-task-%d", id),
			}, ergon.WithGroup(groupKey))

			if err != nil {
				t.Errorf("enqueue to group failed: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Verify group integrity
	groupTasks, err := store.GetGroup(ctx, groupKey)
	AssertNoError(t, err, "get group failed")

	if len(groupTasks) != 20 {
		t.Errorf("expected 20 tasks in group, got %d", len(groupTasks))
	}

	// Concurrent group reads and consume
	var consumeWg sync.WaitGroup
	var consumeCount atomic.Int32

	for i := 0; i < 5; i++ {
		consumeWg.Add(1)
		go func() {
			defer consumeWg.Done()

			tasks, err := store.ConsumeGroup(ctx, groupKey)
			if err == nil && len(tasks) > 0 {
				consumeCount.Add(1)
			}
		}()
	}

	consumeWg.Wait()

	// Only one consume should succeed
	if consumeCount.Load() > 1 {
		t.Errorf("multiple consumes succeeded: %d", consumeCount.Load())
	}
}

func TestRace_ServerStartStop(t *testing.T) {
	helper := NewTestHelper(t)
	defer helper.Close()

	store, err := badger.Newergon.Store(filepath.Join(helper.TempDir(), "queue_data"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	helper.Cleanup(func() { _ = store.Close() })

	workers := CreateTestWorkers()

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				PollInterval: 50 * time.Millisecond,
			},
		},
	})
	AssertNoError(t, err, "failed to create server")

	var wg sync.WaitGroup

	// Concurrent start/stop attempts
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if id%2 == 0 {
				_ = server.Start(ctx)
			} else {
				time.Sleep(100 * time.Millisecond)
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer shutdownCancel()
				_ = server.Stop(shutdownCtx)
			}
		}(i)
	}

	wg.Wait()

	// Server should still be functional
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Should be able to start and stop cleanly
	go func() {
		_ = server.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	err = server.Stop(shutdownCtx)
	if err != nil && err != ergon.ErrServerStopped {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRace_ConcurrentMetadataAccess(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	task, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "metadata"})
	AssertNoError(t, err, "enqueue failed")

	var wg sync.WaitGroup

	// Concurrent metadata updates
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			metadata := map[string]interface{}{
				fmt.Sprintf("key-%d", id): id,
				"common":                  id,
			}

			_ = store.UpdateTask(ctx, task.ID, &ergon.TaskUpdate{
				Metadata: metadata,
			})
		}(i)
	}

	// Concurrent metadata reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				retrieved, err := store.GetTask(ctx, task.ID)
				if err == nil && retrieved.Metadata != nil {
					// Access metadata
					_ = retrieved.Metadata["common"]
				}
			}
		}()
	}

	wg.Wait()

	// Task should still be valid
	retrieved, err := store.GetTask(ctx, task.ID)
	AssertNoError(t, err, "get task failed")

	if retrieved.Metadata == nil {
		t.Error("metadata lost")
	}
}

func TestRace_ConcurrentStoreOperations(t *testing.T) {
	store := mock.NewPostgresStore()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	var wg sync.WaitGroup
	operations := 100

	// Mix of all store operations
	for i := 0; i < operations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			switch id % 7 {
			case 0: // Enqueue
				_, _ = ergon.Enqueue(client, ctx, TestTaskArgs{Message: fmt.Sprintf("task-%d", id)})
			case 1: // List
				_, _ = store.ListTasks(ctx, &ergon.TaskFilter{})
			case 2: // Count
				_, _ = store.CountTasks(ctx, &ergon.TaskFilter{})
			case 3: // List queues
				_, _ = store.ListQueues(ctx)
			case 4: // Dequeue
				_, _ = store.Dequeue(ctx, []string{"default"}, fmt.Sprintf("worker-%d", id))
			case 5: // Update
				tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{Limit: 1})
				if len(tasks) > 0 {
					priority := id
					_ = store.UpdateTask(ctx, tasks[0].ID, &ergon.TaskUpdate{Priority: &priority})
				}
			case 6: // Delete
				tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StateCompleted, Limit: 1})
				if len(tasks) > 0 {
					_ = store.DeleteTask(ctx, tasks[0].ID)
				}
			}
		}(i)
	}

	wg.Wait()

	// ergon.Store should still be functional
	_, err := store.ListTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "store corrupted after concurrent operations")
}
