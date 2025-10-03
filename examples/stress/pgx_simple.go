package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/postgres"
)

type SimpleTask struct {
	ID int `json:"id"`
}

func (SimpleTask) Kind() string { return "simple" }

type SimpleWorker struct{}

func (w *SimpleWorker) Work(ctx context.Context, task *ergon.Task[SimpleTask]) error {
	return nil
}

func main() {
	log.SetFlags(log.Ltime)
	ctx := context.Background()

	fmt.Println("Testing PostgreSQL with native pgxpool...")

	dsn := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"
	store, err := postgres.NewStorePgx(ctx, dsn)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer store.Close()

	// Clean database
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &SimpleWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})

	fmt.Println("\n📊 Sequential Enqueue Test (1000 tasks)")
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_, err := ergon.Enqueue(client, ctx, SimpleTask{ID: i})
		if err != nil {
			log.Printf("Enqueue error: %v", err)
			break
		}
	}
	seqDuration := time.Since(start)
	seqTPS := 1000.0 / seqDuration.Seconds()
	fmt.Printf("✅ Sequential: %.0f tasks/sec (%.2f seconds)\n", seqTPS, seqDuration.Seconds())

	// Query count
	var count int
	err = store.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM queue_tasks").Scan(&count)
	if err != nil {
		log.Printf("Query error: %v", err)
	} else {
		fmt.Printf("📝 Tasks in database: %d\n", count)
	}

	fmt.Println("\n✅ Test completed successfully!")
}
