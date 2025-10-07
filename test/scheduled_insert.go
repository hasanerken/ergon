//go:build ignore

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/postgres"
)

type TestArgs struct {
	Message string `json:"message"`
}

func (TestArgs) Kind() string { return "test" }

func main() {
	ctx := context.Background()
	connStr := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"

	// Open database
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create store
	store, err := postgres.NewStore(db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Create client (without workers - just for testing insert)
	client := ergon.NewClient(store, ergon.ClientConfig{})

	// Schedule task for 2 minutes from now
	now := time.Now()
	targetTime := now.Add(2 * time.Minute)

	fmt.Printf("Current time: %s\n", now.Format("15:04:05"))
	fmt.Printf("Target time:  %s\n", targetTime.Format("15:04:05"))
	fmt.Printf("Target > Now: %v\n", targetTime.After(now))

	task, err := ergon.Enqueue(client, ctx, TestArgs{
		Message: "Test scheduled task",
	}, ergon.WithScheduledAt(targetTime))

	if err != nil {
		log.Fatalf("Error enqueueing: %v", err)
	}

	fmt.Printf("\nTask created:\n")
	fmt.Printf("  ID: %s\n", task.ID)
	fmt.Printf("  State: %s\n", task.State)
	fmt.Printf("  ScheduledAt in struct: %v\n", task.ScheduledAt)

	// Query database immediately
	var dbState string
	var dbScheduledAt *time.Time
	err = db.QueryRowContext(ctx, "SELECT state, scheduled_at FROM queue_tasks WHERE id = $1", task.ID).Scan(&dbState, &dbScheduledAt)
	if err != nil {
		log.Fatalf("Error querying: %v", err)
	}

	fmt.Printf("\nDatabase values:\n")
	fmt.Printf("  State: %s\n", dbState)
	if dbScheduledAt != nil {
		fmt.Printf("  ScheduledAt: %s\n", dbScheduledAt.Format("15:04:05"))
	} else {
		fmt.Printf("  ScheduledAt: NULL ❌\n")
	}
}
