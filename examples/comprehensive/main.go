package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// ============================================================================
// TASK DEFINITIONS
// ============================================================================

// EmailTask - Standard task for sending emails
type EmailTask struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailTask) Kind() string { return "email" }

// ReportTask - Scheduled task for generating reports
type ReportTask struct {
	ReportType string    `json:"report_type"`
	StartDate  time.Time `json:"start_date"`
	EndDate    time.Time `json:"end_date"`
}

func (ReportTask) Kind() string { return "report" }

// NotificationTask - Delayed task for sending notifications
type NotificationTask struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (NotificationTask) Kind() string { return "notification" }

// HealthCheckTask - Recurring task (every hour)
type HealthCheckTask struct {
	ServiceName string `json:"service_name"`
	Endpoint    string `json:"endpoint"`
}

func (HealthCheckTask) Kind() string { return "health_check" }

// DataSyncTask - Task with retry logic
type DataSyncTask struct {
	SourceID      string `json:"source_id"`
	DestinationID string `json:"destination_id"`
}

func (DataSyncTask) Kind() string { return "data_sync" }

// BatchProcessTask - Batch processing task
type BatchProcessTask struct {
	BatchID  string   `json:"batch_id"`
	ItemIDs  []string `json:"item_ids"`
	Priority int      `json:"priority"`
}

func (BatchProcessTask) Kind() string { return "batch_process" }

// AlwaysFailTask - Task that always fails (for testing retry exhaustion)
type AlwaysFailTask struct {
	Reason string `json:"reason"`
}

func (AlwaysFailTask) Kind() string { return "always_fail" }

// ============================================================================
// WORKERS
// ============================================================================

// EmailWorker sends emails
type EmailWorker struct {
	ergon.WorkerDefaults[EmailTask]
}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailTask]) error {
	log.Printf("📧 [EMAIL] Sending to: %s | Subject: %s", task.Args.To, task.Args.Subject)
	time.Sleep(500 * time.Millisecond) // Simulate work
	log.Printf("✅ [EMAIL] Sent successfully to %s", task.Args.To)
	return nil
}

func (w *EmailWorker) Timeout(task *ergon.Task[EmailTask]) time.Duration {
	return 30 * time.Second
}

// ReportWorker generates reports
type ReportWorker struct {
	ergon.WorkerDefaults[ReportTask]
}

func (w *ReportWorker) Work(ctx context.Context, task *ergon.Task[ReportTask]) error {
	log.Printf("📊 [REPORT] Generating %s report from %s to %s",
		task.Args.ReportType,
		task.Args.StartDate.Format("2006-01-02"),
		task.Args.EndDate.Format("2006-01-02"))
	time.Sleep(1 * time.Second) // Simulate report generation
	log.Printf("✅ [REPORT] %s report completed", task.Args.ReportType)
	return nil
}

// NotificationWorker sends notifications
type NotificationWorker struct {
	ergon.WorkerDefaults[NotificationTask]
}

func (w *NotificationWorker) Work(ctx context.Context, task *ergon.Task[NotificationTask]) error {
	log.Printf("🔔 [NOTIFICATION] Sending %s notification to user %s: %s",
		task.Args.Type, task.Args.UserID, task.Args.Message)
	time.Sleep(300 * time.Millisecond)
	log.Printf("✅ [NOTIFICATION] Sent to user %s", task.Args.UserID)
	return nil
}

// HealthCheckWorker performs health checks
type HealthCheckWorker struct {
	ergon.WorkerDefaults[HealthCheckTask]
}

func (w *HealthCheckWorker) Work(ctx context.Context, task *ergon.Task[HealthCheckTask]) error {
	log.Printf("🏥 [HEALTH] Checking %s at %s", task.Args.ServiceName, task.Args.Endpoint)
	time.Sleep(200 * time.Millisecond) // Simulate health check
	log.Printf("✅ [HEALTH] %s is healthy", task.Args.ServiceName)
	return nil
}

// DataSyncWorker syncs data with retry logic
type DataSyncWorker struct {
	ergon.WorkerDefaults[DataSyncTask]
	attemptCount int
}

func (w *DataSyncWorker) Work(ctx context.Context, task *ergon.Task[DataSyncTask]) error {
	w.attemptCount++
	log.Printf("🔄 [SYNC] Attempt %d - Syncing from %s to %s",
		task.Retried+1, task.Args.SourceID, task.Args.DestinationID)

	// Simulate failure for first 2 attempts
	if task.Retried < 2 {
		log.Printf("❌ [SYNC] Failed (will retry): network timeout")
		return fmt.Errorf("network timeout on attempt %d", task.Retried+1)
	}

	time.Sleep(400 * time.Millisecond)
	log.Printf("✅ [SYNC] Successfully synced data")
	return nil
}

func (w *DataSyncWorker) MaxRetries(task *ergon.Task[DataSyncTask]) int {
	return 5
}

func (w *DataSyncWorker) RetryDelay(task *ergon.Task[DataSyncTask], attempt int, err error) time.Duration {
	// Exponential backoff: 2s, 4s, 8s, 16s, 32s
	return time.Duration(1<<uint(attempt)) * time.Second
}

// BatchProcessWorker processes batches
type BatchProcessWorker struct {
	ergon.WorkerDefaults[BatchProcessTask]
}

func (w *BatchProcessWorker) Work(ctx context.Context, task *ergon.Task[BatchProcessTask]) error {
	log.Printf("📦 [BATCH] Processing batch %s with %d items (priority: %d)",
		task.Args.BatchID, len(task.Args.ItemIDs), task.Args.Priority)
	time.Sleep(600 * time.Millisecond) // Simulate batch processing
	log.Printf("✅ [BATCH] Completed batch %s", task.Args.BatchID)
	return nil
}

// ============================================================================
// MAIN APPLICATION
// ============================================================================

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Println("🚀 Ergon Comprehensive Example - Starting...")

	// ========================================================================
	// 1. SETUP BADGER STORE (Persistent, separate from your app's DB)
	// ========================================================================
	log.Println("\n📦 Setting up Badger store...")

	store, err := badger.NewStore("./ergon-tasks-db")
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	// Clean up after demo (remove this in production)
	defer os.RemoveAll("./ergon-tasks-db")

	log.Println("✅ Badger store ready at ./ergon-tasks-db")

	// ========================================================================
	// 2. REGISTER WORKERS
	// ========================================================================
	log.Println("\n👷 Registering workers...")

	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &ReportWorker{})
	ergon.AddWorker(workers, &NotificationWorker{})
	ergon.AddWorker(workers, &HealthCheckWorker{})
	ergon.AddWorker(workers, &DataSyncWorker{})
	ergon.AddWorker(workers, &BatchProcessWorker{})

	log.Printf("✅ Registered %d workers", 6)

	// ========================================================================
	// 3. CREATE CLIENT (for enqueueing tasks)
	// ========================================================================
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue:   "default",
		DefaultRetries: 3,
		DefaultTimeout: 5 * time.Minute,
		Workers:        workers, // Validates task types at enqueue time
	})

	// ========================================================================
	// 4. CREATE MANAGER (unified API for inspection, control, stats)
	// ========================================================================
	manager := ergon.NewManager(store, ergon.ClientConfig{Workers: workers})

	// ========================================================================
	// 5. CREATE SERVER (processes tasks)
	// ========================================================================
	log.Println("\n🔧 Configuring server...")

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: 10,
		Workers:     workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   5,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
			"high_priority": {
				MaxWorkers:   3,
				Priority:     10,
				PollInterval: 200 * time.Millisecond,
			},
			"low_priority": {
				MaxWorkers:   2,
				Priority:     0,
				PollInterval: 1 * time.Second,
			},
		},
		Middleware: []ergon.MiddlewareFunc{
			ergon.LoggingMiddleware(),
			ergon.RecoveryMiddleware(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Println("✅ Server configured with 3 queues and middleware")

	// ========================================================================
	// 6. START SERVER
	// ========================================================================
	log.Println("\n▶️  Starting server...")

	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond) // Let server start
	log.Println("✅ Server started and processing tasks")

	// ========================================================================
	// 7. DEMONSTRATE ALL FEATURES
	// ========================================================================

	log.Println("\n" + strings.Repeat("=", 70))
	log.Println("🎯 DEMONSTRATING ALL ERGON FEATURES")
	log.Println(strings.Repeat("=", 70))

	// FEATURE 1: Standard Task
	log.Println("\n1️⃣  Standard Task - Immediate execution")
	task1, _ := ergon.Enqueue(client, ctx, EmailTask{
		To:      "user@example.com",
		Subject: "Welcome to Ergon!",
		Body:    "This is a standard task example",
	})
	log.Printf("   Enqueued task: %s", task1.ID)

	time.Sleep(1 * time.Second)

	// FEATURE 2: Delayed Task
	log.Println("\n2️⃣  Delayed Task - Executes after 3 seconds")
	task2, _ := ergon.Enqueue(client, ctx, NotificationTask{
		UserID:  "user-123",
		Message: "Your delayed notification",
		Type:    "reminder",
	}, ergon.WithDelay(3*time.Second))
	log.Printf("   Enqueued delayed task: %s (executes at %s)",
		task2.ID, time.Now().Add(3*time.Second).Format("15:04:05"))

	// FEATURE 3: Scheduled Task
	log.Println("\n3️⃣  Scheduled Task - Executes at specific time")
	scheduleTime := time.Now().Add(5 * time.Second)
	task3, _ := ergon.Enqueue(client, ctx, ReportTask{
		ReportType: "daily_sales",
		StartDate:  time.Now().AddDate(0, 0, -1),
		EndDate:    time.Now(),
	}, ergon.WithScheduledAt(scheduleTime))
	log.Printf("   Scheduled report task: %s (at %s)", task3.ID, scheduleTime.Format("15:04:05"))

	// FEATURE 4: Recurring Task (Every Hour simulation - using short interval for demo)
	log.Println("\n4️⃣  Recurring Task - Every 10 seconds (simulating hourly)")
	task4, _ := ergon.Enqueue(client, ctx, HealthCheckTask{
		ServiceName: "API Server",
		Endpoint:    "http://localhost:8080/health",
	}, ergon.WithInterval(10*time.Second))
	log.Printf("   Recurring health check: %s (repeats every 10s)", task4.ID)

	// FEATURE 5: Task with Retry Logic
	log.Println("\n5️⃣  Task with Retry - Will fail twice, succeed on 3rd attempt")
	task5, _ := ergon.Enqueue(client, ctx, DataSyncTask{
		SourceID:      "db-primary",
		DestinationID: "db-replica",
	}, ergon.WithMaxRetries(5))
	log.Printf("   Sync task with retry: %s", task5.ID)

	// FEATURE 6: Priority Tasks
	log.Println("\n6️⃣  Priority Tasks - High priority executed first")
	task6a, _ := ergon.Enqueue(client, ctx, BatchProcessTask{
		BatchID:  "batch-low",
		ItemIDs:  []string{"item1", "item2"},
		Priority: 1,
	}, ergon.WithQueue("low_priority"), ergon.WithPriority(1))

	task6b, _ := ergon.Enqueue(client, ctx, BatchProcessTask{
		BatchID:  "batch-high",
		ItemIDs:  []string{"item3", "item4"},
		Priority: 10,
	}, ergon.WithQueue("high_priority"), ergon.WithPriority(10))
	log.Printf("   Low priority: %s | High priority: %s", task6a.ID, task6b.ID)

	// FEATURE 7: Batch Enqueue
	log.Println("\n7️⃣  Batch Enqueue - Multiple tasks at once")
	emails := []EmailTask{
		{To: "user1@example.com", Subject: "Newsletter", Body: "News 1"},
		{To: "user2@example.com", Subject: "Newsletter", Body: "News 2"},
		{To: "user3@example.com", Subject: "Newsletter", Body: "News 3"},
	}
	tasks, _ := ergon.EnqueueMany(client, ctx, emails)
	log.Printf("   Enqueued %d newsletter emails", len(tasks))

	// FEATURE 8: Unique Tasks (at most once per hour)
	log.Println("\n8️⃣  Unique Task - Prevents duplicates")
	task8, _ := ergon.Enqueue(client, ctx, DataSyncTask{
		SourceID:      "unique-source",
		DestinationID: "unique-dest",
	}, ergon.AtMostOncePerHour())
	log.Printf("   Unique task: %s", task8.ID)

	// Try duplicate (should fail)
	_, err = ergon.Enqueue(client, ctx, DataSyncTask{
		SourceID:      "unique-source",
		DestinationID: "unique-dest",
	}, ergon.AtMostOncePerHour())
	if err != nil {
		log.Printf("   ✓ Duplicate rejected: %v", err)
	}

	// FEATURE 9: Task with Metadata
	log.Println("\n9️⃣  Task with Metadata - Custom tracking data")
	task9, _ := ergon.Enqueue(client, ctx, EmailTask{
		To:      "admin@example.com",
		Subject: "System Alert",
		Body:    "Critical notification",
	},
		ergon.WithMetadata("user_id", 12345),
		ergon.WithMetadata("request_id", "req-abc-123"),
		ergon.WithMetadata("source", "monitoring"),
	)
	log.Printf("   Task with metadata: %s", task9.ID)

	// Wait for initial tasks to process
	time.Sleep(7 * time.Second)

	// ========================================================================
	// 8. TASK INSPECTION & STATISTICS
	// ========================================================================
	log.Println("\n" + strings.Repeat("=", 70))
	log.Println("📊 TASK INSPECTION & STATISTICS")
	log.Println(strings.Repeat("=", 70))

	// Overall statistics
	stats, _ := manager.Stats().GetOverallStats(ctx)
	log.Printf("\n📈 Overall Stats:")
	log.Printf("   Total Tasks: %d", stats.TotalTasks)
	log.Printf("   Completed: %d", stats.CompletedTasks)
	log.Printf("   Failed: %d", stats.FailedTasks)
	log.Printf("   Pending: %d", stats.PendingTasks)

	// Queue statistics
	queueStats, _ := manager.Stats().GetQueueStats(ctx, "default")
	log.Printf("\n📋 Default Queue Stats:")
	log.Printf("   Pending: %d", queueStats.PendingCount)
	log.Printf("   Running: %d", queueStats.RunningCount)
	log.Printf("   Completed: %d", queueStats.CompletedCount)

	// Task details
	taskDetails, _ := manager.Tasks().GetTaskDetails(ctx, task1.ID)
	log.Printf("\n🔍 Task Details (ID: %s):", task1.ID)
	log.Printf("   State: %s", taskDetails.Task.State)
	log.Printf("   Kind: %s", taskDetails.Task.Kind)
	log.Printf("   Retried: %d times", taskDetails.Task.Retried)

	// ========================================================================
	// 9. TASK CONTROL OPERATIONS
	// ========================================================================
	log.Println("\n" + strings.Repeat("=", 70))
	log.Println("🎮 TASK CONTROL OPERATIONS")
	log.Println(strings.Repeat("=", 70))

	// A. Cancel a PENDING (not yet executed) task
	log.Println("\n🔴 Cancelling Pending Task")
	pendingCancelTask, _ := ergon.Enqueue(client, ctx, EmailTask{
		To:      "pending-cancel@example.com",
		Subject: "Will be cancelled before execution",
		Body:    "This pending task will be cancelled",
	}, ergon.WithDelay(30*time.Second)) // Won't execute for 30 seconds

	log.Printf("   Enqueued task with 30s delay: %s", pendingCancelTask.ID)

	// Cancel it immediately (before it executes)
	err = manager.Control().Cancel(ctx, pendingCancelTask.ID)
	if err == nil {
		log.Printf("   ✅ Cancelled pending task: %s", pendingCancelTask.ID)

		// Verify it's cancelled
		details, _ := manager.Tasks().GetTaskDetails(ctx, pendingCancelTask.ID)
		log.Printf("   📊 Task state: %s (never executed)", details.Task.State)
	} else {
		log.Printf("   ❌ Failed to cancel: %v", err)
	}

	// B. Create a task that will FAIL all retries
	log.Println("\n💥 Creating Task That Will Fail All Retries")

	// Register worker that always fails
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[AlwaysFailTask]) error {
		log.Printf("❌ [FAIL] Attempt %d - Task failing: %s", task.Retried+1, task.Args.Reason)
		return fmt.Errorf("permanent failure: %s", task.Args.Reason)
	})

	failedTask, _ := ergon.Enqueue(client, ctx, AlwaysFailTask{
		Reason: "Server is down",
	}, ergon.WithMaxRetries(2)) // Only 2 retries, will fail completely

	log.Printf("   Enqueued task that will fail: %s", failedTask.ID)
	log.Println("   ⏳ Waiting for it to fail all retries...")

	// Wait for all retries to exhaust (attempt 1 + 2s + attempt 2 + 4s + attempt 3)
	time.Sleep(10 * time.Second)

	// Check if it's failed
	failedDetails, _ := manager.Tasks().GetTaskDetails(ctx, failedTask.ID)
	log.Printf("   📊 Task state after retries: %s", failedDetails.Task.State)
	log.Printf("   📊 Retry count: %d", failedDetails.Task.Retried)

	// If still retrying, wait for final attempt
	if failedDetails.Task.State == ergon.StateRetrying {
		log.Println("   ⏳ Waiting for final retry attempt...")
		time.Sleep(5 * time.Second)
		failedDetails, _ = manager.Tasks().GetTaskDetails(ctx, failedTask.ID)
		log.Printf("   📊 Final state: %s", failedDetails.Task.State)
	}

	// C. Manual Retry - Retry the failed task manually
	log.Println("\n🔄 Manual Retry After Complete Failure")
	log.Println("   💡 Scenario: Server is back up, manually retrying failed task")

	err = manager.Control().RetryNow(ctx, failedTask.ID)
	if err == nil {
		log.Printf("   ✅ Manually triggered retry for task: %s", failedTask.ID)
		log.Println("   ⏳ Task will be retried immediately...")
		time.Sleep(3 * time.Second)

		// Check status again
		retryDetails, _ := manager.Tasks().GetTaskDetails(ctx, failedTask.ID)
		log.Printf("   📊 Task state after manual retry: %s", retryDetails.Task.State)
	} else {
		log.Printf("   ❌ Failed to manually retry: %v", err)
	}

	// D. Delete completed tasks
	log.Println("\n🗑️  Deleting Completed Task")
	deleteTask, _ := ergon.Enqueue(client, ctx, EmailTask{
		To: "delete@example.com", Subject: "Will be deleted", Body: "Temp task",
	})
	time.Sleep(2 * time.Second)
	err = manager.Control().Delete(ctx, deleteTask.ID)
	if err == nil {
		log.Printf("   ✅ Deleted task: %s", deleteTask.ID)
	}

	// ========================================================================
	// 10. GRACEFUL SHUTDOWN
	// ========================================================================
	log.Println("\n" + strings.Repeat("=", 70))
	log.Println("⏳ Running for 15 seconds... Press Ctrl+C to stop early")
	log.Println(strings.Repeat("=", 70))

	// Wait for shutdown signal or timeout
	select {
	case <-sigChan:
		log.Println("\n🛑 Received shutdown signal...")
	case <-time.After(15 * time.Second):
		log.Println("\n⏰ Demo timeout reached...")
	}

	log.Println("🛑 Shutting down server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("❌ Shutdown error: %v", err)
	} else {
		log.Println("✅ Server stopped gracefully")
	}

	// Final statistics
	finalStats, _ := manager.Stats().GetOverallStats(ctx)
	log.Println("\n" + strings.Repeat("=", 70))
	log.Println("📊 FINAL STATISTICS")
	log.Println(strings.Repeat("=", 70))
	log.Printf("Total Tasks Processed: %d", finalStats.TotalTasks)
	log.Printf("Successful: %d", finalStats.CompletedTasks)
	log.Printf("Failed: %d", finalStats.FailedTasks)

	log.Println("\n✨ Comprehensive Example Completed!")
	log.Println("💾 Database was stored at: ./ergon-tasks-db (cleaned up for demo)")
}
