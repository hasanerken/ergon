# Ergon Comprehensive Example

This example demonstrates **ALL** major features of the Ergon task queue library in a single application.

## Features Demonstrated

### 1. **Standard Tasks** ✅
- Immediate task execution
- Basic worker implementation
- Task completion tracking

### 2. **Delayed Tasks** ⏰
- Tasks that execute after a specified delay
- Using `WithDelay()` option

### 3. **Scheduled Tasks** 📅
- Tasks scheduled for specific future time
- Using `WithScheduledAt()` option
- Automatic scheduler moves tasks when time arrives

### 4. **Recurring Tasks (Every Hour)** 🔄
- Tasks that automatically reschedule after completion
- Using `WithInterval()` option
- Simulates hourly tasks (demo uses 10s interval)

### 5. **Task Retry Logic** 🔁
- Automatic retry on failure
- Configurable retry count and delay
- Exponential backoff strategy
- Custom retry policies per worker

### 6. **Priority Queues** 🎯
- Multiple queues with different priorities
- High priority tasks processed first
- Queue-specific worker pools

### 7. **Batch Operations** 📦
- Enqueue multiple tasks at once
- Using `EnqueueMany()` for efficiency
- Batch processing tasks

### 8. **Unique Tasks** 🔒
- Prevent duplicate task execution
- Using `AtMostOncePerHour()`, `AtMostOncePerDay()`
- Duplicate detection and rejection

### 9. **Task Metadata** 🏷️
- Attach custom metadata to tasks
- Track user_id, request_id, source, etc.
- Using `WithMetadata()` option

### 10. **Task Inspection** 🔍
- Get detailed task information
- List tasks with filters
- Count tasks by state/queue

### 11. **Task Statistics** 📊
- Overall system statistics
- Per-queue statistics
- Per-task-kind statistics

### 12. **Task Control** 🎮
- Cancel pending/running tasks
- Delete completed tasks
- Batch delete operations

### 13. **Persistent Storage (Badger)** 💾
- Embedded key-value store
- No external dependencies
- Separate from your app's database
- Persistent across restarts

### 14. **Middleware** 🔌
- Logging middleware
- Recovery middleware (panic handling)
- Custom middleware support

### 15. **Graceful Shutdown** 🛑
- Clean server shutdown
- Wait for in-flight tasks
- Signal handling (Ctrl+C)

## Running the Example

```bash
cd examples/comprehensive
go run main.go
```

## Expected Output

The example will demonstrate all features in sequence:

```
🚀 Ergon Comprehensive Example - Starting...

📦 Setting up Badger store...
✅ Badger store ready at ./ergon-tasks-db

👷 Registering workers...
✅ Registered 6 workers

🔧 Configuring server...
✅ Server configured with 3 queues and middleware

▶️  Starting server...
✅ Server started and processing tasks

======================================================================
🎯 DEMONSTRATING ALL ERGON FEATURES
======================================================================

1️⃣  Standard Task - Immediate execution
   Enqueued task: 0199...
📧 [EMAIL] Sending to: user@example.com | Subject: Welcome to Ergon!
✅ [EMAIL] Sent successfully

2️⃣  Delayed Task - Executes after 3 seconds
   Enqueued delayed task: 0199... (executes at 14:23:45)

3️⃣  Scheduled Task - Executes at specific time
   Scheduled report task: 0199... (at 14:23:47)

4️⃣  Recurring Task - Every 10 seconds (simulating hourly)
   Recurring health check: 0199... (repeats every 10s)

5️⃣  Task with Retry - Will fail twice, succeed on 3rd attempt
   Sync task with retry: 0199...

6️⃣  Priority Tasks - High priority executed first
   Low priority: 0199... | High priority: 0199...

7️⃣  Batch Enqueue - Multiple tasks at once
   Enqueued 3 newsletter emails

8️⃣  Unique Task - Prevents duplicates
   Unique task: 0199...
   ✓ Duplicate rejected: task already exists

9️⃣  Task with Metadata - Custom tracking data
   Task with metadata: 0199...

======================================================================
📊 TASK INSPECTION & STATISTICS
======================================================================

📈 Overall Stats:
   Total Tasks: 15
   Completed: 12
   Failed: 0
   Pending: 3

📋 Default Queue Stats:
   Pending: 1
   Running: 0
   Completed: 8

🔍 Task Details (ID: 0199...):
   State: completed
   Kind: email
   Retried: 0 times

======================================================================
🎮 TASK CONTROL OPERATIONS
======================================================================

✅ Cancelled task: 0199...
🗑️  Deleted task: 0199...

======================================================================
⏳ Running for 15 seconds... Press Ctrl+C to stop early
======================================================================

🛑 Shutting down server gracefully...
✅ Server stopped gracefully

======================================================================
📊 FINAL STATISTICS
======================================================================
Total Tasks Processed: 16
Successful: 14
Failed: 0

✨ Comprehensive Example Completed!
💾 Database was stored at: ./ergon-tasks-db (cleaned up for demo)
```

## Task Types

The example includes 6 different task types:

1. **EmailTask** - Standard email sending
2. **ReportTask** - Scheduled report generation
3. **NotificationTask** - Delayed notifications
4. **HealthCheckTask** - Recurring health checks
5. **DataSyncTask** - Tasks with retry logic
6. **BatchProcessTask** - Batch processing with priorities

## Workers

Each task type has a corresponding worker:

- **EmailWorker** - Processes emails with 30s timeout
- **ReportWorker** - Generates reports
- **NotificationWorker** - Sends notifications
- **HealthCheckWorker** - Performs health checks
- **DataSyncWorker** - Syncs data with custom retry logic (exponential backoff)
- **BatchProcessWorker** - Processes batches

## Storage

The example uses **Badger** (embedded key-value store):

- **Location**: `./ergon-tasks-db/`
- **Type**: Persistent (survives restarts)
- **Cleanup**: Automatically deleted after demo
- **Isolation**: Completely separate from your app's database

### Using Badger in Production

```go
// Your app's cache
cacheDB, _ := badger.Open(badger.DefaultOptions("./cache"))

// Ergon's task queue - SEPARATE DATABASE
taskStore, _ := ergonbadger.NewStore("./ergon-tasks")

// No interference between the two!
```

## Queue Configuration

The example demonstrates 3 queues with different settings:

| Queue | Workers | Priority | Poll Interval |
|-------|---------|----------|---------------|
| `default` | 5 | 1 | 500ms |
| `high_priority` | 3 | 10 | 200ms |
| `low_priority` | 2 | 0 | 1s |

Higher priority queues are checked first!

## Middleware

The example uses:

1. **LoggingMiddleware** - Logs task start/completion with duration
2. **RecoveryMiddleware** - Catches panics and converts to errors

## Key Takeaways

✅ **Easy Setup** - No external dependencies with Badger
✅ **Type-Safe** - Generic workers ensure compile-time safety
✅ **Flexible Scheduling** - Delayed, scheduled, and recurring tasks
✅ **Robust** - Built-in retry logic with custom strategies
✅ **Observable** - Rich statistics and inspection APIs
✅ **Controllable** - Cancel, delete, and manage tasks
✅ **Production-Ready** - Graceful shutdown, middleware, error handling

## Next Steps

1. Explore other examples in `examples/` directory
2. Check out the main README for detailed API documentation
3. Read `CLAUDE.md` for architectural overview
4. Run the integration tests: `go test ./test/integration/...`
