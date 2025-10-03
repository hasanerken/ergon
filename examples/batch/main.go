package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// Task types
type EmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailArgs) Kind() string { return "send_email" }

type NotificationArgs struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

func (NotificationArgs) Kind() string { return "send_notification" }

type ReportArgs struct {
	ReportType string `json:"report_type"`
	UserID     string `json:"user_id"`
}

func (ReportArgs) Kind() string { return "generate_report" }

// Workers
type EmailWorker struct {
	ergon.WorkerDefaults[EmailArgs]
}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailArgs]) error {
	time.Sleep(100 * time.Millisecond)

	// Simulate some failures
	if rand.Float32() < 0.2 {
		return fmt.Errorf("failed to send email")
	}

	return nil
}

type NotificationWorker struct {
	ergon.WorkerDefaults[NotificationArgs]
}

func (w *NotificationWorker) Work(ctx context.Context, task *ergon.Task[NotificationArgs]) error {
	time.Sleep(50 * time.Millisecond)
	return nil
}

type ReportWorker struct {
	ergon.WorkerDefaults[ReportArgs]
}

func (w *ReportWorker) Work(ctx context.Context, task *ergon.Task[ReportArgs]) error {
	time.Sleep(200 * time.Millisecond)

	// Simulate occasional failures
	if rand.Float32() < 0.15 {
		return fmt.Errorf("report generation failed")
	}

	return nil
}

func main() {
	ctx := context.Background()
	dbPath := "./batch_test"

	// Clean up
	os.RemoveAll(dbPath)
	defer os.RemoveAll(dbPath)

	// Create store
	store, err := badger.NewStore(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &NotificationWorker{})
	ergon.AddWorker(workers, &ReportWorker{})

	// Create manager
	mgr := ergon.NewManager(store, ergon.ClientConfig{
		Workers: workers,
	})
	defer mgr.Close()

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"emails": {
				MaxWorkers:   3,
				PollInterval: 100 * time.Millisecond,
			},
			"notifications": {
				MaxWorkers:   2,
				PollInterval: 100 * time.Millisecond,
			},
			"reports": {
				MaxWorkers:   2,
				PollInterval: 100 * time.Millisecond,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start server
	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()
	defer server.Stop(ctx)

	fmt.Println("🚀 Batch Operations Demo")
	fmt.Println("=" + string(make([]byte, 60)))
	fmt.Println()

	// Demo 1: Bulk Enqueue and Cancel
	fmt.Println("📝 Demo 1: Bulk Cancel Operations")
	demoBulkCancel(ctx, mgr)

	// Demo 2: Retry Failed Tasks
	fmt.Println("\n🔄 Demo 2: Retry Failed Tasks")
	demoRetryFailed(ctx, mgr)

	// Demo 3: Delete by Filter
	fmt.Println("\n🗑️  Demo 3: Delete by Filter")
	demoDeleteByFilter(ctx, mgr)

	// Demo 4: Purge Old Tasks
	fmt.Println("\n🧹 Demo 4: Purge Old Tasks")
	demoPurgeOld(ctx, mgr)

	// Demo 5: Update Priority in Bulk
	fmt.Println("\n⬆️  Demo 5: Update Priority in Bulk")
	demoUpdatePriority(ctx, mgr)

	// Demo 6: Reschedule Tasks
	fmt.Println("\n📅 Demo 6: Reschedule Tasks in Bulk")
	demoReschedule(ctx, mgr)

	// Demo 7: Queue Management
	fmt.Println("\n⏸️  Demo 7: Queue Management (Pause/Resume)")
	demoQueueManagement(ctx, mgr)

	// Demo 8: Cleanup Operations
	fmt.Println("\n🧼 Demo 8: Cleanup Operations")
	demoCleanup(ctx, mgr)

	fmt.Println("\n✅ Batch Operations Demos Complete!")
}

func demoBulkCancel(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue tasks
	var taskIDs []string
	for i := 0; i < 20; i++ {
		task, err := ergon.Enqueue(mgr.Client(), ctx, EmailArgs{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: "Test Email",
			Body:    "This is a test",
		}, ergon.WithQueue("emails"))
		if err != nil {
			log.Printf("Failed to enqueue: %v", err)
			continue
		}
		taskIDs = append(taskIDs, task.ID)
	}

	fmt.Printf("  ✓ Enqueued %d email tasks\n", len(taskIDs))
	time.Sleep(100 * time.Millisecond)

	// Cancel half of them
	cancelCount, err := mgr.Batch().CancelMany(ctx, taskIDs[:10])
	if err != nil {
		log.Printf("Failed to cancel tasks: %v", err)
	}

	fmt.Printf("  ✓ Cancelled %d tasks using CancelMany\n", cancelCount)

	// Cancel remaining by filter
	filter := ergon.TaskFilter{
		Queue: "emails",
		State: ergon.StatePending,
	}

	canceledByFilter, err := mgr.Batch().CancelByFilter(ctx, filter)
	if err != nil {
		log.Printf("Failed to cancel by filter: %v", err)
	}

	fmt.Printf("  ✓ Cancelled %d more tasks using CancelByFilter\n", canceledByFilter)

	// Show final counts
	counts, _ := mgr.Tasks().CountByState(ctx, "emails")
	fmt.Printf("  📊 Final state - Cancelled: %d, Completed: %d\n",
		counts[ergon.StateCancelled], counts[ergon.StateCompleted])
}

func demoRetryFailed(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue tasks (some will fail)
	for i := 0; i < 30; i++ {
		ergon.Enqueue(mgr.Client(), ctx, EmailArgs{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: "Retry Test",
			Body:    "This might fail",
		}, ergon.WithQueue("emails"), ergon.WithMaxRetries(3))
	}

	fmt.Println("  ✓ Enqueued 30 email tasks (some will fail)")
	time.Sleep(3 * time.Second) // Let tasks process

	// Count failed tasks
	counts, _ := mgr.Tasks().CountByState(ctx, "emails")
	fmt.Printf("  📊 Before retry - Failed: %d, Completed: %d\n",
		counts[ergon.StateFailed], counts[ergon.StateCompleted])

	// Retry all failed email tasks
	retriedCount, err := mgr.Batch().RetryAllFailed(ctx, "send_email")
	if err != nil {
		log.Printf("Failed to retry: %v", err)
	}

	fmt.Printf("  ✓ Retried %d failed tasks\n", retriedCount)
	time.Sleep(2 * time.Second) // Let retries process

	// Show results
	counts, _ = mgr.Tasks().CountByState(ctx, "emails")
	fmt.Printf("  📊 After retry - Failed: %d, Completed: %d, Retrying: %d\n",
		counts[ergon.StateFailed], counts[ergon.StateCompleted], counts[ergon.StateRetrying])
}

func demoDeleteByFilter(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue completed tasks (simulate)
	for i := 0; i < 15; i++ {
		ergon.Enqueue(mgr.Client(), ctx, NotificationArgs{
			UserID:  fmt.Sprintf("user-%d", i),
			Message: "Test notification",
		}, ergon.WithQueue("notifications"))
	}

	fmt.Println("  ✓ Enqueued 15 notification tasks")
	time.Sleep(2 * time.Second) // Let them complete

	// Count before deletion
	beforeCount, _ := mgr.Tasks().CountByState(ctx, "notifications")
	fmt.Printf("  📊 Before deletion - Completed: %d\n", beforeCount[ergon.StateCompleted])

	// Delete all completed notifications
	filter := ergon.TaskFilter{
		Queue: "notifications",
		State: ergon.StateCompleted,
	}

	deletedCount, err := mgr.Batch().DeleteByFilter(ctx, filter)
	if err != nil {
		log.Printf("Failed to delete: %v", err)
	}

	fmt.Printf("  ✓ Deleted %d completed tasks using DeleteByFilter\n", deletedCount)

	// Show results
	afterCount, _ := mgr.Tasks().CountByState(ctx, "notifications")
	fmt.Printf("  📊 After deletion - Completed: %d\n", afterCount[ergon.StateCompleted])
}

func demoPurgeOld(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue and complete some tasks
	for i := 0; i < 20; i++ {
		ergon.Enqueue(mgr.Client(), ctx, NotificationArgs{
			UserID:  fmt.Sprintf("user-%d", i),
			Message: "Old notification",
		}, ergon.WithQueue("notifications"))
	}

	fmt.Println("  ✓ Enqueued 20 notification tasks")
	time.Sleep(2 * time.Second) // Let them complete

	// Count before purge
	before, _ := mgr.Tasks().CountByState(ctx, "notifications")
	fmt.Printf("  📊 Before purge - Completed: %d\n", before[ergon.StateCompleted])

	// Purge tasks older than 100ms (should get most of them)
	purgedCount, err := mgr.Batch().PurgeCompleted(ctx, 100*time.Millisecond)
	if err != nil {
		log.Printf("Failed to purge: %v", err)
	}

	fmt.Printf("  ✓ Purged %d completed tasks older than 100ms\n", purgedCount)

	// Show results
	after, _ := mgr.Tasks().CountByState(ctx, "notifications")
	fmt.Printf("  📊 After purge - Completed: %d\n", after[ergon.StateCompleted])
}

func demoUpdatePriority(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue low priority tasks
	for i := 0; i < 15; i++ {
		ergon.Enqueue(mgr.Client(), ctx, ReportArgs{
			ReportType: "sales",
			UserID:     fmt.Sprintf("user-%d", i),
		},
			ergon.WithQueue("reports"),
			ergon.WithPriority(1),
		)
	}

	fmt.Println("  ✓ Enqueued 15 low-priority report tasks (priority=1)")

	// Show some task details
	taskList, _ := mgr.Tasks().ListTasks(ctx, ergon.ListOptions{
		Queue: "reports",
		State: ergon.StatePending,
		Limit: 3,
	})

	if taskList != nil && len(taskList.Tasks) > 0 {
		fmt.Printf("  📋 Sample task priority before: %d\n", taskList.Tasks[0].Task.Priority)
	}

	// Boost priority for all pending reports
	filter := ergon.TaskFilter{
		Queue: "reports",
		State: ergon.StatePending,
	}

	updatedCount, err := mgr.Batch().UpdatePriorityByFilter(ctx, filter, 10)
	if err != nil {
		log.Printf("Failed to update priority: %v", err)
	}

	fmt.Printf("  ✓ Updated priority to 10 for %d tasks\n", updatedCount)

	// Show updated priority
	taskList, _ = mgr.Tasks().ListTasks(ctx, ergon.ListOptions{
		Queue: "reports",
		State: ergon.StatePending,
		Limit: 3,
	})

	if taskList != nil && len(taskList.Tasks) > 0 {
		fmt.Printf("  📋 Sample task priority after: %d\n", taskList.Tasks[0].Task.Priority)
	}
}

func demoReschedule(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue scheduled tasks
	futureTime := time.Now().Add(1 * time.Hour)

	for i := 0; i < 10; i++ {
		ergon.Enqueue(mgr.Client(), ctx, EmailArgs{
			To:      fmt.Sprintf("scheduled%d@example.com", i),
			Subject: "Scheduled Email",
			Body:    "This was scheduled",
		},
			ergon.WithQueue("emails"),
			ergon.WithScheduledAt(futureTime),
		)
	}

	fmt.Printf("  ✓ Enqueued 10 tasks scheduled for %s\n",
		futureTime.Format("15:04:05"))

	// Count scheduled tasks
	counts, _ := mgr.Tasks().CountByState(ctx, "emails")
	fmt.Printf("  📊 Scheduled tasks: %d\n", counts[ergon.StateScheduled])

	// Reschedule all to run now
	filter := ergon.TaskFilter{
		Queue: "emails",
		State: ergon.StateScheduled,
	}

	rescheduledCount, err := mgr.Batch().RescheduleByFilter(ctx, filter, time.Now())
	if err != nil {
		log.Printf("Failed to reschedule: %v", err)
	}

	fmt.Printf("  ✓ Rescheduled %d tasks to run immediately\n", rescheduledCount)

	// Show results
	time.Sleep(500 * time.Millisecond)
	counts, _ = mgr.Tasks().CountByState(ctx, "emails")
	fmt.Printf("  📊 After reschedule - Scheduled: %d, Pending: %d\n",
		counts[ergon.StateScheduled], counts[ergon.StatePending])
}

func demoQueueManagement(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue tasks to multiple queues
	for i := 0; i < 5; i++ {
		ergon.Enqueue(mgr.Client(), ctx, EmailArgs{
			To:      fmt.Sprintf("test%d@example.com", i),
			Subject: "Test",
			Body:    "Test",
		}, ergon.WithQueue("emails"))

		ergon.Enqueue(mgr.Client(), ctx, NotificationArgs{
			UserID:  fmt.Sprintf("user-%d", i),
			Message: "Test",
		}, ergon.WithQueue("notifications"))
	}

	fmt.Println("  ✓ Enqueued 5 tasks to emails queue")
	fmt.Println("  ✓ Enqueued 5 tasks to notifications queue")

	// Pause both queues
	err := mgr.Batch().PauseQueues(ctx, []string{"emails", "notifications"})
	if err != nil {
		log.Printf("Failed to pause queues: %v", err)
	}

	fmt.Println("  ⏸️  Paused emails and notifications queues")
	time.Sleep(500 * time.Millisecond)

	// Check queue status
	emailInfo, _ := mgr.Store().GetQueueInfo(ctx, "emails")
	notifInfo, _ := mgr.Store().GetQueueInfo(ctx, "notifications")

	fmt.Printf("  📊 Emails queue paused: %v\n", emailInfo.Paused)
	fmt.Printf("  📊 Notifications queue paused: %v\n", notifInfo.Paused)

	// Resume queues
	err = mgr.Batch().ResumeQueues(ctx, []string{"emails", "notifications"})
	if err != nil {
		log.Printf("Failed to resume queues: %v", err)
	}

	fmt.Println("  ▶️  Resumed emails and notifications queues")

	// Check queue status again
	emailInfo, _ = mgr.Store().GetQueueInfo(ctx, "emails")
	notifInfo, _ = mgr.Store().GetQueueInfo(ctx, "notifications")

	fmt.Printf("  📊 Emails queue paused: %v\n", emailInfo.Paused)
	fmt.Printf("  📊 Notifications queue paused: %v\n", notifInfo.Paused)
}

func demoCleanup(ctx context.Context, mgr *ergon.Manager) {
	// Create batch operations helper
	batchOps := ergon.NewBatchOperations(mgr.Batch())

	// Enqueue and process tasks
	for i := 0; i < 25; i++ {
		ergon.Enqueue(mgr.Client(), ctx, NotificationArgs{
			UserID:  fmt.Sprintf("user-%d", i),
			Message: "Cleanup test",
		}, ergon.WithQueue("notifications"))
	}

	fmt.Println("  ✓ Enqueued 25 notification tasks")
	time.Sleep(3 * time.Second) // Let them complete

	// Show counts before cleanup
	before, _ := mgr.Tasks().CountByState(ctx, "notifications")
	total := before[ergon.StateCompleted] + before[ergon.StateFailed]
	fmt.Printf("  📊 Before cleanup - Total finished: %d (Completed: %d, Failed: %d)\n",
		total, before[ergon.StateCompleted], before[ergon.StateFailed])

	// Cleanup old tasks (completed and failed)
	cleanedCount, err := batchOps.CleanupOldTasks(ctx, 100*time.Millisecond)
	if err != nil {
		log.Printf("Failed to cleanup: %v", err)
	}

	fmt.Printf("  ✓ Cleaned up %d old tasks\n", cleanedCount)

	// Show counts after cleanup
	after, _ := mgr.Tasks().CountByState(ctx, "notifications")
	total = after[ergon.StateCompleted] + after[ergon.StateFailed]
	fmt.Printf("  📊 After cleanup - Total finished: %d (Completed: %d, Failed: %d)\n",
		total, after[ergon.StateCompleted], after[ergon.StateFailed])

	// Additional convenience operations
	fmt.Println("\n  🔧 Additional convenience operations available:")
	fmt.Println("    - CancelAllInQueue()")
	fmt.Println("    - DeleteAllInQueue()")
	fmt.Println("    - RetryAllFailedInQueue()")
	fmt.Println("    - BoostPriorityForKind()")
	fmt.Println("    - RescheduleAllInQueue()")
}
