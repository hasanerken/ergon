# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Ergon** is a distributed task queue system for Go with persistence, at-least-once delivery, and multiple storage backends (PostgreSQL and BadgerDB). It provides type-safe task definitions, worker pools, middleware support, and advanced features like task scheduling, grouping/aggregation, and uniqueness guarantees.

## Development Commands

### Running Tests
```bash
go test ./...                    # Run all tests
go test -v ./...                 # Run tests with verbose output
go test -run TestName ./...      # Run specific test
```

### Building
```bash
go build ./...                   # Build all packages
go mod tidy                      # Clean up dependencies
go mod download                  # Download dependencies
```

### Running Examples
```bash
go run examples/basic/main.go       # Basic usage example
go run examples/batch/main.go       # Batch operations example
go run examples/postgres/main.go    # PostgreSQL backend example
go run examples/statistics/main.go  # Statistics and monitoring example
```

## Architecture

### Core Components

**Manager** (`manager.go`)
- Unified facade providing access to all task management capabilities
- Combines Client, Inspector, Controller, BatchController, and Statistics
- Single entry point for most operations

**Client** (`client.go`)
- Type-safe task enqueueing with generics (`Enqueue[T TaskArgs]`)
- Supports individual tasks, batch operations, and transactional enqueues
- Validates task kinds against registered workers at enqueue time

**Server** (`server.go`)
- Processes tasks from queues using worker pools
- Manages multiple queues with configurable concurrency and priorities
- Handles task lifecycle: scheduling, retrying, error handling, recovery
- Supports middleware chain for cross-cutting concerns
- Includes scheduler for moving scheduled tasks to available state
- Optional aggregation scheduler for grouped tasks
- Optional auto-cleanup scheduler for removing old completed/failed tasks

**Workers** (`worker.go`)
- Registry for type-safe worker implementations
- Workers implement `Worker[T TaskArgs]` interface with `Work(ctx, task)` method
- Optional interfaces: `TimeoutProvider`, `RetryProvider`, `MiddlewareProvider`
- `WorkFunc[T]` adapter for simple function-based workers

### Task Lifecycle

1. **Enqueue**: Client validates and stores task (pending/scheduled/aggregating state)
2. **Schedule**: Scheduler moves scheduled tasks to available when time arrives
3. **Dequeue**: Server workers pull available tasks by queue priority
4. **Execute**: Worker processes task through middleware chain
5. **Complete/Retry/Fail**: Server transitions task to final state

**Task States**: `pending`, `scheduled`, `available`, `running`, `retrying`, `completed`, `failed`, `cancelled`, `aggregating`

### Storage Backends

**Store Interface** (`store.go`)
- Defines storage operations for tasks, queues, and groups
- Two implementations in `store/` directory:
  - `store/badger/`: Embedded key-value store (BadgerDB) - no infrastructure needed
  - `store/postgres/`: PostgreSQL-based storage with transactions and pub/sub

**Key Operations**:
- Task CRUD: `Enqueue`, `Dequeue`, `GetTask`, `ListTasks`, `UpdateTask`
- State transitions: `MarkRunning`, `MarkCompleted`, `MarkFailed`, `MarkRetrying`
- Aggregation: `AddToGroup`, `GetGroup`, `ConsumeGroup`
- Maintenance: `ArchiveTasks`, `RecoverStuckTasks`

### Management APIs

**TaskInspector** (`inspector.go`)
- Read-only task inspection and querying
- Methods: `GetTaskDetails`, `ListTasks`, `CountByState`, `SearchTasks`
- Supports filtering by queue, state, kind, time ranges, metadata

**TaskController** (`controller.go`)
- Individual task control operations
- Methods: `Cancel`, `Delete`, `RetryNow`, `Reschedule`, `UpdatePriority`, `ExtendTimeout`

**BatchController** (`batch.go`)
- Bulk operations on multiple tasks
- Methods: `CancelMany`, `DeleteByFilter`, `RetryAllFailed`, `PurgeCompleted`, `PurgeFailed`
- Cleanup helpers: `CleanupOldTasks(olderThan)` removes both completed and failed tasks

**TaskStatistics** (`statistics.go`)
- Queue metrics and analytics
- Methods: `GetQueueStats`, `GetKindStats`, `GetOverallStats`, `GetTimeSeries`
- Provides success rates, duration percentiles, throughput metrics

### Options System (`options.go`)

Functional options pattern for task configuration:

**Basic**: `WithQueue`, `WithPriority`, `WithMaxRetries`, `WithTimeout`, `WithMetadata`

**Scheduling**: `WithScheduledAt`, `WithDelay`, `WithProcessIn`

**Uniqueness**: `WithUnique(period)`, `AtMostOncePerHour`, `AtMostOncePerDay`
- Prevents duplicate tasks using content-based keys
- Configurable time windows and state filters

**Recurring**: `WithInterval`, `WithCronSchedule`, `EveryHour`, `EveryDay`
- Server automatically reschedules recurring tasks after completion

**Aggregation**: `WithGroup(key)`, `WithGroupMaxSize`, `WithGroupMaxDelay`
- Groups related tasks for batch processing
- Triggered by size threshold or time delay

**Concurrency**: `WithGlobalConcurrency`, `WithPartitionByArgs`
- Controls concurrent execution limits

### Middleware (`middleware.go`)

Chain-of-responsibility pattern for cross-cutting concerns:
- `LoggingMiddleware()`: Logs task start/completion with duration
- `RecoveryMiddleware()`: Recovers from panics, converts to errors
- `MetricsMiddleware()`: Records performance metrics (template)

Applied globally (ServerConfig) or per-worker (MiddlewareProvider interface).

### Auto-Cleanup Configuration

Server supports automatic cleanup of old completed/failed/cancelled tasks:

**ServerConfig options**:
- `EnableAutoCleanup bool`: Enable automatic cleanup (default: false)
- `CleanupInterval time.Duration`: How often to run cleanup (default: 1 hour)
- `CleanupRetention time.Duration`: Keep tasks for this duration (default: 7 days)
- `CleanupStates []TaskState`: States to clean (default: completed, failed, cancelled)
- `OnTasksCleaned func(ctx, count, states)`: Callback when cleanup runs

**Example**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers: workers,
    EnableAutoCleanup:  true,
    CleanupInterval:    1 * time.Hour,   // Clean every hour
    CleanupRetention:   7 * 24 * time.Hour, // Keep for 7 days
})
```

**Manual cleanup alternatives**:
- `mgr.Batch().PurgeCompleted(ctx, olderThan)` - Remove completed tasks
- `mgr.Batch().PurgeFailed(ctx, olderThan)` - Remove failed tasks
- `mgr.Batch().CleanupOldTasks(ctx, olderThan)` - Remove both
- `store.DeleteArchivedTasks(ctx, before)` - Direct store access (batch deletion)

### Type Safety

The library uses Go generics for compile-time type safety:

```go
// Define task arguments
type SendEmailArgs struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
}
func (SendEmailArgs) Kind() string { return "send_email" }

// Define typed worker
type EmailWorker struct {}
func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[SendEmailArgs]) error {
    // Access task.Args with full type safety
    fmt.Println(task.Args.To, task.Args.Subject)
    return nil
}

// Register and enqueue
workers := ergon.NewWorkers()
ergon.AddWorker(workers, &EmailWorker{})
ergon.Enqueue(client, ctx, SendEmailArgs{To: "user@example.com", Subject: "Hi"})
```

**Internal vs External Types**:
- `InternalTask`: Internal representation with raw `Payload []byte`
- `Task[T]`: Type-safe wrapper with `Args T` for workers
- Conversion happens automatically during worker execution

### Error Handling

**Special Errors** (`errors.go`):
- `JobCancelledError`: Cancel task mid-execution (check with `IsJobCancelled`)
- `JobSnoozedError`: Postpone task (check with `IsJobSnoozed`)
- Standard errors trigger retry logic based on RetryProvider

**Retry Logic**:
- Exponential backoff by default (configurable via `RetryDelayFunc`)
- Worker-specific retry config via `RetryProvider` interface
- Server checks `IsFailure` function to determine if error is retryable

## Key Patterns

1. **Manager Pattern**: Use `Manager` for unified access to all capabilities
2. **Type-Safe Tasks**: Always define typed `TaskArgs` and `Worker[T]` implementations
3. **Options Pattern**: Chain functional options for flexible task configuration
4. **Middleware Chain**: Add cross-cutting logic via middleware (logging, metrics, recovery)
5. **State Machine**: Tasks follow strict state transitions enforced by store implementations
6. **Worker Registry**: Centralize worker registration for validation and lookup

## Important Notes

- Task IDs use UUID v7 (time-ordered) for better database performance
- Workers must be registered in `Workers` before starting the server
- Optional worker validation at enqueue time (pass `Workers` to `ClientConfig`)
- Scheduler runs every 5 seconds to move scheduled tasks
- Aggregation scheduler interval defaults to 10 seconds
- Badger store requires local directory, Postgres requires database connection
- Server includes recovery middleware by default to prevent panics from crashing workers
