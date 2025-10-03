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

type QuickTask struct {
	ID int `json:"id"`
}

func (QuickTask) Kind() string { return "quick" }

type QuickWorker struct{}

func (w *QuickWorker) Work(ctx context.Context, task *ergon.Task[QuickTask]) error {
	time.Sleep(2 * time.Millisecond)
	return nil
}

func main() {
	log.SetFlags(log.Ltime)

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   BadgerDB vs PostgreSQL Performance Comparison           ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Test with 200 tasks, 20 workers
	workers := 20
	tasks := 200

	fmt.Printf("📊 Test Configuration: %d workers, %d tasks per test\n", workers, tasks)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Test BadgerDB
	badgerSeq, badgerConc := testBadger(workers, tasks)

	// Test PostgreSQL
	pgSeq, pgConc := testPostgres(workers, tasks)

	// Compare
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📈 Performance Comparison:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("\n   Sequential Enqueue:\n")
	fmt.Printf("      BadgerDB:    %.0f tasks/sec\n", badgerSeq)
	fmt.Printf("      PostgreSQL:  %.0f tasks/sec", pgSeq)
	if pgSeq > badgerSeq {
		fmt.Printf(" (%.1fx faster) ✨\n", pgSeq/badgerSeq)
	} else {
		fmt.Printf("\n")
	}

	fmt.Printf("\n   Concurrent Enqueue:\n")
	fmt.Printf("      BadgerDB:    %.0f tasks/sec\n", badgerConc)
	fmt.Printf("      PostgreSQL:  %.0f tasks/sec", pgConc)
	if pgConc > badgerConc {
		fmt.Printf(" (%.1fx faster) ✨\n", pgConc/badgerConc)
	} else {
		fmt.Printf("\n")
	}

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ Comparison complete!")
	fmt.Println()

	os.RemoveAll("./quick-badger-db")
}

func testBadger(workers, taskCount int) (seqTPS, concTPS float64) {
	ctx := context.Background()

	os.RemoveAll("./quick-badger-db")
	store, _ := badger.NewStore("./quick-badger-db")
	defer store.Close()

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &QuickWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})
	server, _ := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: workers,
		Workers:     workerRegistry,
		Queues: map[string]ergon.QueueConfig{
			"default": {MaxWorkers: workers, PollInterval: 10 * time.Millisecond},
		},
	})

	server.Start(ctx)
	defer server.Stop(ctx)

	fmt.Println("🗄️  BadgerDB:")

	// Sequential
	start := time.Now()
	for i := 0; i < taskCount; i++ {
		ergon.Enqueue(client, ctx, QuickTask{ID: i})
	}
	seqTPS = float64(taskCount) / time.Since(start).Seconds()
	fmt.Printf("   Sequential: %.0f tasks/sec\n", seqTPS)

	time.Sleep(2 * time.Second)
	// BadgerDB doesn't need explicit cleanup - just continue

	// Concurrent
	var wg sync.WaitGroup
	var enqueued atomic.Int64
	start = time.Now()
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < taskCount/10; i++ {
				if _, err := ergon.Enqueue(client, ctx, QuickTask{ID: offset + i}); err == nil {
					enqueued.Add(1)
				}
			}
		}(g * (taskCount / 10))
	}
	wg.Wait()
	concTPS = float64(enqueued.Load()) / time.Since(start).Seconds()
	fmt.Printf("   Concurrent: %.0f tasks/sec\n", concTPS)

	time.Sleep(2 * time.Second)
	fmt.Println()
	return
}

func testPostgres(workers, taskCount int) (seqTPS, concTPS float64) {
	ctx := context.Background()

	db, _ := sql.Open("postgres", "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable")
	defer db.Close()
	db.Exec("TRUNCATE queue_tasks CASCADE")

	store, _ := postgres.NewStore(db)
	defer store.Close()

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &QuickWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})
	server, _ := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: workers,
		Workers:     workerRegistry,
		Queues: map[string]ergon.QueueConfig{
			"default": {MaxWorkers: workers, PollInterval: 10 * time.Millisecond},
		},
	})

	server.Start(ctx)
	defer server.Stop(ctx)

	fmt.Println("🐘 PostgreSQL:")

	// Sequential
	start := time.Now()
	for i := 0; i < taskCount; i++ {
		ergon.Enqueue(client, ctx, QuickTask{ID: i})
	}
	seqTPS = float64(taskCount) / time.Since(start).Seconds()
	fmt.Printf("   Sequential: %.0f tasks/sec\n", seqTPS)

	time.Sleep(2 * time.Second)
	db.Exec("TRUNCATE queue_tasks CASCADE")

	// Concurrent
	var wg sync.WaitGroup
	var enqueued atomic.Int64
	start = time.Now()
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < taskCount/10; i++ {
				if _, err := ergon.Enqueue(client, ctx, QuickTask{ID: offset + i}); err == nil {
					enqueued.Add(1)
				}
			}
		}(g * (taskCount / 10))
	}
	wg.Wait()
	concTPS = float64(enqueued.Load()) / time.Since(start).Seconds()
	fmt.Printf("   Concurrent: %.0f tasks/sec\n", concTPS)

	time.Sleep(2 * time.Second)
	fmt.Println()
	return
}
