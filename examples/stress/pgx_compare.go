package main

import (
	"context"
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

type BenchTask struct {
	ID int `json:"id"`
}

func (BenchTask) Kind() string { return "bench" }

type BenchWorker struct{}

func (w *BenchWorker) Work(ctx context.Context, task *ergon.Task[BenchTask]) error {
	time.Sleep(2 * time.Millisecond)
	return nil
}

func main() {
	log.SetFlags(log.Ltime)

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   BadgerDB vs PostgreSQL (pgx) Performance Comparison     ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Restart PostgreSQL
	fmt.Println("🐘 Starting PostgreSQL...")
	os.Remove("/tmp/ergon_pg.log")
	// Note: User should start postgres manually or we use existing container

	ctx := context.Background()

	// Test configurations
	tests := []struct {
		name           string
		workers        int
		seqTasks       int
		concTasks      int
		concGoroutines int
	}{
		{"Light Load", 10, 200, 200, 10},
		{"Medium Load", 20, 500, 500, 20},
		{"Heavy Load", 50, 1000, 1000, 50},
	}

	for _, test := range tests {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("📊 %s: %d workers\n", test.name, test.workers)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Test BadgerDB
		badgerSeq, badgerConc := testBadger(ctx, test.workers, test.seqTasks, test.concTasks, test.concGoroutines)

		// Test PostgreSQL with pgx
		pgSeq, pgConc := testPostgresPgx(ctx, test.workers, test.seqTasks, test.concTasks, test.concGoroutines)

		// Compare
		fmt.Println("\n📈 Results:")
		fmt.Printf("   Sequential Enqueue:\n")
		fmt.Printf("      BadgerDB:    %7.0f tasks/sec\n", badgerSeq)
		fmt.Printf("      PostgreSQL:  %7.0f tasks/sec", pgSeq)
		if pgSeq > badgerSeq {
			fmt.Printf(" (%.1fx faster) ✨\n", pgSeq/badgerSeq)
		} else if badgerSeq > pgSeq {
			fmt.Printf(" (%.1fx slower)\n", badgerSeq/pgSeq)
		} else {
			fmt.Println()
		}

		fmt.Printf("\n   Concurrent Enqueue:\n")
		fmt.Printf("      BadgerDB:    %7.0f tasks/sec\n", badgerConc)
		fmt.Printf("      PostgreSQL:  %7.0f tasks/sec", pgConc)
		if pgConc > badgerConc {
			fmt.Printf(" (%.1fx faster) ✨\n", pgConc/badgerConc)
		} else if badgerConc > pgConc {
			fmt.Printf(" (%.1fx slower)\n", badgerConc/pgConc)
		} else {
			fmt.Println()
		}

		fmt.Println()
		time.Sleep(1 * time.Second)
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ All tests complete!")
	fmt.Println()

	os.RemoveAll("./pgx-badger-db")
}

func testBadger(ctx context.Context, workers, seqTasks, concTasks, concGoroutines int) (seqTPS, concTPS float64) {
	os.RemoveAll("./pgx-badger-db")
	store, _ := badger.NewStore("./pgx-badger-db")
	defer store.Close()

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &BenchWorker{})

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
	for i := 0; i < seqTasks; i++ {
		ergon.Enqueue(client, ctx, BenchTask{ID: i})
	}
	seqTPS = float64(seqTasks) / time.Since(start).Seconds()
	fmt.Printf("   Sequential: %.0f tasks/sec\n", seqTPS)

	time.Sleep(3 * time.Second)

	// Concurrent
	var wg sync.WaitGroup
	var enqueued atomic.Int64
	tasksPerGo := concTasks / concGoroutines

	start = time.Now()
	for g := 0; g < concGoroutines; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < tasksPerGo; i++ {
				if _, err := ergon.Enqueue(client, ctx, BenchTask{ID: offset + i}); err == nil {
					enqueued.Add(1)
				}
			}
		}(g * tasksPerGo)
	}
	wg.Wait()
	concTPS = float64(enqueued.Load()) / time.Since(start).Seconds()
	fmt.Printf("   Concurrent: %.0f tasks/sec\n", concTPS)

	time.Sleep(3 * time.Second)
	fmt.Println()
	return
}

func testPostgresPgx(ctx context.Context, workers, seqTasks, concTasks, concGoroutines int) (seqTPS, concTPS float64) {
	dsn := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"

	store, err := postgres.NewStorePgx(ctx, dsn)
	if err != nil {
		fmt.Printf("   ❌ Failed to connect: %v\n", err)
		return 0, 0
	}
	defer store.Close()

	// Clean database
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &BenchWorker{})

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

	fmt.Println("🐘 PostgreSQL (pgx):")

	// Sequential
	start := time.Now()
	for i := 0; i < seqTasks; i++ {
		if _, err := ergon.Enqueue(client, ctx, BenchTask{ID: i}); err != nil {
			fmt.Printf("   Enqueue error: %v\n", err)
			break
		}
	}
	seqTPS = float64(seqTasks) / time.Since(start).Seconds()
	fmt.Printf("   Sequential: %.0f tasks/sec\n", seqTPS)

	time.Sleep(3 * time.Second)
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	// Concurrent
	var wg sync.WaitGroup
	var enqueued, errors atomic.Int64
	tasksPerGo := concTasks / concGoroutines

	start = time.Now()
	for g := 0; g < concGoroutines; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < tasksPerGo; i++ {
				if _, err := ergon.Enqueue(client, ctx, BenchTask{ID: offset + i}); err == nil {
					enqueued.Add(1)
				} else {
					errors.Add(1)
				}
			}
		}(g * tasksPerGo)
	}
	wg.Wait()
	concTPS = float64(enqueued.Load()) / time.Since(start).Seconds()
	fmt.Printf("   Concurrent: %.0f tasks/sec", concTPS)
	if errors.Load() > 0 {
		fmt.Printf(" (%d errors)", errors.Load())
	}
	fmt.Println()

	time.Sleep(3 * time.Second)
	fmt.Println()
	return
}
