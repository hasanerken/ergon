package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/store/badger"
)

// EmailTaskArgs represents arguments for sending emails
type EmailTaskArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (EmailTaskArgs) Kind() string { return "send_email" }

// EmailWorker processes email tasks
type EmailWorker struct{}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailTaskArgs]) error {
	log.Printf("Sending email to %s: %s", task.Args.To, task.Args.Subject)

	// Simulate email sending
	time.Sleep(100 * time.Millisecond)

	log.Printf("Email sent to %s", task.Args.To)
	return nil
}

// APICallArgs represents arguments for making API calls
type APICallArgs struct {
	Endpoint string `json:"endpoint"`
	Method   string `json:"method"`
}

func (APICallArgs) Kind() string { return "api_call" }

// APIWorker processes API call tasks
type APIWorker struct{}

func (w *APIWorker) Work(ctx context.Context, task *ergon.Task[APICallArgs]) error {
	log.Printf("Making API call to %s (%s)", task.Args.Endpoint, task.Args.Method)

	// Simulate API call
	time.Sleep(200 * time.Millisecond)

	log.Printf("API call completed: %s", task.Args.Endpoint)
	return nil
}

func main() {
	ctx := context.Background()

	// Create store
	store, err := badger.NewStore("./data/ratelimit_demo")
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Register workers
	workers := ergon.NewWorkers()
	ergon.AddWorker(workers, &EmailWorker{})
	ergon.AddWorker(workers, &APIWorker{})

	// Create client
	client := ergon.NewClient(store, ergon.ClientConfig{
		Workers: workers,
	})

	// Create server with rate limiting enabled
	server, err := ergon.NewServer(store, ergon.ServerConfig{
		Workers:            workers,
		Concurrency:        10,
		EnableRateLimiting: true, // Enable rate limiting
		Queues: map[string]ergon.QueueConfig{
			"default": {
				MaxWorkers:   5,
				Priority:     1,
				PollInterval: 500 * time.Millisecond,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start server in background
	go func() {
		if err := server.Run(ctx); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n=== Example 1: Rate Limiting Emails ===")
	fmt.Println("Enqueueing 20 emails with max 3 concurrent")

	// Enqueue 20 email tasks with rate limit of 3 concurrent
	for i := 1; i <= 20; i++ {
		_, err := ergon.Enqueue(client, ctx, EmailTaskArgs{
			To:      fmt.Sprintf("user%d@example.com", i),
			Subject: fmt.Sprintf("Test Email %d", i),
			Body:    "This is a test email",
		},
			ergon.WithRateLimit(3),                  // Max 3 concurrent emails
			ergon.WithRateLimitScope("email-sender"), // Scope for rate limiting
		)
		if err != nil {
			log.Printf("Failed to enqueue email %d: %v", i, err)
		}
	}

	// Wait for emails to complete
	fmt.Println("Processing emails with rate limit...")
	time.Sleep(5 * time.Second)

	fmt.Println("\n=== Example 2: Rate Limiting API Calls ===")
	fmt.Println("Enqueueing 15 API calls with max 2 concurrent")

	// Enqueue 15 API call tasks with rate limit of 2 concurrent
	for i := 1; i <= 15; i++ {
		_, err := ergon.Enqueue(client, ctx, APICallArgs{
			Endpoint: fmt.Sprintf("/api/endpoint/%d", i),
			Method:   "GET",
		},
			ergon.WithRateLimit(2),                // Max 2 concurrent API calls
			ergon.WithRateLimitScope("api-client"), // Scope for rate limiting
		)
		if err != nil {
			log.Printf("Failed to enqueue API call %d: %v", i, err)
		}
	}

	// Wait for API calls to complete
	fmt.Println("Processing API calls with rate limit...")
	time.Sleep(5 * time.Second)

	fmt.Println("\n=== Example 3: Mixed Rate Limits ===")
	fmt.Println("Enqueueing tasks with different rate limit scopes")

	// These will run with independent rate limits
	for i := 1; i <= 10; i++ {
		// Email with limit of 3
		_, _ = ergon.Enqueue(client, ctx, EmailTaskArgs{
			To:      fmt.Sprintf("mixed%d@example.com", i),
			Subject: "Mixed Test",
			Body:    "Test",
		},
			ergon.WithRateLimit(3),
			ergon.WithRateLimitScope("email-sender"),
		)

		// API call with limit of 2
		_, _ = ergon.Enqueue(client, ctx, APICallArgs{
			Endpoint: fmt.Sprintf("/api/mixed/%d", i),
			Method:   "POST",
		},
			ergon.WithRateLimit(2),
			ergon.WithRateLimitScope("api-client"),
		)
	}

	fmt.Println("Processing mixed tasks with independent rate limits...")
	time.Sleep(5 * time.Second)

	fmt.Println("\n=== Rate Limiting Demo Complete ===")
	fmt.Println("Notice how tasks are throttled based on their rate limit scopes")
}
