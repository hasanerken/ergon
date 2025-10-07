//go:build ignore

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

// Task 1: Send Email
type SendEmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

type EmailWorker struct{}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[SendEmailArgs]) error {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("📧 EMAIL TASK EXECUTED!\n")
	fmt.Printf("To: %s\n", task.Args.To)
	fmt.Printf("Subject: %s\n", task.Args.Subject)
	fmt.Printf("Body: %s\n", task.Args.Body)
	fmt.Printf("Scheduled: %v | Executed: %v\n", task.ScheduledAt, time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))
	return nil
}

// Task 2: Process Payment
type ProcessPaymentArgs struct {
	UserID      string  `json:"user_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Description string  `json:"description"`
}

func (ProcessPaymentArgs) Kind() string { return "process_payment" }

type PaymentWorker struct{}

func (w *PaymentWorker) Work(ctx context.Context, task *ergon.Task[ProcessPaymentArgs]) error {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("💳 PAYMENT TASK EXECUTED!\n")
	fmt.Printf("User ID: %s\n", task.Args.UserID)
	fmt.Printf("Amount: %.2f %s\n", task.Args.Amount, task.Args.Currency)
	fmt.Printf("Description: %s\n", task.Args.Description)
	fmt.Printf("Scheduled: %v | Executed: %v\n", task.ScheduledAt, time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))
	return nil
}

// Task 3: Generate Report
type GenerateReportArgs struct {
	ReportType string    `json:"report_type"`
	StartDate  time.Time `json:"start_date"`
	EndDate    time.Time `json:"end_date"`
	Format     string    `json:"format"`
	Recipients []string  `json:"recipients"`
}

func (GenerateReportArgs) Kind() string { return "generate_report" }

type ReportWorker struct{}

func (w *ReportWorker) Work(ctx context.Context, task *ergon.Task[GenerateReportArgs]) error {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("📊 REPORT TASK EXECUTED!\n")
	fmt.Printf("Type: %s\n", task.Args.ReportType)
	fmt.Printf("Period: %s to %s\n",
		task.Args.StartDate.Format("2006-01-02"),
		task.Args.EndDate.Format("2006-01-02"))
	fmt.Printf("Format: %s\n", task.Args.Format)
	fmt.Printf("Recipients: %v\n", task.Args.Recipients)
	fmt.Printf("Scheduled: %v | Executed: %v\n", task.ScheduledAt, time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))
	return nil
}

// Task 4: Send Notification
type SendNotificationArgs struct {
	UserID  string            `json:"user_id"`
	Title   string            `json:"title"`
	Message string            `json:"message"`
	Type    string            `json:"type"`
	Data    map[string]string `json:"data"`
}

func (SendNotificationArgs) Kind() string { return "send_notification" }

type NotificationWorker struct{}

func (w *NotificationWorker) Work(ctx context.Context, task *ergon.Task[SendNotificationArgs]) error {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("🔔 NOTIFICATION TASK EXECUTED!\n")
	fmt.Printf("User: %s\n", task.Args.UserID)
	fmt.Printf("Type: %s\n", task.Args.Type)
	fmt.Printf("Title: %s\n", task.Args.Title)
	fmt.Printf("Message: %s\n", task.Args.Message)
	fmt.Printf("Data: %v\n", task.Args.Data)
	fmt.Printf("Scheduled: %v | Executed: %v\n", task.ScheduledAt, time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))
	return nil
}

func main() {
	ctx := context.Background()

	// Clean up old data
	os.RemoveAll("./data/multi_test")

	// Create BadgerDB store
	store, err := badger.NewStore("./data/multi_test")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &PaymentWorker{})
	ergon.AddWorker(workers, &ReportWorker{})
	ergon.AddWorker(workers, &NotificationWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	fmt.Println("\n🧪 ERGON MULTI-TASK SCHEDULING TEST - BadgerDB")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("⏰ Current time: %s\n", time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))

	now := time.Now()

	// Schedule different tasks at different times
	fmt.Println("\n📅 Scheduling different task types:")
	fmt.Println(strings.Repeat("-", 70))

	// Task 1: Email in 1 minute
	emailTime := now.Add(1 * time.Minute)
	task1, err := ergon.Enqueue(client, ctx, SendEmailArgs{
		To:      "user@example.com",
		Subject: "Scheduled Email Test",
		Body:    "This email was scheduled for " + emailTime.Format("15:04:05"),
	}, ergon.WithScheduledAt(emailTime))
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  ✓ [EMAIL] Scheduled for %s\n", emailTime.Format("15:04:05"))
		fmt.Printf("    ID: %s | State: %s\n", task1.ID[:16]+"...", task1.State)
	}

	// Task 2: Payment in 2 minutes
	paymentTime := now.Add(2 * time.Minute)
	task2, err := ergon.Enqueue(client, ctx, ProcessPaymentArgs{
		UserID:      "user_123",
		Amount:      99.99,
		Currency:    "USD",
		Description: "Monthly subscription",
	}, ergon.WithScheduledAt(paymentTime))
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  ✓ [PAYMENT] Scheduled for %s\n", paymentTime.Format("15:04:05"))
		fmt.Printf("    ID: %s | State: %s\n", task2.ID[:16]+"...", task2.State)
	}

	// Task 3: Report in 3 minutes
	reportTime := now.Add(3 * time.Minute)
	task3, err := ergon.Enqueue(client, ctx, GenerateReportArgs{
		ReportType: "Sales Summary",
		StartDate:  time.Now().AddDate(0, 0, -7),
		EndDate:    time.Now(),
		Format:     "PDF",
		Recipients: []string{"manager@example.com", "ceo@example.com"},
	}, ergon.WithScheduledAt(reportTime))
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  ✓ [REPORT] Scheduled for %s\n", reportTime.Format("15:04:05"))
		fmt.Printf("    ID: %s | State: %s\n", task3.ID[:16]+"...", task3.State)
	}

	// Task 4: Notification in 4 minutes
	notifTime := now.Add(4 * time.Minute)
	task4, err := ergon.Enqueue(client, ctx, SendNotificationArgs{
		UserID:  "user_456",
		Title:   "System Maintenance",
		Message: "Scheduled maintenance will begin soon",
		Type:    "info",
		Data: map[string]string{
			"priority": "medium",
			"category": "system",
		},
	}, ergon.WithScheduledAt(notifTime))
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  ✓ [NOTIFICATION] Scheduled for %s\n", notifTime.Format("15:04:05"))
		fmt.Printf("    ID: %s | State: %s\n", task4.ID[:16]+"...", task4.State)
	}

	// Start server
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("🚀 Starting server...")
	fmt.Println(strings.Repeat("=", 70))

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:     workers,
		Concurrency: 4,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   3,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
		},

		OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
			log.Printf("▶️  Task started: %s (%s)", task.Kind, task.ID[:16]+"...")
		},

		OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
			log.Printf("✅ Task completed: %s (%s) - took %v", task.Kind, task.ID[:16]+"...", duration.Round(time.Millisecond))
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	// Start server in background
	go func() {
		if err := server.Run(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n✅ Server running!")
	fmt.Println("📊 The scheduler checks for scheduled tasks every 5 seconds")
	fmt.Println("⏰ Watch different task types execute at their scheduled times...")
	fmt.Println("\nPress Ctrl+C to stop\n")

	// Show status updates
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			scheduled, _ := store.CountTasks(ctx, &ergon.TaskFilter{
				State: ergon.StateScheduled,
			})

			completed, _ := store.CountTasks(ctx, &ergon.TaskFilter{
				State: ergon.StateCompleted,
			})

			fmt.Printf("\n📊 Status: %d scheduled, %d completed\n", scheduled, completed)
		}
	}()

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n\n🛑 Shutting down...")
	server.Stop(ctx)
}
