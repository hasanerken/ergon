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

func TestStress_HighVolume_ergon.Enqueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numTasks = 10000
	start := time.Now()

	// Enqueue tasks
	for i := 0; i < numTasks; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("task-%d", i),
		})
		if err != nil {
			t.Fatalf("enqueue failed at %d: %v", i, err)
		}
	}

	duration := time.Since(start)
	t.Logf("Enqueued %d tasks in %v (%.2f tasks/sec)", numTasks, duration, float64(numTasks)/duration.Seconds())

	// Verify count
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != numTasks {
		t.Errorf("expected %d tasks, got %d", numTasks, count)
	}
}

func TestStress_Concurrentergon.Enqueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numGoroutines = 100
	const tasksPerGoroutine = 100

	var wg sync.WaitGroup
	var enqueued atomic.Int64
	var failed atomic.Int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < tasksPerGoroutine; j++ {
				_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
					Message: fmt.Sprintf("task-%d-%d", id, j),
				})
				if err != nil {
					failed.Add(1)
				} else {
					enqueued.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	expectedTotal := int64(numGoroutines * tasksPerGoroutine)
	t.Logf("Enqueued %d/%d tasks in %v with %d goroutines (%.2f tasks/sec)",
		enqueued.Load(), expectedTotal, duration, numGoroutines, float64(enqueued.Load())/duration.Seconds())

	if failed.Load() > 0 {
		t.Errorf("%d tasks failed to enqueue", failed.Load())
	}

	// Verify count
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != int(enqueued.Load()) {
		t.Errorf("expected %d tasks, got %d", enqueued.Load(), count)
	}
}

func TestStress_HighThroughput_Processing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store, err := badger.Newergon.Store(filepath.Join(helper.TempDir(), "queue_data"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	helper.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	const numTasks = 1000
	const numWorkers = 20

	var processed atomic.Int64

	workers := ergon.NewWorkers()
	ergon.Addergon.WorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		processed.Add(1)
		// Simulate minimal work
		time.Sleep(1 * time.Millisecond)
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   numWorkers,
				PollInterval: 10 * time.Millisecond,
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

	// Enqueue tasks
	start := time.Now()
	for i := 0; i < numTasks; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("task-%d", i),
		})
		AssertNoError(t, err, "enqueue failed")
	}
	enqueueTime := time.Since(start)

	// Wait for processing
	WaitForCondition(t, func() bool {
		return processed.Load() >= int64(numTasks)
	}, 30*time.Second, "tasks to be processed")

	processingTime := time.Since(start)

	t.Logf("Enqueued %d tasks in %v", numTasks, enqueueTime)
	t.Logf("Processed %d tasks in %v with %d workers (%.2f tasks/sec)",
		processed.Load(), processingTime, numWorkers, float64(processed.Load())/processingTime.Seconds())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestStress_MixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const duration = 5 * time.Second
	var enqueued, cancelled, deleted atomic.Int64

	var wg sync.WaitGroup

	// Enqueuer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			<-ticker.C
			_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "mixed"})
			if err == nil {
				enqueued.Add(1)
			}
		}
	}()

	// Canceller goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			<-ticker.C
			tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StatePending, Limit: 10})
			for _, task := range tasks {
				err := store.MarkCancelled(ctx, task.ID)
				if err == nil {
					cancelled.Add(1)
				}
			}
		}
	}()

	// Deleter goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			<-ticker.C
			tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{State: ergon.StateCancelled, Limit: 5})
			for _, task := range tasks {
				err := store.DeleteTask(ctx, task.ID)
				if err == nil {
					deleted.Add(1)
				}
			}
		}
	}()

	wg.Wait()

	t.Logf("Mixed workload stats:")
	t.Logf("  Enqueued: %d", enqueued.Load())
	t.Logf("  Cancelled: %d", cancelled.Load())
	t.Logf("  Deleted: %d", deleted.Load())

	// Final count
	count, _ := store.CountTasks(ctx, &ergon.TaskFilter{})
	t.Logf("  Remaining: %d", count)
}

func TestStress_BatchOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numBatches = 100
	const batchSize = 100

	start := time.Now()

	for i := 0; i < numBatches; i++ {
		argsList := make([]TestTaskArgs, batchSize)
		for j := 0; j < batchSize; j++ {
			argsList[j] = TestTaskArgs{
				Message: fmt.Sprintf("batch-%d-task-%d", i, j),
			}
		}

		_, err := ergon.EnqueueMany(client, ctx, argsList)
		AssertNoError(t, err, fmt.Sprintf("batch %d failed", i))
	}

	duration := time.Since(start)
	totalTasks := numBatches * batchSize

	t.Logf("Enqueued %d tasks in %d batches in %v (%.2f tasks/sec)",
		totalTasks, numBatches, duration, float64(totalTasks)/duration.Seconds())

	// Verify count
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != totalTasks {
		t.Errorf("expected %d tasks, got %d", totalTasks, count)
	}
}

func TestStress_QueueSaturation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Close()

	store, err := badger.Newergon.Store(filepath.Join(helper.TempDir(), "queue_data"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	helper.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	const numTasks = 500
	const numWorkers = 5

	var started, completed atomic.Int64
	var maxConcurrent atomic.Int64

	workers := ergon.NewWorkers()
	ergon.Addergon.WorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		current := started.Add(1)

		// Track max concurrent
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}

		// Simulate work
		time.Sleep(50 * time.Millisecond)

		completed.Add(1)
		return nil
	})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   numWorkers,
				PollInterval: 10 * time.Millisecond,
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

	// Enqueue many tasks quickly to saturate queue
	for i := 0; i < numTasks; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message: fmt.Sprintf("task-%d", i),
		})
		AssertNoError(t, err, "enqueue failed")
	}

	// Wait for completion
	WaitForCondition(t, func() bool {
		return completed.Load() >= int64(numTasks)
	}, 60*time.Second, "all tasks to complete")

	t.Logf("Queue saturation stats:")
	t.Logf("  Max concurrent: %d (expected ~%d)", maxConcurrent.Load(), numWorkers)
	t.Logf("  Total completed: %d", completed.Load())

	// Max concurrent should not exceed worker count by much
	if maxConcurrent.Load() > int64(numWorkers+5) {
		t.Errorf("max concurrent %d exceeded worker count %d by too much", maxConcurrent.Load(), numWorkers)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}

func TestStress_MemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const numIterations = 10
	const tasksPerIteration = 1000

	for i := 0; i < numIterations; i++ {
		// Enqueue tasks
		for j := 0; j < tasksPerIteration; j++ {
			_, err := ergon.Enqueue(client, ctx, TestTaskArgs{
				Message: fmt.Sprintf("iter-%d-task-%d", i, j),
			})
			AssertNoError(t, err, "enqueue failed")
		}

		// Process and clean up
		tasks, _ := store.ListTasks(ctx, &ergon.TaskFilter{})
		for _, task := range tasks {
			_ = store.MarkCompleted(ctx, task.ID, nil)
			_ = store.DeleteTask(ctx, task.ID)
		}

		if i%2 == 0 {
			t.Logf("Iteration %d/%d completed", i+1, numIterations)
		}
	}

	// Final count should be 0
	count, err := store.CountTasks(ctx, &ergon.TaskFilter{})
	AssertNoError(t, err, "count failed")

	if count != 0 {
		t.Errorf("expected 0 remaining tasks, got %d", count)
	}

	t.Logf("Processed %d total tasks across %d iterations", numIterations*tasksPerIteration, numIterations)
}

func Benchmarkergon.Enqueue(b *testing.B) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "bench"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEnqueueParallel(b *testing.B) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := ergon.Enqueue(client, ctx, TestTaskArgs{Message: "bench"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDequeue(b *testing.B) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	// Pre-populate tasks
	for i := 0; i < b.N; i++ {
		_, _ = ergon.Enqueue(client, ctx, TestTaskArgs{Message: "bench"})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := store.Dequeue(ctx, []string{"default"}, "worker-1")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func Benchmarkergon.EnqueueMany(b *testing.B) {
	store := mock.NewPostgresergon.Store()
	ctx := context.Background()

	workers := CreateTestWorkers()
	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workers})

	const batchSize = 100
	argsList := make([]TestTaskArgs, batchSize)
	for i := 0; i < batchSize; i++ {
		argsList[i] = TestTaskArgs{Message: "bench"}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := ergon.EnqueueMany(client, ctx, argsList)
		if err != nil {
			b.Fatal(err)
		}
	}
}
