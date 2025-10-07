package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// TaskMetrics tracks task execution metrics
type TaskMetrics struct {
	started   atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	retried   atomic.Int64
}

// ProcessTaskArgs represents a generic task
type ProcessTaskArgs struct {
	ID      int    `json:"id"`
	Action  string `json:"action"`
	ShouldFail bool   `json:"should_fail"`
}

func (ProcessTaskArgs) Kind() string { return "process_task" }

// ProcessWorker processes tasks
type ProcessWorker struct{}

func (w *ProcessWorker) Work(ctx context.Context, task *ergon.Task[ProcessTaskArgs]) error {
	log.Printf("Processing task %d: %s", task.Args.ID, task.Args.Action)

	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Fail some tasks intentionally for demonstration
	if task.Args.ShouldFail {
		return fmt.Errorf("task %d failed as requested", task.Args.ID)
	}

	log.Printf("Task %d completed successfully", task.Args.ID)
	return nil
}

func main() {
	ctx := context.Background()

	// Initialize metrics tracker
	metrics := &TaskMetrics{}

	// Create store
	store, err := badger.NewStore("./data/callbacks_demo")
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &ProcessWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	// Create server with event callbacks
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:     workers,
		Concurrency: 5,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   3,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
		},

		// Event Callbacks for Monitoring
		OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
			metrics.started.Add(1)
			log.Printf("📊 [STARTED] Task %s (kind=%s, queue=%s)",
				task.ID, task.Kind, task.Queue)
		},

		OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
			metrics.completed.Add(1)
			log.Printf("✅ [COMPLETED] Task %s in %v (kind=%s)",
				task.ID, duration.Round(time.Millisecond), task.Kind)

			// Example: Send metrics to monitoring system
			// metrics.RecordTaskDuration(task.Kind, duration)
			// metrics.IncrementCounter("tasks.completed")
		},

		OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
			metrics.failed.Add(1)
			log.Printf("❌ [FAILED] Task %s: %v (kind=%s, retried=%d/%d)",
				task.ID, err, task.Kind, task.Retried, task.MaxRetries)

			// Example: Alert on critical failures
			if task.Queue == "critical" {
				log.Printf("🚨 ALERT: Critical task failed: %s", task.ID)
				// alerts.Send("Critical task failure", task)
			}

			// Example: Track failure metrics
			// metrics.IncrementCounter("tasks.failed", map[string]string{
			//     "kind": task.Kind,
			//     "queue": task.Queue,
			// })
		},

		OnTaskRetried: func(ctx context.Context, task *ergon.InternalTask, attempt int, nextRetry time.Time) {
			metrics.retried.Add(1)
			retryIn := time.Until(nextRetry).Round(time.Second)
			log.Printf("🔄 [RETRY] Task %s will retry in %v (attempt %d/%d)",
				task.ID, retryIn, attempt, task.MaxRetries)

			// Example: Track retry patterns
			// if attempt >= task.MaxRetries/2 {
			//     log.Printf("⚠️  Task %s is failing repeatedly", task.ID)
			// }
		},
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start server in background
	go func() {
		if err := server.Run(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n=== Example 1: Successful Tasks ===")
	fmt.Println("Enqueueing 5 tasks that will succeed")

	for i := 1; i <= 5; i++ {
		_, err := ergon.Enqueue(client, ctx, ProcessTaskArgs{
			ID:         i,
			Action:     "process_data",
			ShouldFail: false,
		})
		if err != nil {
			log.Printf("Failed to enqueue task %d: %v", i, err)
		}
	}

	// Wait for tasks to complete
	time.Sleep(2 * time.Second)

	fmt.Println("\n=== Example 2: Failing Tasks (with Retries) ===")
	fmt.Println("Enqueueing 3 tasks that will fail and retry")

	for i := 6; i <= 8; i++ {
		_, err := ergon.Enqueue(client, ctx, ProcessTaskArgs{
			ID:         i,
			Action:     "risky_operation",
			ShouldFail: true,
		},
			ergon.WithMaxRetries(3), // Allow 3 retries
		)
		if err != nil {
			log.Printf("Failed to enqueue task %d: %v", i, err)
		}
	}

	// Wait for tasks to fail and retry
	time.Sleep(5 * time.Second)

	fmt.Println("\n=== Example 3: Mixed Tasks ===")
	fmt.Println("Enqueueing mix of successful and failing tasks")

	tasks := []ProcessTaskArgs{
		{ID: 10, Action: "task_a", ShouldFail: false},
		{ID: 11, Action: "task_b", ShouldFail: true},
		{ID: 12, Action: "task_c", ShouldFail: false},
		{ID: 13, Action: "task_d", ShouldFail: true},
		{ID: 14, Action: "task_e", ShouldFail: false},
	}

	for _, taskArgs := range tasks {
		_, _ = ergon.Enqueue(client, ctx, taskArgs,
			ergon.WithMaxRetries(2),
		)
	}

	// Wait for tasks
	time.Sleep(5 * time.Second)

	// Print final metrics
	fmt.Println("\n=== Final Metrics ===")
	fmt.Printf("Tasks Started:   %d\n", metrics.started.Load())
	fmt.Printf("Tasks Completed: %d\n", metrics.completed.Load())
	fmt.Printf("Tasks Failed:    %d\n", metrics.failed.Load())
	fmt.Printf("Tasks Retried:   %d\n", metrics.retried.Load())

	fmt.Println("\n=== Event Callbacks Demo Complete ===")
	fmt.Println("Check the logs above to see callbacks in action!")
	fmt.Println("\nCallbacks can be used to:")
	fmt.Println("  • Send metrics to Datadog, Prometheus, etc.")
	fmt.Println("  • Trigger alerts for critical failures")
	fmt.Println("  • Track SLA violations")
	fmt.Println("  • Build custom dashboards")
	fmt.Println("  • Implement custom retry strategies")
}
