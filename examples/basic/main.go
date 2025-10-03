package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// ============================================================================
// DEFINE TASK ARGUMENTS (implements TaskArgs interface)
// ============================================================================

// SendEmailArgs represents arguments for sending an email
type SendEmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

// ProcessOrderArgs represents arguments for order processing
type ProcessOrderArgs struct {
	OrderID    string  `json:"order_id"`
	CustomerID string  `json:"customer_id"`
	Amount     float64 `json:"amount"`
}

func (ProcessOrderArgs) Kind() string { return "process_order" }

// LogArgs represents simple logging task
type LogArgs struct {
	Message string `json:"message"`
	Level   string `json:"level"`
}

func (LogArgs) Kind() string { return "log" }

// ============================================================================
// DEFINE WORKERS
// ============================================================================

// EmailWorker sends emails
type EmailWorker struct {
	ergon.WorkerDefaults[SendEmailArgs]
}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[SendEmailArgs]) error {
	log.Printf("📧 Sending email to %s: %s", task.Args.To, task.Args.Subject)

	// Simulate email sending
	time.Sleep(100 * time.Millisecond)

	log.Printf("✅ Email sent successfully to %s", task.Args.To)
	return nil
}

func (w *EmailWorker) Timeout(task *ergon.Task[SendEmailArgs]) time.Duration {
	return 30 * time.Second
}

// OrderWorker processes orders
type OrderWorker struct {
	ergon.WorkerDefaults[ProcessOrderArgs]
}

func (w *OrderWorker) Work(ctx context.Context, task *ergon.Task[ProcessOrderArgs]) error {
	log.Printf("📦 Processing order %s for customer %s (amount: $%.2f)",
		task.Args.OrderID, task.Args.CustomerID, task.Args.Amount)

	// Simulate order processing
	time.Sleep(200 * time.Millisecond)

	// Simulate occasional failure
	if task.Args.Amount > 1000 {
		return fmt.Errorf("order amount too high, requires manual review")
	}

	log.Printf("✅ Order %s processed successfully", task.Args.OrderID)
	return nil
}

func (w *OrderWorker) MaxRetries(task *ergon.Task[ProcessOrderArgs]) int {
	return 3 // Only retry 3 times
}

func (w *OrderWorker) RetryDelay(task *ergon.Task[ProcessOrderArgs], attempt int, err error) time.Duration {
	// Linear backoff: 5s, 10s, 15s
	return time.Duration(attempt*5) * time.Second
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	ctx := context.Background()

	// Use Badger store (embedded, no infrastructure needed!)
	store, err := badger.NewStore("./queue_data")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	defer os.RemoveAll("./queue_data") // Clean up after example

	// Create workers registry
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &OrderWorker{})

	// Add a simple logging worker using WorkFunc
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[LogArgs]) error {
		log.Printf("[%s] %s", task.Args.Level, task.Args.Message)
		return nil
	})

	// Create client for enqueuing tasks
	client := ergon.NewClient(store, ergon.ClientConfig{
		DefaultQueue:   "default",
		DefaultRetries: 25,
		DefaultTimeout: 5 * time.Minute,
		Workers:        workers, // Validates task kinds at enqueue
	})

	// Create server for processing tasks
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: 5,
		Workers:     workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   3,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
			"emails": {
				MaxWorkers:   2,
				Priority:     2,
				PollInterval: 500 * time.Millisecond,
			},
		},
		Middleware: []ergon.MiddlewareFunc{
			ergon.LoggingMiddleware(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	log.Println("🚀 Zeus Queue Example - Enqueueing tasks...")

	// ========================================================================
	// EXAMPLE 1: Basic task enqueueing
	// ========================================================================

	task1, err := ergon.Enqueue(client, ctx, SendEmailArgs{
		To:      "user@example.com",
		Subject: "Welcome!",
		Body:    "Thanks for signing up!",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued task: %s", task1.ID)

	// ========================================================================
	// EXAMPLE 2: Task with options
	// ========================================================================

	task2, err := ergon.Enqueue(client, ctx, SendEmailArgs{
		To:      "admin@example.com",
		Subject: "Alert",
		Body:    "System alert!",
	},
		ergon.WithQueue("emails"),
		ergon.WithPriority(10),
		ergon.WithTimeout(10*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued high-priority email: %s", task2.ID)

	// ========================================================================
	// EXAMPLE 3: Scheduled task
	// ========================================================================

	task3, err := ergon.Enqueue(client, ctx, LogArgs{
		Message: "This is a scheduled log message",
		Level:   "INFO",
	},
		ergon.WithScheduledAt(time.Now().Add(2*time.Second)),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued scheduled task: %s (runs in 2 seconds)", task3.ID)

	// ========================================================================
	// EXAMPLE 4: Unique task (at most once per hour)
	// ========================================================================

	task4, err := ergon.Enqueue(client, ctx, ProcessOrderArgs{
		OrderID:    "ORDER-001",
		CustomerID: "CUST-123",
		Amount:     99.99,
	},
		ergon.AtMostOncePerHour(),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued unique order task: %s", task4.ID)

	// Try to enqueue duplicate (should fail with ErrDuplicateTask)
	_, err = ergon.Enqueue(client, ctx, ProcessOrderArgs{
		OrderID:    "ORDER-001",
		CustomerID: "CUST-123",
		Amount:     99.99,
	},
		ergon.AtMostOncePerHour(),
	)
	if err != nil {
		log.Printf("✗ Duplicate task rejected: %v", err)
	}

	// ========================================================================
	// EXAMPLE 5: Task with custom retry
	// ========================================================================

	task5, err := ergon.Enqueue(client, ctx, ProcessOrderArgs{
		OrderID:    "ORDER-002",
		CustomerID: "CUST-456",
		Amount:     1500.00, // This will fail and retry
	},
		ergon.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued order (will fail and retry): %s", task5.ID)

	// ========================================================================
	// EXAMPLE 6: Batch enqueue
	// ========================================================================

	emails := []SendEmailArgs{
		{To: "user1@example.com", Subject: "Newsletter", Body: "Check out our latest updates!"},
		{To: "user2@example.com", Subject: "Newsletter", Body: "Check out our latest updates!"},
		{To: "user3@example.com", Subject: "Newsletter", Body: "Check out our latest updates!"},
	}

	tasks, err := ergon.EnqueueMany(client, ctx, emails, ergon.WithQueue("emails"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued %d newsletter emails", len(tasks))

	// ========================================================================
	// EXAMPLE 7: Delayed task
	// ========================================================================

	task6, err := ergon.Enqueue(client, ctx, LogArgs{
		Message: "Delayed notification",
		Level:   "INFO",
	},
		ergon.WithDelay(5*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("✓ Enqueued delayed task: %s (runs in 5 seconds)", task6.ID)

	// Wait for tasks to process
	log.Println("\n⏳ Processing tasks...")
	time.Sleep(10 * time.Second)

	// Graceful shutdown
	log.Println("\n🛑 Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	log.Println("✅ Example completed!")
}
