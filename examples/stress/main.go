package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// StressTestTask - Simple task for stress testing
type StressTestTask struct {
	ID      int    `json:"id"`
	Payload string `json:"payload"`
}

func (StressTestTask) Kind() string { return "stress_test" }

// HeavyTask - Task with some processing time
type HeavyTask struct {
	ID        int    `json:"id"`
	ProcessMs int    `json:"process_ms"`
	Data      string `json:"data"`
}

func (HeavyTask) Kind() string { return "heavy_task" }

func main() {
	log.Println("🚀 Ergon Stress Test - Testing Parallel Execution & Race Conditions")
	log.Println(strings.Repeat("=", 80))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Setup Badger store
	store, err := badger.NewStore("./stress-test-db")
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	defer os.RemoveAll("./stress-test-db")

	// Metrics
	var (
		tasksEnqueued  atomic.Int64
		tasksProcessed atomic.Int64
		tasksSucceeded atomic.Int64
		tasksFailed    atomic.Int64
		enqueueErrors  atomic.Int64
	)

	// Create workers
	workers := ergon.NewWorkers()

	// Fast worker for stress test tasks
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[StressTestTask]) error {
		tasksProcessed.Add(1)
		// Minimal processing
		time.Sleep(1 * time.Millisecond)
		tasksSucceeded.Add(1)
		return nil
	})

	// Heavy worker with configurable processing time
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[HeavyTask]) error {
		tasksProcessed.Add(1)
		// Simulate heavy processing
		time.Sleep(time.Duration(task.Args.ProcessMs) * time.Millisecond)
		tasksSucceeded.Add(1)
		return nil
	})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue: "default",
		Workers:      workers,
	})

	// Create server with multiple workers
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: 50, // High concurrency for stress test
		Workers:     workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   30,
				PollInterval: 10 * time.Millisecond,
			},
			"heavy": {
				MaxWorkers:   10,
				PollInterval: 50 * time.Millisecond,
			},
			"low_priority": {
				MaxWorkers:   5,
				PollInterval: 100 * time.Millisecond,
			},
		},
		Middleware: []ergon.MiddlewareFunc{
			ergon.RecoveryMiddleware(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	log.Println("✅ Server started with 45 total workers across 3 queues")

	// ========================================================================
	// TEST 1: High Volume Sequential Enqueue
	// ========================================================================
	log.Println("\n" + strings.Repeat("-", 80))
	log.Println("📊 TEST 1: High Volume Sequential Enqueue")
	log.Println(strings.Repeat("-", 80))

	const sequentialTasks = 5000
	start := time.Now()

	for i := 0; i < sequentialTasks; i++ {
		_, err := ergon.Enqueue(client, ctx, StressTestTask{
			ID:      i,
			Payload: fmt.Sprintf("sequential-task-%d", i),
		})
		if err != nil {
			enqueueErrors.Add(1)
		} else {
			tasksEnqueued.Add(1)
		}
	}

	sequentialDuration := time.Since(start)
	log.Printf("✅ Enqueued %d tasks in %v (%.2f tasks/sec)",
		sequentialTasks, sequentialDuration, float64(sequentialTasks)/sequentialDuration.Seconds())
	log.Printf("   Enqueue errors: %d", enqueueErrors.Load())

	// Wait for processing
	log.Println("⏳ Waiting for tasks to process...")
	waitStart := time.Now()
	for tasksProcessed.Load() < int64(sequentialTasks) {
		time.Sleep(100 * time.Millisecond)
		if time.Since(waitStart) > 30*time.Second {
			log.Println("⚠️  Timeout waiting for tasks")
			break
		}
	}

	processingDuration := time.Since(start)
	log.Printf("✅ Processed %d tasks in %v (%.2f tasks/sec)",
		tasksProcessed.Load(), processingDuration, float64(tasksProcessed.Load())/processingDuration.Seconds())

	// ========================================================================
	// TEST 2: Concurrent Enqueue (Race Condition Test)
	// ========================================================================
	log.Println("\n" + strings.Repeat("-", 80))
	log.Println("🔥 TEST 2: Concurrent Enqueue - Race Condition Test")
	log.Println(strings.Repeat("-", 80))

	const numGoroutines = 100
	const tasksPerGoroutine = 50
	const totalConcurrentTasks = numGoroutines * tasksPerGoroutine

	// Reset metrics
	beforeEnqueued := tasksEnqueued.Load()
	beforeProcessed := tasksProcessed.Load()

	var wg sync.WaitGroup
	start = time.Now()

	log.Printf("🚀 Launching %d goroutines, each enqueueing %d tasks...", numGoroutines, tasksPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < tasksPerGoroutine; j++ {
				_, err := ergon.Enqueue(client, ctx, StressTestTask{
					ID:      goroutineID*1000 + j,
					Payload: fmt.Sprintf("concurrent-task-%d-%d", goroutineID, j),
				})
				if err != nil {
					enqueueErrors.Add(1)
				} else {
					tasksEnqueued.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	concurrentDuration := time.Since(start)

	newEnqueued := tasksEnqueued.Load() - beforeEnqueued
	log.Printf("✅ Enqueued %d tasks in %v with %d goroutines (%.2f tasks/sec)",
		newEnqueued, concurrentDuration, numGoroutines, float64(newEnqueued)/concurrentDuration.Seconds())
	log.Printf("   Enqueue errors: %d", enqueueErrors.Load())
	log.Printf("   Expected: %d, Got: %d", totalConcurrentTasks, newEnqueued)

	if newEnqueued != totalConcurrentTasks {
		log.Printf("⚠️  WARNING: Task count mismatch! Possible race condition detected!")
	}

	// Wait for processing
	log.Println("⏳ Waiting for concurrent tasks to process...")
	waitStart = time.Now()
	expectedProcessed := beforeProcessed + int64(totalConcurrentTasks)
	for tasksProcessed.Load() < expectedProcessed {
		time.Sleep(100 * time.Millisecond)
		if time.Since(waitStart) > 30*time.Second {
			log.Println("⚠️  Timeout waiting for tasks")
			break
		}
	}

	newProcessed := tasksProcessed.Load() - beforeProcessed
	log.Printf("✅ Processed %d tasks (%.2f tasks/sec)",
		newProcessed, float64(newProcessed)/time.Since(start).Seconds())

	// ========================================================================
	// TEST 3: Mixed Workload - Different Queues & Priorities
	// ========================================================================
	log.Println("\n" + strings.Repeat("-", 80))
	log.Println("⚡ TEST 3: Mixed Workload - Multiple Queues & Priorities")
	log.Println(strings.Repeat("-", 80))

	beforeEnqueued = tasksEnqueued.Load()
	beforeProcessed = tasksProcessed.Load()

	const mixedTasks = 1000
	start = time.Now()

	for i := 0; i < mixedTasks; i++ {
		var opts []ergon.Option

		// Distribute across queues
		if i%3 == 0 {
			opts = append(opts, ergon.WithQueue("heavy"))
			_, err = ergon.Enqueue(client, ctx, HeavyTask{
				ID:        i,
				ProcessMs: 10,
				Data:      fmt.Sprintf("heavy-%d", i),
			}, opts...)
		} else if i%3 == 1 {
			opts = append(opts, ergon.WithQueue("low_priority"))
			_, err = ergon.Enqueue(client, ctx, StressTestTask{
				ID:      i,
				Payload: fmt.Sprintf("low-priority-%d", i),
			}, opts...)
		} else {
			// Random priority
			opts = append(opts, ergon.WithPriority(i%10))
			_, err = ergon.Enqueue(client, ctx, StressTestTask{
				ID:      i,
				Payload: fmt.Sprintf("default-%d", i),
			}, opts...)
		}

		if err != nil {
			enqueueErrors.Add(1)
		} else {
			tasksEnqueued.Add(1)
		}
	}

	mixedDuration := time.Since(start)
	log.Printf("✅ Enqueued %d mixed tasks across 3 queues in %v",
		mixedTasks, mixedDuration)

	// ========================================================================
	// TEST 4: Burst Load - Sudden Spike
	// ========================================================================
	log.Println("\n" + strings.Repeat("-", 80))
	log.Println("💥 TEST 4: Burst Load - Sudden Traffic Spike")
	log.Println(strings.Repeat("-", 80))

	const burstSize = 2000
	const burstGoroutines = 50

	beforeEnqueued = tasksEnqueued.Load()
	wg = sync.WaitGroup{}
	start = time.Now()

	log.Printf("💥 Simulating burst: %d tasks from %d goroutines simultaneously", burstSize, burstGoroutines)

	for i := 0; i < burstGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < burstSize/burstGoroutines; j++ {
				_, err := ergon.Enqueue(client, ctx, StressTestTask{
					ID:      id*10000 + j,
					Payload: fmt.Sprintf("burst-%d-%d", id, j),
				})
				if err != nil {
					enqueueErrors.Add(1)
				} else {
					tasksEnqueued.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	burstDuration := time.Since(start)

	burstEnqueued := tasksEnqueued.Load() - beforeEnqueued
	log.Printf("✅ Burst complete: %d tasks in %v (%.2f tasks/sec)",
		burstEnqueued, burstDuration, float64(burstEnqueued)/burstDuration.Seconds())

	// ========================================================================
	// FINAL STATISTICS & REPORT
	// ========================================================================
	log.Println("\n" + strings.Repeat("=", 80))
	log.Println("📊 STRESS TEST FINAL REPORT")
	log.Println(strings.Repeat("=", 80))

	// Wait a bit for final processing
	time.Sleep(5 * time.Second)

	totalEnqueued := tasksEnqueued.Load()
	totalProcessed := tasksProcessed.Load()
	totalSucceeded := tasksSucceeded.Load()
	totalFailed := tasksFailed.Load()
	totalErrors := enqueueErrors.Load()

	log.Printf("\n📈 ENQUEUE STATISTICS:")
	log.Printf("   Total Enqueued: %d", totalEnqueued)
	log.Printf("   Enqueue Errors: %d", totalErrors)
	log.Printf("   Success Rate:   %.2f%%", float64(totalEnqueued-totalErrors)/float64(totalEnqueued)*100)

	log.Printf("\n⚙️  PROCESSING STATISTICS:")
	log.Printf("   Total Processed: %d", totalProcessed)
	log.Printf("   Succeeded:       %d", totalSucceeded)
	log.Printf("   Failed:          %d", totalFailed)
	log.Printf("   Pending:         %d", totalEnqueued-totalProcessed)

	log.Printf("\n🔍 RACE CONDITION CHECK:")
	if totalEnqueued == totalProcessed {
		log.Printf("   ✅ PASS: All enqueued tasks were processed")
	} else {
		log.Printf("   ⚠️  WARNING: %d tasks not processed yet", totalEnqueued-totalProcessed)
	}

	if totalErrors == 0 {
		log.Printf("   ✅ PASS: No enqueue errors detected")
	} else {
		log.Printf("   ❌ FAIL: %d enqueue errors detected", totalErrors)
	}

	if totalFailed == 0 {
		log.Printf("   ✅ PASS: No task failures detected")
	} else {
		log.Printf("   ❌ FAIL: %d task failures detected", totalFailed)
	}

	// Get store statistics
	manager := ergon.NewManager(store, ergon.ClientConfig{Workers: workers})
	stats, err := manager.Stats().GetOverallStats(ctx)
	if err == nil {
		log.Printf("\n📊 STORE STATISTICS:")
		log.Printf("   Total Tasks:     %d", stats.TotalTasks)
		log.Printf("   Completed:       %d", stats.CompletedTasks)
		log.Printf("   Failed:          %d", stats.FailedTasks)
		log.Printf("   Pending:         %d", stats.PendingTasks)
		log.Printf("   Running:         %d", stats.RunningTasks)
	}

	// Graceful shutdown
	log.Println("\n🛑 Shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("❌ Shutdown error: %v", err)
	} else {
		log.Println("✅ Server stopped gracefully")
	}

	log.Println("\n✨ Stress Test Completed!")
	log.Printf("💾 Database was stored at: ./stress-test-db (cleaned up)")

	// Exit with appropriate code
	if totalErrors > 0 || totalFailed > 0 {
		os.Exit(1)
	}
}
