package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/postgres"
)

type PerfTask struct {
	ID int `json:"id"`
}

func (PerfTask) Kind() string { return "perf" }

type PerfWorker struct{}

func (w *PerfWorker) Work(ctx context.Context, task *ergon.Task[PerfTask]) error {
	return nil
}

func main() {
	log.SetFlags(log.Ltime)
	ctx := context.Background()

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   PostgreSQL Performance - Optimized Configuration        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	dsn := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"

	fmt.Println("🔧 Testing 3 configurations:")
	fmt.Println("   1. Default (synchronous_commit=on)")
	fmt.Println("   2. Async commit (synchronous_commit=off)")
	fmt.Println("   3. Batch operations with async commit")
	fmt.Println()

	// Test 1: Default settings
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 Test 1: Default Settings (synchronous_commit=on)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	testConfig(ctx, dsn, false, false, 1000, 10)

	// Test 2: Async commit
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 Test 2: Async Commit (synchronous_commit=off)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	testConfig(ctx, dsn, true, false, 1000, 10)

	// Test 3: Batch + Async
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 Test 3: Batch Operations + Async Commit")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	testConfig(ctx, dsn, true, true, 10000, 50)

	fmt.Println("\n✅ All tests completed!")
}

func testConfig(ctx context.Context, dsn string, asyncCommit bool, useBatch bool, taskCount int, concurrency int) {
	store, err := postgres.NewStorePgx(ctx, dsn)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer store.Close()

	// Apply async commit setting
	if asyncCommit {
		_, err = store.GetPool().Exec(ctx, "SET synchronous_commit = off")
		if err != nil {
			log.Printf("Warning: failed to set synchronous_commit: %v", err)
		}
	}

	// Clean database
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &PerfWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})

	// Sequential test
	fmt.Printf("\n📝 Sequential: Enqueueing %d tasks...\n", taskCount)
	start := time.Now()

	if useBatch {
		// Use batch API (EnqueueMany)
		args := make([]PerfTask, taskCount)
		for i := 0; i < taskCount; i++ {
			args[i] = PerfTask{ID: i}
		}
		_, err = ergon.EnqueueMany(client, ctx, args)
		if err != nil {
			log.Printf("Batch enqueue error: %v", err)
		}
	} else {
		// Individual enqueues
		for i := 0; i < taskCount; i++ {
			_, err := ergon.Enqueue(client, ctx, PerfTask{ID: i})
			if err != nil {
				log.Printf("Enqueue error: %v", err)
				break
			}
		}
	}

	seqDuration := time.Since(start)
	seqTPS := float64(taskCount) / seqDuration.Seconds()
	fmt.Printf("   ✅ %s %.0f tasks/sec (%.3f seconds)\n",
		getIcon(seqTPS), seqTPS, seqDuration.Seconds())

	// Clean for concurrent test
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	// Concurrent test
	fmt.Printf("\n📝 Concurrent: Enqueueing %d tasks with %d goroutines...\n", taskCount, concurrency)
	var wg sync.WaitGroup
	var enqueued atomic.Int64
	tasksPerGo := taskCount / concurrency

	start = time.Now()
	for g := 0; g < concurrency; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < tasksPerGo; i++ {
				if _, err := ergon.Enqueue(client, ctx, PerfTask{ID: offset + i}); err == nil {
					enqueued.Add(1)
				}
			}
		}(g * tasksPerGo)
	}
	wg.Wait()

	concDuration := time.Since(start)
	concTPS := float64(enqueued.Load()) / concDuration.Seconds()
	fmt.Printf("   ✅ %s %.0f tasks/sec (%.3f seconds)\n",
		getIcon(concTPS), concTPS, concDuration.Seconds())
}

func getIcon(tps float64) string {
	if tps > 20000 {
		return "🚀"
	} else if tps > 10000 {
		return "⚡"
	} else if tps > 5000 {
		return "🔥"
	} else if tps > 1000 {
		return "✨"
	}
	return "📊"
}
