package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
	"github.com/hasanerken/ergon/store/postgres"
)

// BenchTask for testing
type BenchTask struct {
	ID int `json:"id"`
}

func (BenchTask) Kind() string { return "bench" }

type BenchWorker struct{}

func (w *BenchWorker) Work(ctx context.Context, task *ergon.Task[BenchTask]) error {
	time.Sleep(5 * time.Millisecond) // Simulate work
	return nil
}

func main() {
	log.SetFlags(log.Ltime)

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   BadgerDB vs PostgreSQL Performance Comparison           ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Test configurations
	tests := []struct {
		name         string
		workers      int
		taskCount    int
		concurrentGo int // For concurrent enqueue test
	}{
		{"Small Load (10 workers)", 10, 500, 10},
		{"Medium Load (25 workers)", 25, 1000, 25},
		{"High Load (45 workers)", 45, 2000, 50},
	}

	for _, test := range tests {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("📊 Test: %s\n", test.name)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Test BadgerDB
		fmt.Println("🗄️  BadgerDB:")
		badgerResults := runTest("badger", test.workers, test.taskCount, test.concurrentGo)

		// Test PostgreSQL
		fmt.Println("\n🐘 PostgreSQL:")
		pgResults := runTest("postgres", test.workers, test.taskCount, test.concurrentGo)

		// Compare results
		fmt.Println("\n📈 Comparison:")
		fmt.Printf("   Sequential Enqueue:\n")
		fmt.Printf("      BadgerDB:    %.0f tasks/sec\n", badgerResults.seqThroughput)
		fmt.Printf("      PostgreSQL:  %.0f tasks/sec", pgResults.seqThroughput)
		if pgResults.seqThroughput > badgerResults.seqThroughput {
			fmt.Printf(" (%.1fx faster) ✨\n", pgResults.seqThroughput/badgerResults.seqThroughput)
		} else {
			fmt.Printf(" (%.1fx slower)\n", badgerResults.seqThroughput/pgResults.seqThroughput)
		}

		fmt.Printf("\n   Concurrent Enqueue:\n")
		fmt.Printf("      BadgerDB:    %.0f tasks/sec (%d errors)\n", badgerResults.concThroughput, badgerResults.enqueueErrors)
		fmt.Printf("      PostgreSQL:  %.0f tasks/sec (%d errors)", pgResults.concThroughput, pgResults.enqueueErrors)
		if pgResults.concThroughput > badgerResults.concThroughput {
			fmt.Printf(" (%.1fx faster) ✨\n", pgResults.concThroughput/badgerResults.concThroughput)
		} else {
			fmt.Printf(" (%.1fx slower)\n", badgerResults.concThroughput/pgResults.concThroughput)
		}

		fmt.Printf("\n   Processing:\n")
		fmt.Printf("      BadgerDB:    %d completed, %d failed\n", badgerResults.completed, badgerResults.failed)
		fmt.Printf("      PostgreSQL:  %d completed, %d failed\n", pgResults.completed, pgResults.failed)

		fmt.Println()
		time.Sleep(2 * time.Second) // Brief pause between tests
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ All tests completed!")
	fmt.Println()

	// Cleanup
	_ = os.RemoveAll("./bench-badger-db")
}

type testResults struct {
	seqThroughput  float64
	concThroughput float64
	enqueueErrors  int64
	completed      int64
	failed         int64
}

func runTest(storeType string, workers, taskCount, concurrentGo int) testResults {
	ctx := context.Background()
	var store ergon.Store
	var db *sql.DB
	var err error

	// Create store
	if storeType == "badger" {
		os.RemoveAll("./bench-badger-db")
		store, err = badger.NewStore("./bench-badger-db")
	} else {
		connStr := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			// Clean up existing tasks
			_, _ = db.Exec("TRUNCATE tasks, task_groups CASCADE")
			store, err = postgres.NewStore(db)
		}
	}
	if err != nil {
		log.Printf("   Failed to create store: %v\n", err)
		return testResults{}
	}
	defer store.Close()
	if db != nil {
		defer db.Close()
	}

	// Setup workers
	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &BenchWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: workers,
		Workers:     workerRegistry,
		Queues: map[string]ergon.QueueConfig{
			"default": {MaxWorkers: workers, PollInterval: 10 * time.Millisecond},
		},
	})
	if err != nil {
		log.Printf("   Failed to create server: %v\n", err)
		return testResults{}
	}

	if err := server.Start(ctx); err != nil {
		log.Printf("   Failed to start server: %v\n", err)
		return testResults{}
	}
	defer server.Stop(ctx)

	var results testResults

	// Test 1: Sequential Enqueue
	start := time.Now()
	for i := 0; i < taskCount; i++ {
		_, err := ergon.Enqueue(client, ctx, BenchTask{ID: i})
		if err != nil {
			results.enqueueErrors++
		}
	}
	seqDuration := time.Since(start)
	results.seqThroughput = float64(taskCount) / seqDuration.Seconds()
	fmt.Printf("   Sequential: %d tasks in %v (%.0f tasks/sec)\n", taskCount, seqDuration, results.seqThroughput)

	// Wait for processing
	time.Sleep(time.Duration(taskCount/workers+5) * time.Second)

	// Check stats
	manager := ergon.NewManager(store, ergon.ClientConfig{Workers: workerRegistry})
	stats, _ := manager.Stats().GetOverallStats(ctx)
	fmt.Printf("   Processed:  %d completed, %d failed\n", stats.CompletedTasks, stats.FailedTasks)

	// Clean for next test
	if db != nil {
		_, _ = db.Exec("TRUNCATE tasks, task_groups CASCADE")
	}

	// Test 2: Concurrent Enqueue
	var wg sync.WaitGroup
	var enqueued, errors atomic.Int64
	start = time.Now()

	tasksPerGoroutine := taskCount / concurrentGo
	for g := 0; g < concurrentGo; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				_, err := ergon.Enqueue(client, ctx, BenchTask{ID: offset + i})
				if err != nil {
					errors.Add(1)
				} else {
					enqueued.Add(1)
				}
			}
		}(g * tasksPerGoroutine)
	}
	wg.Wait()
	concDuration := time.Since(start)
	results.concThroughput = float64(enqueued.Load()) / concDuration.Seconds()
	results.enqueueErrors = errors.Load()
	fmt.Printf("   Concurrent: %d tasks in %v (%.0f tasks/sec, %d errors)\n",
		enqueued.Load(), concDuration, results.concThroughput, errors.Load())

	// Wait for processing
	time.Sleep(time.Duration(taskCount/workers+5) * time.Second)

	// Final stats
	stats, _ = manager.Stats().GetOverallStats(ctx)
	results.completed = stats.CompletedTasks
	results.failed = stats.FailedTasks

	return results
}
