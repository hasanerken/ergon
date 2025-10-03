# Quick Start Guide

## Run the Comprehensive Example

```bash
cd examples/comprehensive
go run main.go
```

The example will run for 15 seconds demonstrating all features, or press `Ctrl+C` to stop early.

## What You'll See

The example demonstrates all Ergon features in action:

### 🚀 Immediate Features
- **Standard tasks** executing right away
- **Batch enqueue** of multiple emails at once
- **Priority queues** - high priority tasks jump the line
- **Unique tasks** - duplicates are rejected

### ⏰ Time-Based Features  
- **Delayed tasks** - execute after 3 seconds
- **Scheduled tasks** - execute at specific time (5 seconds)
- **Recurring tasks** - health check every 10 seconds

### 🔁 Resilience Features
- **Retry logic** - tasks fail 2 times, succeed on 3rd attempt
- **Exponential backoff** - 2s, 4s, 8s retry delays
- **Graceful shutdown** - waits for in-flight tasks

### 📊 Management Features
- **Task inspection** - get detailed task info
- **Statistics** - overall, per-queue, per-kind
- **Task control** - cancel and delete tasks
- **Metadata** - attach custom tracking data

### 💾 Storage
- **Badger database** - persistent, embedded
- **Location**: `./ergon-tasks-db/`
- **Separate from your app** - no interference

## Code Structure

```go
// 1. Setup store
store, _ := badger.NewStore("./ergon-tasks-db")

// 2. Register workers
workers := ergon.NewWorkers()
ergon.AddWorker(workers, &EmailWorker{})

// 3. Create client
client := ergon.NewClient(store, ergon.ClientConfig{
    Workers: workers,
})

// 4. Create server
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers: workers,
    Queues: map[string]ergon.QueueConfig{
        "default": {
            MaxWorkers: 5,
            PollInterval: 500 * time.Millisecond,
        },
    },
})

// 5. Start server
go server.Start(ctx)

// 6. Enqueue tasks
ergon.Enqueue(client, ctx, EmailTask{
    To: "user@example.com",
    Subject: "Hello",
})

// 7. Use manager for inspection/control
manager := ergon.NewManager(store, ergon.ClientConfig{})
stats, _ := manager.Stats().GetOverallStats(ctx)
```

## Key Points

✨ **No external dependencies** - Badger is embedded
🔒 **Type-safe** - Compile-time task validation
⚡ **High performance** - Concurrent worker pools
📈 **Observable** - Rich statistics API
🎮 **Controllable** - Cancel, delete, reschedule
🛡️ **Robust** - Retry logic, panic recovery

## Next Steps

1. **Modify the example** - change delays, add your own tasks
2. **Check statistics** - see task counts and durations
3. **Try PostgreSQL** - run `examples/postgres/main.go`
4. **Read the docs** - see main README.md and CLAUDE.md
