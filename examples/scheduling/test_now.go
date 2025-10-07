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

// Simple test task
type TestTaskArgs struct {
	Message      string    `json:"message"`
	ScheduledFor time.Time `json:"scheduled_for"`
	TaskNumber   int       `json:"task_number"`
}

func (TestTaskArgs) Kind() string { return "test_scheduled_task" }

// Worker that executes the test task
type TestWorker struct{}

func (w *TestWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
	now := time.Now()
	scheduled := task.Args.ScheduledFor

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("🎯 TASK #%d EXECUTED!\n", task.Args.TaskNumber)
	fmt.Printf("📋 Message: %s\n", task.Args.Message)
	fmt.Printf("⏰ Scheduled for: %s\n", scheduled.Format("15:04:05"))
	fmt.Printf("✅ Executed at:   %s\n", now.Format("15:04:05"))
	fmt.Printf("⏱️  Delay: %v\n", now.Sub(scheduled).Round(time.Second))
	fmt.Println(strings.Repeat("=", 70))

	return nil
}

func main() {
	ctx := context.Background()

	// Clean up old data
	os.RemoveAll("./data/schedule_test")

	// Create store
	store, err := badger.NewStore("./data/schedule_test")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &TestWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	fmt.Println("\n🧪 ERGON SCHEDULING TEST - NEXT FEW MINUTES")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("⏰ Current time: %s\n", time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 70))

	// Get current time
	now := time.Now()

	// Schedule tasks for specific times in the next few minutes
	// Using SPECIFIC DATES, not relative delays!

	schedules := []struct {
		minutesFromNow int
		message        string
	}{
		{1, "First task - 1 minute from start"},
		{2, "Second task - 2 minutes from start"},
		{3, "Third task - 3 minutes from start"},
		{4, "Fourth task - 4 minutes from start"},
		{5, "Fifth task - 5 minutes from start"},
	}

	fmt.Println("\n📅 Scheduling tasks with SPECIFIC DATES:")
	fmt.Println(strings.Repeat("-", 70))

	for i, sched := range schedules {
		// Calculate SPECIFIC date/time (not using WithDelay!)
		targetTime := time.Date(
			now.Year(),
			now.Month(),
			now.Day(),
			now.Hour(),
			now.Minute()+sched.minutesFromNow,
			now.Second(),
			0, // nanoseconds
			time.Local,
		)

		task, err := ergon.Enqueue(client, ctx, TestTaskArgs{
			Message:      sched.message,
			ScheduledFor: targetTime,
			TaskNumber:   i + 1,
		},
			ergon.WithScheduledAt(targetTime), // Using specific date!
		)

		if err != nil {
			log.Printf("Error enqueueing task: %v", err)
			continue
		}

		fmt.Printf("  ✓ Task #%d scheduled for: %s (in %d min)\n",
			i+1,
			targetTime.Format("15:04:05"),
			sched.minutesFromNow,
		)
		fmt.Printf("    Task ID: %s\n", task.ID[:16]+"...")
		fmt.Printf("    State: %s\n", task.State)
		fmt.Println()
	}

	// Start server with event callbacks to show real-time execution
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("🚀 Starting server...")
	fmt.Println(strings.Repeat("=", 70))

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:     workers,
		Concurrency: 3,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   2,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
		},

		// Event callbacks to show what's happening
		OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
			log.Printf("▶️  Task started: %s", task.ID[:16]+"...")
		},

		OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
			log.Printf("✅ Task completed: %s (took %v)", task.ID[:16]+"...", duration.Round(time.Millisecond))
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

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n✅ Server running!")
	fmt.Println("📊 The scheduler checks for scheduled tasks every 5 seconds")
	fmt.Println("⏰ Watch tasks execute at their scheduled times...")
	fmt.Println("\nPress Ctrl+C to stop\n")

	// Show countdown timer
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// Count scheduled tasks
			scheduled, _ := store.CountTasks(ctx, &ergon.TaskFilter{
				State: ergon.StateScheduled,
			})

			// Count completed tasks
			completed, _ := store.CountTasks(ctx, &ergon.TaskFilter{
				State: ergon.StateCompleted,
			})

			fmt.Printf("\n📊 Status: %d scheduled, %d completed\n",
				scheduled, completed)
		}
	}()

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n\n🛑 Shutting down...")
	server.Stop(ctx)
}
