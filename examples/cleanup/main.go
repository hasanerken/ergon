package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// EmailTaskArgs represents an email sending task
type EmailTaskArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailTaskArgs) Kind() string { return "send_email" }

// EmailWorker processes email tasks
type EmailWorker struct{}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailTaskArgs]) error {
	fmt.Printf("📧 Sending email to: %s, subject: %s\n", task.Args.To, task.Args.Subject)
	// Simulate email sending
	time.Sleep(100 * time.Millisecond)
	return nil
}

func main() {
	fmt.Println("=== Ergon Auto-Cleanup Example ===\n")

	// Create store
	tempDir, err := os.MkdirTemp("", "ergon-cleanup-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	store, err := badger.NewStore(filepath.Join(tempDir, "queue_data"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})

	ctx := context.Background()

	// Create manager with auto-cleanup enabled
	mgr := ergon.NewManager(store, ergon.ClientConfig{
		DefaultQueue: "emails",
		Workers:      workers,
	})

	// Track cleanup events
	var cleanupCount int

	// Create server with auto-cleanup configuration
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"emails": {
				MaxWorkers:   2,
				PollInterval: 100 * time.Millisecond,
			},
		},
		// Enable automatic cleanup
		EnableAutoCleanup:  true,
		CleanupInterval:    10 * time.Second,  // Run cleanup every 10 seconds
		CleanupRetention:   30 * time.Second,  // Keep tasks for 30 seconds after completion
		CleanupStates:      []ergon.TaskState{ergon.StateCompleted, ergon.StateFailed, ergon.StateCancelled},

		// Cleanup callback for monitoring
		OnTasksCleaned: func(ctx context.Context, count int, states []ergon.TaskState) {
			cleanupCount += count
			fmt.Printf("🧹 Auto-cleanup ran: removed %d old tasks (total cleaned: %d)\n", count, cleanupCount)
		},

		// Task lifecycle callbacks for visibility
		OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
			fmt.Printf("▶️  Task started: %s\n", task.ID[:8])
		},
		OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
			fmt.Printf("✅ Task completed: %s (took %v)\n", task.ID[:8], duration)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start server
	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()

	go func() {
		if err := server.Run(serverCtx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n--- Configuration ---")
	fmt.Println("Auto-cleanup: ENABLED")
	fmt.Println("Cleanup interval: 10 seconds")
	fmt.Println("Retention period: 30 seconds")
	fmt.Println("States to clean: completed, failed, cancelled")
	fmt.Println()

	// Enqueue tasks over time
	fmt.Println("--- Enqueueing Tasks ---")
	for i := 1; i <= 5; i++ {
		task, err := ergon.Enqueue(mgr.Client(), ctx, EmailTaskArgs{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: fmt.Sprintf("Welcome Email %d", i),
			Body:    "Welcome to our platform!",
		})
		if err != nil {
			log.Printf("Failed to enqueue task: %v", err)
			continue
		}
		fmt.Printf("📤 Enqueued task %d: %s\n", i, task.ID[:8])
	}

	// Wait for tasks to complete
	fmt.Println("\n--- Processing Tasks ---")
	time.Sleep(2 * time.Second)

	// Show statistics
	stats, _ := mgr.Stats().GetOverallStats(ctx)
	fmt.Println("\n--- Statistics (before cleanup) ---")
	fmt.Printf("Total tasks: %d\n", stats.TotalTasks)
	fmt.Printf("Completed: %d\n", stats.CompletedTasks)
	fmt.Printf("Failed: %d\n", stats.FailedTasks)
	fmt.Printf("Pending: %d\n", stats.PendingTasks)

	// Show that tasks exist
	completedTasks, _ := mgr.ListTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateCompleted,
	})
	fmt.Printf("\n📋 Completed tasks in database: %d\n", len(completedTasks))

	// Wait for cleanup to run
	fmt.Println("\n--- Waiting for Auto-Cleanup ---")
	fmt.Println("Note: Cleanup runs every 10 seconds and removes tasks older than 30 seconds")
	fmt.Println("These tasks are only ~2 seconds old, so they won't be cleaned yet...")
	time.Sleep(12 * time.Second)

	// Verify tasks still exist (too new to clean)
	completedTasks, _ = mgr.ListTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateCompleted,
	})
	fmt.Printf("📋 Completed tasks after first cleanup attempt: %d (still within retention period)\n", len(completedTasks))

	// Wait for retention period to expire
	fmt.Println("\n--- Waiting for Retention Period to Expire ---")
	fmt.Println("Waiting 20 more seconds for tasks to exceed 30-second retention...")
	time.Sleep(20 * time.Second)

	// Now cleanup should remove them
	completedTasks, _ = mgr.ListTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateCompleted,
	})
	fmt.Printf("📋 Completed tasks after cleanup: %d (cleaned up!)\n", len(completedTasks))

	// Show final statistics
	stats, _ = mgr.Stats().GetOverallStats(ctx)
	fmt.Println("\n--- Final Statistics ---")
	fmt.Printf("Total tasks in database: %d\n", stats.TotalTasks)
	fmt.Printf("Completed (still in DB): %d\n", stats.CompletedTasks)
	fmt.Printf("Total tasks cleaned up: %d\n", cleanupCount)

	fmt.Println("\n--- Manual Cleanup Options ---")
	fmt.Println("You can also trigger cleanup manually:")
	fmt.Println("  mgr.Batch().PurgeCompleted(ctx, 24*time.Hour)   // Remove completed tasks older than 24h")
	fmt.Println("  mgr.Batch().PurgeFailed(ctx, 7*24*time.Hour)    // Remove failed tasks older than 7 days")
	fmt.Println("  mgr.Store().DeleteArchivedTasks(ctx, cutoff)    // Direct store access")

	fmt.Println("\n--- Cleanup Complete ---")
	fmt.Println("✨ Auto-cleanup successfully removed old tasks")
	fmt.Println("\nPress Ctrl+C to exit...")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n\n🛑 Shutting down...")
	cancelServer()
	time.Sleep(500 * time.Millisecond)
}
