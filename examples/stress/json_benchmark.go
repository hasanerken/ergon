package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/postgres"
)

type BenchTask struct {
	ID        int                    `json:"id"`
	Name      string                 `json:"name"`
	Email     string                 `json:"email"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

func (BenchTask) Kind() string { return "bench" }

type BenchWorker struct{}

func (w *BenchWorker) Work(ctx context.Context, task *ergon.Task[BenchTask]) error {
	return nil
}

func main() {
	log.SetFlags(log.Ltime)
	ctx := context.Background()

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║          JSON v1 vs JSON v2 Performance Benchmark         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Determine which JSON version is being used
	jsonVersion := "v1 (encoding/json)"
	// When built with GOEXPERIMENT=jsonv2, the jsonutil package uses v2
	fmt.Printf("🔍 JSON Implementation: %s\n", jsonVersion)
	fmt.Println()

	dsn := "postgres://postgres:ergon123@localhost:5432/ergon?sslmode=disable"

	// Test with PostgreSQL
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 PostgreSQL Performance Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	store, err := postgres.NewStorePgx(ctx, dsn)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer store.Close()

	// Enable async commit for maximum performance
	store.GetPool().Exec(ctx, "SET synchronous_commit = off")
	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	workerRegistry := ergon.NewWorkers()
	ergon.AddWorker(workerRegistry, &BenchWorker{})

	client := ergon.NewClient(store, ergon.ClientConfig{Workers: workerRegistry})

	// Benchmark 1: Sequential enqueue with small payloads
	fmt.Println("\n📝 Test 1: Sequential Enqueue (1,000 tasks, simple payload)")
	count := 1000
	start := time.Now()
	for i := 0; i < count; i++ {
		task := BenchTask{
			ID:        i,
			Name:      fmt.Sprintf("Task %d", i),
			Email:     fmt.Sprintf("user%d@example.com", i),
			Timestamp: time.Now(),
		}
		if _, err := ergon.Enqueue(client, ctx, task); err != nil {
			log.Printf("Enqueue error: %v", err)
			break
		}
	}
	duration := time.Since(start)
	tps := float64(count) / duration.Seconds()
	fmt.Printf("   ✅ %s %.0f tasks/sec (%.3f seconds)\n", getIcon(tps), tps, duration.Seconds())

	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	// Benchmark 2: Sequential enqueue with complex payloads
	fmt.Println("\n📝 Test 2: Sequential Enqueue (1,000 tasks, complex payload)")
	start = time.Now()
	for i := 0; i < count; i++ {
		task := BenchTask{
			ID:        i,
			Name:      fmt.Sprintf("Task %d", i),
			Email:     fmt.Sprintf("user%d@example.com", i),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"user_id":    i,
				"department": "Engineering",
				"role":       "Developer",
				"metadata": map[string]interface{}{
					"projects":    []string{"project-a", "project-b", "project-c"},
					"skills":      []string{"Go", "PostgreSQL", "Docker", "Kubernetes"},
					"experience":  5,
					"remote":      true,
					"permissions": []string{"read", "write", "admin"},
				},
				"preferences": map[string]interface{}{
					"theme":         "dark",
					"language":      "en",
					"timezone":      "UTC",
					"notifications": true,
				},
			},
		}
		if _, err := ergon.Enqueue(client, ctx, task); err != nil {
			log.Printf("Enqueue error: %v", err)
			break
		}
	}
	duration = time.Since(start)
	tps = float64(count) / duration.Seconds()
	fmt.Printf("   ✅ %s %.0f tasks/sec (%.3f seconds)\n", getIcon(tps), tps, duration.Seconds())

	store.GetPool().Exec(ctx, "TRUNCATE queue_tasks CASCADE")

	// Benchmark 3: Batch operations
	fmt.Println("\n📝 Test 3: Batch Enqueue (10,000 tasks)")
	batchCount := 10000
	tasks := make([]BenchTask, batchCount)
	for i := 0; i < batchCount; i++ {
		tasks[i] = BenchTask{
			ID:        i,
			Name:      fmt.Sprintf("Task %d", i),
			Email:     fmt.Sprintf("user%d@example.com", i),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"value": i,
			},
		}
	}

	start = time.Now()
	if _, err := ergon.EnqueueMany(client, ctx, tasks); err != nil {
		log.Printf("Batch enqueue error: %v", err)
	}
	duration = time.Since(start)
	tps = float64(batchCount) / duration.Seconds()
	fmt.Printf("   ✅ %s %.0f tasks/sec (%.3f seconds)\n", getIcon(tps), tps, duration.Seconds())

	fmt.Println("\n✅ Benchmark completed!")
	fmt.Println("\n💡 To test JSON v2, rebuild with: GOEXPERIMENT=jsonv2 go run json_benchmark.go")
}

func getIcon(tps float64) string {
	if tps > 40000 {
		return "🚀"
	} else if tps > 20000 {
		return "⚡"
	} else if tps > 10000 {
		return "🔥"
	} else if tps > 5000 {
		return "✨"
	}
	return "📊"
}
