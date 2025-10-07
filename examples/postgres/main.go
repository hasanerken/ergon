package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Task types
type EmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailArgs) Kind() string { return "email" }

type OrderArgs struct {
	OrderID string  `json:"order_id"`
	Amount  float64 `json:"amount"`
}

func (OrderArgs) Kind() string { return "order" }

// Workers
type EmailWorker struct {
	ergon.WorkerDefaults[EmailArgs]
}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailArgs]) error {
	log.Printf("📧 [Worker] Sending email to %s: %s", task.Args.To, task.Args.Subject)
	time.Sleep(200 * time.Millisecond)
	return nil
}

type OrderWorker struct {
	ergon.WorkerDefaults[OrderArgs]
}

func (w *OrderWorker) Work(ctx context.Context, task *ergon.Task[OrderArgs]) error {
	log.Printf("📦 [Worker] Processing order %s ($%.2f)", task.Args.OrderID, task.Args.Amount)
	time.Sleep(300 * time.Millisecond)
	return nil
}

func main() {
	log.Println("🐘 Zeus Queue - PostgreSQL Multi-Node Example")
	log.Println("=" + string(make([]byte, 60)))

	// NOTE: This example requires a PostgreSQL database
	// Default connection to local PostgreSQL
	dsn := "postgres://postgres:postgres@localhost/zeus_queue?sslmode=disable"

	// You can override with environment variable
	// export POSTGRES_DSN="postgres://user:pass@host/dbname"
	// if envDSN := os.Getenv("POSTGRES_DSN"); envDSN != "" {
	//     dsn = envDSN
	// }

	log.Printf("Connecting to PostgreSQL...")
	log.Printf("DSN: %s", maskPassword(dsn))

	// Create store
	store, err := postgres.NewStoreFromDSN(dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to PostgreSQL: %v", err)
		log.Println("\n💡 Make sure PostgreSQL is running and database exists:")
		log.Println("   createdb zeus_queue")
		log.Println("   psql zeus_queue < pkg/queue/store/postgres/schema.sql")
		return
	}
	defer store.Close()

	log.Println("✅ Connected to PostgreSQL")

	// Test connection
	if err := store.Ping(context.Background()); err != nil {
		log.Fatalf("❌ Failed to ping database: %v", err)
	}

	ctx := context.Background()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &OrderWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	// Try to acquire leader lease (for scheduler)
	log.Println("\n🔐 Attempting to acquire leader lease...")
	lease, err := store.TryAcquireLease(ctx, "scheduler", 30*time.Second)
	isLeader := err == nil

	if isLeader {
		log.Println("✅ Acquired leader lease - this instance will run the scheduler")
	} else {
		log.Printf("⚠️  Another instance is the leader: %v", err)
		log.Println("   This instance will only process tasks")
	}

	// Create server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Concurrency: 3,
		Workers:     workers,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				Priority:     1,
				PollInterval: 1 * time.Second,
			},
			"emails": {
				MaxWorkers:   1,
				Priority:     2,
				PollInterval: 1 * time.Second,
			},
		},
		Middleware: []ergon.MiddlewareFunc{
			ergon.LoggingMiddleware(),
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

	time.Sleep(500 * time.Millisecond)

	// Renew lease periodically if we're the leader
	if isLeader {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := store.RenewLease(ctx, lease); err != nil {
					log.Printf("⚠️  Failed to renew lease: %v", err)
				} else {
					log.Println("🔄 Renewed leader lease")
				}
			}
		}()
	}

	// Enqueue tasks
	log.Println("\n📝 Enqueueing tasks...")

	// Standard task
	task1, err := ergon.Enqueue(client, ctx, EmailArgs{
		To:      "user@example.com",
		Subject: "Welcome!",
		Body:    "Welcome to Zeus Queue",
	})
	if err != nil {
		log.Fatalf("Failed to enqueue: %v", err)
	}
	log.Printf("✓ Enqueued email task: %s", task1.ID)

	// High priority task
	task2, err := ergon.Enqueue(client, ctx, OrderArgs{
		OrderID: "ORDER-001",
		Amount:  99.99,
	}, ergon.WithPriority(10))
	if err != nil {
		log.Fatalf("Failed to enqueue: %v", err)
	}
	log.Printf("✓ Enqueued high-priority order: %s", task2.ID)

	// Scheduled task
	task3, err := ergon.Enqueue(client, ctx, EmailArgs{
		To:      "admin@example.com",
		Subject: "Scheduled Email",
		Body:    "This was scheduled",
	}, ergon.WithDelay(3*time.Second))
	if err != nil {
		log.Fatalf("Failed to enqueue: %v", err)
	}
	log.Printf("✓ Enqueued scheduled task: %s (runs in 3s)", task3.ID)

	// Unique task
	task4, err := ergon.Enqueue(client, ctx, EmailArgs{
		To:      "newsletter@example.com",
		Subject: "Newsletter",
		Body:    "Weekly newsletter",
	}, ergon.AtMostOncePerHour())
	if err != nil {
		log.Fatalf("Failed to enqueue: %v", err)
	}
	log.Printf("✓ Enqueued unique task: %s", task4.ID)

	// Try duplicate (should fail)
	_, err = ergon.Enqueue(client, ctx, EmailArgs{
		To:      "newsletter@example.com",
		Subject: "Newsletter",
		Body:    "Weekly newsletter",
	}, ergon.AtMostOncePerHour())
	if err != nil {
		log.Printf("✓ Duplicate rejected (expected): %v", err)
	}

	// Transactional enqueue
	log.Println("\n🔄 Demonstrating transactional energon...")
	if err := demonstrateTransaction(ctx, store, client); err != nil {
		log.Printf("Transaction demo failed: %v", err)
	}

	// Show queue stats
	log.Println("\n📊 Queue Statistics:")
	queues, err := store.ListQueues(ctx)
	if err != nil {
		log.Printf("Failed to list queues: %v", err)
	} else {
		for _, q := range queues {
			log.Printf("  Queue: %s", q.Name)
			log.Printf("    Pending: %d, Running: %d, Completed: %d, Failed: %d",
				q.PendingCount, q.RunningCount, q.CompletedCount, q.FailedCount)
		}
	}

	// Wait for processing
	log.Println("\n⏳ Processing tasks (10 seconds)...")
	log.Println("   (In multi-node setup, these would be distributed across servers)")
	time.Sleep(10 * time.Second)

	// Cleanup
	log.Println("\n🛑 Shutting down...")
	if isLeader {
		store.ReleaseLease(ctx, lease)
		log.Println("✓ Released leader lease")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server.Stop(shutdownCtx)

	log.Println("\n✅ Example completed!")
	log.Println("\n💡 Multi-Node Tips:")
	log.Println("   1. Run this example on multiple servers")
	log.Println("   2. They'll share the same PostgreSQL queue")
	log.Println("   3. Only one will be the leader (runs scheduler)")
	log.Println("   4. All will process tasks from the shared queue")
	log.Println("   5. Use load balancer to distribute enqueue requests")
}

func demonstrateTransaction(ctx context.Context, store *postgres.Store, client *ergon.Client) error {
	// Begin transaction
	tx, err := store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	log.Println("  Started transaction...")

	// Enqueue task within transaction
	task, err := ergon.EnqueueTx(client, ctx, tx, OrderArgs{
		OrderID: "ORDER-TX-001",
		Amount:  299.99,
	})
	if err != nil {
		return fmt.Errorf("failed to enqueue in tx: %w", err)
	}

	log.Printf("  ✓ Enqueued in transaction: %s", task.ID)

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit tx: %w", err)
	}

	log.Println("  ✓ Transaction committed - task is now visible")
	return nil
}

func maskPassword(dsn string) string {
	// Simple password masking for display
	// postgres://user:PASSWORD@host/db -> postgres://user:***@host/db
	// This is a simple version, you might want more robust parsing
	return dsn // For now, just return as-is for demo
}
