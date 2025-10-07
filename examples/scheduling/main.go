package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// Task to send birthday wishes
type BirthdayWishArgs struct {
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Birthday  time.Time `json:"birthday"`
	Message   string    `json:"message"`
}

func (BirthdayWishArgs) Kind() string { return "send_birthday_wish" }

// Worker that sends birthday wishes
type BirthdayWorker struct{}

func (w *BirthdayWorker) Work(ctx context.Context, task *ergon.Task[BirthdayWishArgs]) error {
	log.Printf("🎉 Sending birthday wish to %s (User ID: %s)",
		task.Args.Name, task.Args.UserID)
	log.Printf("📧 Message: %s", task.Args.Message)
	log.Printf("🎂 Birthday: %s", task.Args.Birthday.Format("January 2, 2006"))

	// Simulate sending email/notification
	time.Sleep(500 * time.Millisecond)

	log.Printf("✅ Birthday wish sent to %s!", task.Args.Name)
	return nil
}

// Reminder task
type ReminderArgs struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DueDate     time.Time `json:"due_date"`
}

func (ReminderArgs) Kind() string { return "send_reminder" }

type ReminderWorker struct{}

func (w *ReminderWorker) Work(ctx context.Context, task *ergon.Task[ReminderArgs]) error {
	log.Printf("⏰ REMINDER: %s", task.Args.Title)
	log.Printf("📝 %s", task.Args.Description)
	log.Printf("📅 Due: %s", task.Args.DueDate.Format("January 2, 2006 at 3:04 PM"))
	return nil
}

func main() {
	ctx := context.Background()

	// Create store
	store, err := badger.NewStore("./data/scheduling_demo")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &BirthdayWorker{})
	ergon.AddWorker(workers, &ReminderWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	fmt.Println("🗓️  ERGON TASK SCHEDULING EXAMPLES")
	fmt.Println("=" + string(make([]byte, 50)))

	// ========================================================================
	// EXAMPLE 1: Schedule for specific date and time
	// ========================================================================
	fmt.Println("\n📅 Example 1: Schedule task for October 15, 2025 at 14:13")

	// Define the exact time: October 15, 2025 at 14:13:00
	targetTime := time.Date(2025, time.October, 15, 14, 13, 0, 0, time.UTC)

	task1, err := ergon.Enqueue(client, ctx, BirthdayWishArgs{
		UserID:   "user_123",
		Name:     "Alice Johnson",
		Birthday: targetTime,
		Message:  "Happy Birthday Alice! 🎉🎂",
	},
		ergon.WithScheduledAt(targetTime),
		ergon.WithQueue("birthdays"),
	)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("✅ Task scheduled successfully!\n")
	fmt.Printf("   Task ID: %s\n", task1.ID)
	fmt.Printf("   Will execute on: %s\n", targetTime.Format("Monday, January 2, 2006 at 3:04 PM MST"))
	fmt.Printf("   Time until execution: %v\n", time.Until(targetTime))

	// ========================================================================
	// EXAMPLE 2: Multiple future dates
	// ========================================================================
	fmt.Println("\n📅 Example 2: Schedule multiple tasks for different dates")

	futureDates := []struct {
		date time.Time
		name string
	}{
		{time.Date(2025, 12, 25, 9, 0, 0, 0, time.UTC), "Christmas Morning"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "New Year 2026"},
		{time.Date(2025, 11, 28, 12, 0, 0, 0, time.UTC), "Thanksgiving"},
	}

	for _, fd := range futureDates {
		task, err := ergon.Enqueue(client, ctx, ReminderArgs{
			Title:       fd.name + " Reminder",
			Description: "Don't forget about " + fd.name,
			DueDate:     fd.date,
		},
			ergon.WithScheduledAt(fd.date),
		)

		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Printf("   ✓ %s scheduled for %s (ID: %s)\n",
			fd.name,
			fd.date.Format("Jan 2, 2006 3:04 PM"),
			task.ID[:8]+"...")
	}

	// ========================================================================
	// EXAMPLE 3: Schedule with delay (relative time)
	// ========================================================================
	fmt.Println("\n⏰ Example 3: Schedule tasks with relative delays")

	// In 30 seconds
	task2, _ := ergon.Enqueue(client, ctx, ReminderArgs{
		Title:       "Quick Reminder",
		Description: "This will run in 30 seconds",
		DueDate:     time.Now().Add(30 * time.Second),
	},
		ergon.WithDelay(30 * time.Second),
	)
	fmt.Printf("   ✓ Quick reminder scheduled (in 30s, ID: %s)\n", task2.ID[:8]+"...")

	// In 5 minutes
	task3, _ := ergon.Enqueue(client, ctx, ReminderArgs{
		Title:       "Follow-up Task",
		Description: "This will run in 5 minutes",
		DueDate:     time.Now().Add(5 * time.Minute),
	},
		ergon.WithProcessIn(5 * time.Minute),
	)
	fmt.Printf("   ✓ Follow-up scheduled (in 5m, ID: %s)\n", task3.ID[:8]+"...")

	// ========================================================================
	// EXAMPLE 4: Business hours scheduling
	// ========================================================================
	fmt.Println("\n🏢 Example 4: Schedule for next business day at 9 AM")

	nextBusinessDay := getNextBusinessDay(time.Now())
	businessHourTime := time.Date(
		nextBusinessDay.Year(),
		nextBusinessDay.Month(),
		nextBusinessDay.Day(),
		9, 0, 0, 0, // 9:00 AM
		time.Local,
	)

	task4, _ := ergon.Enqueue(client, ctx, ReminderArgs{
		Title:       "Morning Task",
		Description: "Execute this during business hours",
		DueDate:     businessHourTime,
	},
		ergon.WithScheduledAt(businessHourTime),
	)

	fmt.Printf("   ✓ Business hours task scheduled for %s (ID: %s)\n",
		businessHourTime.Format("Mon Jan 2, 2006 at 9:00 AM"),
		task4.ID[:8]+"...")

	// ========================================================================
	// EXAMPLE 5: View all scheduled tasks
	// ========================================================================
	fmt.Println("\n📋 All Scheduled Tasks:")

	allTasks, err := store.ListTasks(ctx, &ergon.TaskFilter{
		State: ergon.StateScheduled,
		Limit: 100,
	})

	if err != nil {
		log.Printf("Error listing tasks: %v", err)
	} else {
		for i, task := range allTasks {
			fmt.Printf("   %d. [%s] %s - Scheduled for %s\n",
				i+1,
				task.Kind,
				task.ID[:8]+"...",
				task.ScheduledAt.Format("Jan 2, 2006 3:04 PM"),
			)
		}
		fmt.Printf("\n   Total scheduled tasks: %d\n", len(allTasks))
	}

	// ========================================================================
	// Start server to process scheduled tasks
	// ========================================================================
	fmt.Println("\n🚀 Starting server to process tasks...")
	fmt.Println("   (The scheduler runs every 5 seconds to check for due tasks)")

	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:     workers,
		Concurrency: 5,
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   3,
				Priority:     1,
				PollInterval: 1 * time.Second,
			},
			"birthdays": {
				MaxWorkers:   2,
				Priority:     2,
				PollInterval: 1 * time.Second,
			},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n✅ Server ready! Scheduled tasks will execute at their appointed times.")
	fmt.Println("   Press Ctrl+C to stop")

	// Run server
	if err := server.Run(ctx); err != nil {
		log.Printf("Server error: %v", err)
	}
}

// Helper function to get next business day
func getNextBusinessDay(from time.Time) time.Time {
	next := from.Add(24 * time.Hour)

	// Skip weekends
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.Add(24 * time.Hour)
	}

	return next
}
