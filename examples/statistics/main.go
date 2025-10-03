package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// ProcessOrderArgs represents order processing task
type ProcessOrderArgs struct {
	OrderID    string  `json:"order_id"`
	Amount     float64 `json:"amount"`
	CustomerID string  `json:"customer_id"`
}

func (ProcessOrderArgs) Kind() string { return "process_order" }

// SendEmailArgs represents email sending task
type SendEmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

// GenerateReportArgs represents report generation task
type GenerateReportArgs struct {
	ReportType string `json:"report_type"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
}

func (GenerateReportArgs) Kind() string { return "generate_report" }

// Workers
type OrderWorker struct {
	ergon.WorkerDefaults[ProcessOrderArgs]
}

func (w *OrderWorker) Work(ctx context.Context, task *ergon.Task[ProcessOrderArgs]) error {
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

	// 5% failure rate
	if rand.Float32() < 0.05 {
		return fmt.Errorf("payment gateway timeout")
	}

	return nil
}

type EmailWorker struct {
	ergon.WorkerDefaults[SendEmailArgs]
}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[SendEmailArgs]) error {
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// 10% failure rate
	if rand.Float32() < 0.10 {
		if rand.Float32() < 0.5 {
			return fmt.Errorf("SMTP connection failed")
		}
		return fmt.Errorf("invalid email address")
	}

	return nil
}

type ReportWorker struct {
	ergon.WorkerDefaults[GenerateReportArgs]
}

func (w *ReportWorker) Work(ctx context.Context, task *ergon.Task[GenerateReportArgs]) error {
	time.Sleep(time.Duration(500+rand.Intn(1500)) * time.Millisecond)

	// 15% failure rate
	if rand.Float32() < 0.15 {
		if rand.Float32() < 0.3 {
			return fmt.Errorf("database timeout")
		} else if rand.Float32() < 0.6 {
			return fmt.Errorf("out of memory")
		}
		return fmt.Errorf("report template not found")
	}

	return nil
}

func main() {
	ctx := context.Background()

	// Setup Badger store
	store, err := badger.NewStore("/tmp/zeus-stats-demo")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &OrderWorker{})
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &ReportWorker{})

	// Create manager
	mgr := ergon.NewManager(store, ergon.ClientConfig{
		Workers: workers,
	})
	defer mgr.Close()

	// Create server with multiple queues
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers: workers,
		Queues: map[string]ergon.QueueConfig{
			"orders": {
				MaxWorkers:   5,
				PollInterval: 100 * time.Millisecond,
			},
			"emails": {
				MaxWorkers:   3,
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

	fmt.Println("🚀 Task Statistics Demo")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	// Demo 1: Enqueue sample tasks
	fmt.Println("📝 Demo 1: Enqueueing sample tasks...")
	enqueueSampleTasks(ctx, mgr)
	time.Sleep(5 * time.Second) // Let some tasks complete

	// Demo 2: Queue Statistics
	fmt.Println("\n📊 Demo 2: Queue Statistics")
	showQueueStats(ctx, mgr)

	// Demo 3: Task Kind Statistics
	fmt.Println("\n🔍 Demo 3: Task Kind Statistics")
	showKindStats(ctx, mgr)

	// Demo 4: Overall Statistics
	fmt.Println("\n🌍 Demo 4: Overall Statistics")
	showOverallStats(ctx, mgr)

	// Demo 5: Error Analysis
	fmt.Println("\n❌ Demo 5: Error Analysis")
	showErrorStats(ctx, mgr)

	// Demo 6: Time Series Data
	time.Sleep(2 * time.Second) // Let more tasks complete
	fmt.Println("\n📈 Demo 6: Time Series Data")
	showTimeSeries(ctx, mgr)

	// Demo 7: Performance Comparison
	fmt.Println("\n⚡ Demo 7: Performance Comparison")
	comparePerformance(ctx, mgr)

	fmt.Println("\n✅ Task Statistics Demos Complete!")
}

func enqueueSampleTasks(ctx context.Context, mgr *ergon.Manager) {
	// Enqueue orders (fast, high success)
	for i := 0; i < 50; i++ {
		_, err := ergon.Enqueue(mgr.Client(), ctx, ProcessOrderArgs{
			OrderID:    fmt.Sprintf("ORD-%d", i),
			Amount:     100.0 + rand.Float64()*400.0,
			CustomerID: fmt.Sprintf("CUST-%d", rand.Intn(20)),
		}, ergon.WithQueue("orders"))
		if err != nil {
			log.Printf("Failed to enqueue order: %v", err)
		}
	}

	// Enqueue emails (medium speed, some failures)
	for i := 0; i < 30; i++ {
		_, err := ergon.Enqueue(mgr.Client(), ctx, SendEmailArgs{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: "Your order confirmation",
			Body:    "Thank you for your order!",
		}, ergon.WithQueue("emails"))
		if err != nil {
			log.Printf("Failed to enqueue email: %v", err)
		}
	}

	// Enqueue reports (slow, occasional failures)
	for i := 0; i < 20; i++ {
		_, err := ergon.Enqueue(mgr.Client(), ctx, GenerateReportArgs{
			ReportType: []string{"sales", "inventory", "analytics"}[rand.Intn(3)],
			StartDate:  "2024-01-01",
			EndDate:    "2024-12-31",
		}, ergon.WithQueue("reports"))
		if err != nil {
			log.Printf("Failed to enqueue report: %v", err)
		}
	}

	fmt.Printf("  ✓ Enqueued 50 orders, 30 emails, 20 reports\n")
}

func showQueueStats(ctx context.Context, mgr *ergon.Manager) {
	queues := []string{"orders", "emails", "reports"}

	for _, queueName := range queues {
		stats, err := mgr.Stats().GetQueueStats(ctx, queueName)
		if err != nil {
			log.Printf("Failed to get stats for %s: %v", queueName, err)
			continue
		}

		fmt.Printf("\n  Queue: %s\n", queueName)
		fmt.Printf("  ─────────────────────────\n")
		fmt.Printf("  Pending:    %d\n", stats.PendingCount)
		fmt.Printf("  Running:    %d\n", stats.RunningCount)
		fmt.Printf("  Completed:  %d\n", stats.CompletedCount)
		fmt.Printf("  Failed:     %d\n", stats.FailedCount)
		fmt.Printf("  Success Rate: %.1f%%\n", stats.SuccessRate*100)
		fmt.Printf("  Avg Duration: %v\n", stats.AvgDuration.Round(time.Millisecond))
		fmt.Printf("  P95 Duration: %v\n", stats.P95Duration.Round(time.Millisecond))
		fmt.Printf("  Tasks/min: %.1f\n", stats.TasksPerMinute)
		if stats.OldestPendingAge > 0 {
			fmt.Printf("  Oldest Pending: %v ago\n", stats.OldestPendingAge.Round(time.Second))
		}
	}
}

func showKindStats(ctx context.Context, mgr *ergon.Manager) {
	kinds := []string{"process_order", "send_email", "generate_report"}

	for _, kind := range kinds {
		stats, err := mgr.Stats().GetKindStats(ctx, kind)
		if err != nil {
			log.Printf("Failed to get stats for %s: %v", kind, err)
			continue
		}

		fmt.Printf("\n  Kind: %s\n", kind)
		fmt.Printf("  ─────────────────────────\n")
		fmt.Printf("  Total Processed: %d\n", stats.TotalProcessed)
		fmt.Printf("  Success Rate: %.1f%%\n", stats.SuccessRate*100)
		fmt.Printf("  Avg Duration: %v\n", stats.AvgDuration.Round(time.Millisecond))
		fmt.Printf("  Median Duration: %v\n", stats.MedianDuration.Round(time.Millisecond))
		fmt.Printf("  P95 Duration: %v\n", stats.P95Duration.Round(time.Millisecond))
		fmt.Printf("  Avg Retries: %.2f\n", stats.AvgRetries)

		if len(stats.CommonErrors) > 0 {
			fmt.Printf("  Common Errors:\n")
			for _, e := range stats.CommonErrors {
				fmt.Printf("    - %s (%.1f%%)\n", e.Error, e.Percentage)
			}
		}
	}
}

func showOverallStats(ctx context.Context, mgr *ergon.Manager) {
	stats, err := mgr.Stats().GetOverallStats(ctx)
	if err != nil {
		log.Printf("Failed to get overall stats: %v", err)
		return
	}

	fmt.Printf("  Total Queues: %d\n", stats.TotalQueues)
	fmt.Printf("  Active Queues: %d\n", stats.ActiveQueues)
	fmt.Printf("  Total Tasks: %d\n", stats.TotalTasks)
	fmt.Printf("  Pending: %d\n", stats.PendingTasks)
	fmt.Printf("  Running: %d\n", stats.RunningTasks)
	fmt.Printf("  Completed: %d\n", stats.CompletedTasks)
	fmt.Printf("  Failed: %d\n", stats.FailedTasks)
	fmt.Printf("  Overall Success Rate: %.1f%%\n", stats.OverallSuccessRate*100)
	fmt.Printf("  Avg Duration: %v\n", stats.AvgDuration.Round(time.Millisecond))
	fmt.Printf("  Tasks/hour: %.1f\n", stats.TasksPerHour)

	if stats.BusiestQueue != "" {
		fmt.Printf("  Busiest Queue: %s\n", stats.BusiestQueue)
	}
	if stats.SlowestKind != "" {
		fmt.Printf("  Slowest Kind: %s\n", stats.SlowestKind)
	}
	if stats.FailingKind != "" {
		fmt.Printf("  Most Failing Kind: %s\n", stats.FailingKind)
	}
	if stats.StuckTasks > 0 {
		fmt.Printf("  ⚠️  Stuck Tasks: %d\n", stats.StuckTasks)
	}
}

func showErrorStats(ctx context.Context, mgr *ergon.Manager) {
	errors, err := mgr.Stats().GetErrorStats(ctx, "", 10)
	if err != nil {
		log.Printf("Failed to get error stats: %v", err)
		return
	}

	if len(errors) == 0 {
		fmt.Println("  No errors found! 🎉")
		return
	}

	fmt.Println("  Top Errors:")
	for i, e := range errors {
		fmt.Printf("    %d. %s\n", i+1, e.Error)
		fmt.Printf("       Count: %d (%.1f%%)\n", e.Count, e.Percentage)
		fmt.Printf("       Last Seen: %v ago\n", time.Since(e.LastSeen).Round(time.Second))
	}
}

// Worker stats not yet implemented - requires WorkerID field in InternalTask
/*
func showWorkerStats(ctx context.Context, mgr *ergon.Manager) {
	// Get running tasks to find active workers
	tasks, err := mgr.Tasks().GetTasksByState(ctx, "", ergon.StateRunning, 10)
	if err != nil {
		log.Printf("Failed to get running tasks: %v", err)
		return
	}

	workerIDs := make(map[string]bool)
	for _, task := range tasks {
		if task.Task.WorkerID != "" {
			workerIDs[task.Task.WorkerID] = true
		}
	}

	// Also check completed tasks
	completed, err := mgr.Tasks().GetTasksByState(ctx, "", ergon.StateCompleted, 100)
	if err == nil {
		for _, task := range completed {
			if task.Task.WorkerID != "" {
				workerIDs[task.Task.WorkerID] = true
			}
		}
	}

	if len(workerIDs) == 0 {
		fmt.Println("  No worker data available yet")
		return
	}

	for workerID := range workerIDs {
		stats, err := mgr.Stats().GetWorkerStats(ctx, workerID)
		if err != nil {
			log.Printf("Failed to get worker stats for %s: %v", workerID, err)
			continue
		}

		fmt.Printf("\n  Worker: %s\n", workerID)
		fmt.Printf("  Tasks Processed: %d\n", stats.TasksProcessed)
		fmt.Printf("  Success Rate: %.1f%%\n", stats.SuccessRate*100)
		fmt.Printf("  Avg Duration: %v\n", stats.AvgDuration.Round(time.Millisecond))

		if stats.CurrentTask != nil {
			fmt.Printf("  Current Task: %s\n", *stats.CurrentTask)
		}
		if stats.LastActive != nil {
			fmt.Printf("  Last Active: %v ago\n", time.Since(*stats.LastActive).Round(time.Second))
		}
	}
}
*/

func showTimeSeries(ctx context.Context, mgr *ergon.Manager) {
	// Get time series for orders queue over last 10 seconds
	ts, err := mgr.Stats().GetTimeSeries(ctx, ergon.TimeSeriesOptions{
		Queue:     "orders",
		StartTime: time.Now().Add(-10 * time.Second),
		EndTime:   time.Now(),
		Interval:  2 * time.Second,
	})
	if err != nil {
		log.Printf("Failed to get time series: %v", err)
		return
	}

	fmt.Printf("  Time Series for 'orders' queue (2s intervals):\n\n")
	fmt.Printf("  Time          Enqueued  Completed  Failed  Avg Latency\n")
	fmt.Printf("  ────────────  ────────  ─────────  ──────  ───────────\n")

	for _, dp := range ts.DataPoints {
		fmt.Printf("  %s  %8d  %9d  %6d  %v\n",
			dp.Timestamp.Format("15:04:05"),
			dp.Enqueued,
			dp.Completed,
			dp.Failed,
			dp.AvgLatency.Round(time.Millisecond),
		)
	}
}

func comparePerformance(ctx context.Context, mgr *ergon.Manager) {
	kinds := []string{"process_order", "send_email", "generate_report"}

	fmt.Printf("  Performance Comparison:\n\n")
	fmt.Printf("  Kind              Success Rate  Avg Duration  P95 Duration\n")
	fmt.Printf("  ────────────────  ────────────  ────────────  ────────────\n")

	for _, kind := range kinds {
		stats, err := mgr.Stats().GetKindStats(ctx, kind)
		if err != nil {
			continue
		}

		fmt.Printf("  %-16s  %11.1f%%  %12v  %12v\n",
			kind,
			stats.SuccessRate*100,
			stats.AvgDuration.Round(time.Millisecond),
			stats.P95Duration.Round(time.Millisecond),
		)
	}

	fmt.Println("\n  💡 Insights:")

	// Analyze and provide insights
	var slowest string
	var slowestDuration time.Duration
	var leastReliable string
	var lowestSuccess float64 = 1.0

	for _, kind := range kinds {
		stats, err := mgr.Stats().GetKindStats(ctx, kind)
		if err != nil {
			continue
		}

		if stats.AvgDuration > slowestDuration {
			slowestDuration = stats.AvgDuration
			slowest = kind
		}

		if stats.SuccessRate < lowestSuccess {
			lowestSuccess = stats.SuccessRate
			leastReliable = kind
		}
	}

	if slowest != "" {
		fmt.Printf("    - '%s' is the slowest task type (~%v)\n",
			slowest, slowestDuration.Round(time.Millisecond))
	}
	if leastReliable != "" {
		fmt.Printf("    - '%s' has the lowest success rate (%.1f%%)\n",
			leastReliable, lowestSuccess*100)
	}
}
