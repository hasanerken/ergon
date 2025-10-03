package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/internal/jsonutil/monitor"
	"github.com/hasanerken/ergon/store/badger"
)

// Example task arguments
type EmailTask struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailTask) Kind() string { return "send_email" }

// Email worker
type EmailWorker struct{}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailTask]) error {
	log.Printf("Sending email to %s: %s", task.Args.To, task.Args.Subject)
	time.Sleep(2 * time.Second) // Simulate work
	return nil
}

// Example data processing task
type ProcessDataTask struct {
	DataID string `json:"data_id"`
	Action string `json:"action"`
}

func (ProcessDataTask) Kind() string { return "process_data" }

type DataWorker struct{}

func (w *DataWorker) Work(ctx context.Context, task *ergon.Task[ProcessDataTask]) error {
	log.Printf("Processing data %s with action %s", task.Args.DataID, task.Args.Action)
	time.Sleep(3 * time.Second) // Simulate work
	return nil
}

func main() {
	ctx := context.Background()

	// Setup BadgerDB store
	store, err := badger.NewStore("./data/monitor_example")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Create workers registry
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &DataWorker{})

	// Create manager
	manager := ergon.NewManager(store, ergon.ClientConfig{
		Workers: workers, // Enable validation at enqueue time
	})
	defer manager.Close()

	// Create and start server
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:     workers,
		Concurrency: 10,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   5,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
			"emails": {
				MaxWorkers:   3,
				Priority:     2,
				PollInterval: 500 * time.Millisecond,
			},
			"data": {
				MaxWorkers:   2,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
		},
		Middleware: []ergon.MiddlewareFunc{
			ergon.LoggingMiddleware(),
			ergon.RecoveryMiddleware(),
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
	defer server.Stop(ctx)

	// Create monitor web UI
	monitorServer, err := monitor.NewServer(manager, monitor.Config{
		Addr:     ":8888",
		BasePath: "/monitor",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start monitor in background
	go func() {
		log.Println("Starting monitor web UI on http://localhost:8888/monitor")
		if err := monitorServer.Start(); err != nil {
			log.Printf("Monitor server error: %v", err)
		}
	}()
	defer monitorServer.Stop()

	// Enqueue some example tasks
	log.Println("Enqueueing example tasks...")

	// Email tasks
	for i := 1; i <= 10; i++ {
		_, err := ergon.Enqueue(manager.Client(), ctx, EmailTask{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: fmt.Sprintf("Test Email %d", i),
			Body:    "This is a test email",
		}, ergon.WithQueue("emails"))
		if err != nil {
			log.Printf("Error enqueueing email task: %v", err)
		}
	}

	// Data processing tasks
	for i := 1; i <= 15; i++ {
		// Schedule some tasks for the future
		if i%3 == 0 {
			_, err := ergon.Enqueue(manager.Client(), ctx, ProcessDataTask{
				DataID: fmt.Sprintf("DATA-%d", i),
				Action: "process",
			}, ergon.WithQueue("data"), ergon.WithDelay(30*time.Second))
			if err != nil {
				log.Printf("Error enqueueing data task: %v", err)
			}
		} else {
			_, err := ergon.Enqueue(manager.Client(), ctx, ProcessDataTask{
				DataID: fmt.Sprintf("DATA-%d", i),
				Action: "process",
			}, ergon.WithQueue("data"))
			if err != nil {
				log.Printf("Error enqueueing data task: %v", err)
			}
		}
	}

	// Some tasks with different priorities
	for i := 1; i <= 5; i++ {
		priority := i * 10
		_, err := ergon.Enqueue(manager.Client(), ctx, EmailTask{
			To:      fmt.Sprintf("priority%d@example.com", i),
			Subject: fmt.Sprintf("Priority %d Email", priority),
			Body:    "High priority email",
		}, ergon.WithQueue("emails"), ergon.WithPriority(priority))
		if err != nil {
			log.Printf("Error enqueueing priority task: %v", err)
		}
	}

	log.Println("✓ Tasks enqueued successfully")
	log.Println("\n===========================================")
	log.Println("Monitor UI: http://localhost:8888/monitor")
	log.Println("===========================================")
	log.Println("\nFeatures to try:")
	log.Println("  • View dashboard with real-time statistics")
	log.Println("  • Browse tasks by queue, state, or kind")
	log.Println("  • View detailed task information")
	log.Println("  • Cancel, retry, or delete tasks")
	log.Println("  • Monitor queue performance")
	log.Println("\nPress Ctrl+C to stop")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")
}
