# Design Patterns Implementation in Ergon

This document describes the design patterns implemented in Ergon task queue library.

## Implemented Patterns

### Structural Patterns

#### 1. Option Pattern (Functional Options) ⭐ EXCELLENT
**Location**: `options.go`

The functional options pattern provides a flexible, extensible way to configure tasks.

**Implementation**:
- 60+ option functions (`WithQueue`, `WithTimeout`, `WithRateLimit`, etc.)
- Composable options that can be combined
- Type-safe with validation
- Self-documenting API

**Example**:
```go
task, _ := ergon.Enqueue(client, ctx, SendEmailArgs{...},
    ergon.WithQueue("emails"),
    ergon.WithRateLimit(100),
    ergon.WithTimeout(5*time.Minute),
    ergon.AtMostOncePerHour(),
)
```

#### 2. Strategy Pattern (Worker Registry) ⭐ EXCELLENT
**Location**: `worker.go`

Different task types are processed by different worker strategies selected at runtime.

**Implementation**:
- `Worker[T]` interface for type-safe task processing
- Runtime worker selection via registry
- Optional interfaces for customization (`TimeoutProvider`, `RetryProvider`)

**Example**:
```go
type EmailWorker struct{}

func (w *EmailWorker) Work(ctx context.Context, task *ergon.Task[EmailTaskArgs]) error {
    // Process email
    return nil
}

workers := ergon.NewWorkers()
ergon.AddWorker(workers, &EmailWorker{})
```

#### 3. Adapter Pattern (Storage Backends) ⭐ EXCELLENT
**Location**: `store.go`, `store/postgres/`, `store/badger/`

The Store interface abstracts different storage backends (PostgreSQL, BadgerDB).

**Implementation**:
- Clean `Store` interface
- Multiple implementations with different characteristics
- Users can bring their own storage

**Example**:
```go
// Use BadgerDB
store, _ := badger.NewStore("./data")

// Or use PostgreSQL
store, _ := postgres.NewStoreFromDSN("postgres://...")
```

#### 4. Decorator Pattern (Middleware) ✅ GOOD
**Location**: `middleware.go`, `server.go`

Middleware wraps task execution to add cross-cutting concerns.

**Implementation**:
- Chain of responsibility pattern
- Composable middleware functions
- Global and per-worker middleware

**Example**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Middleware: []ergon.MiddlewareFunc{
        ergon.LoggingMiddleware(),
        ergon.RecoveryMiddleware(),
        CustomMetricsMiddleware(),
    },
})
```

### Concurrency Patterns

#### 5. Worker Pool ⭐ EXCELLENT
**Location**: `server.go`

Fixed pool of workers process tasks from queues with controlled concurrency.

**Implementation**:
- Fixed number of worker goroutines
- Queue-based job distribution
- Graceful shutdown with sync.WaitGroup
- Backpressure through queue capacity

**Example**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Concurrency: 10,
    Queues: map[string]ergon.QueueConfig{
        "default": {MaxWorkers: 5},
        "emails":  {MaxWorkers: 3},
    },
})
```

#### 6. Timeout Pattern ✅ IMPLEMENTED
**Location**: `server.go:407-423`

Tasks are executed with context deadlines to prevent hanging.

**Implementation**:
- Context with timeout created for each task
- Configurable per-task, per-worker, or globally
- Automatic cancellation on timeout

**Example**:
```go
task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithTimeout(30*time.Second),
)
```

#### 7. Retry with Exponential Backoff ✅ GOOD
**Location**: `server.go:559-607`

Failed tasks automatically retry with increasing delays.

**Implementation**:
- Configurable max retries
- Exponential backoff by default
- Custom retry logic per worker
- Special error types (JobCancel, JobSnooze)

**Example**:
```go
task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithMaxRetries(5),
)
```

## New Features (2025-10-06)

### 8. Rate Limiting Pattern ⭐ NEW
**Location**: `internal/ratelimit/`, `server.go:412-425`

Enforces maximum concurrent task execution per scope.

**Implementation**:
- Token bucket algorithm
- Per-scope rate limiting
- Automatic task requeue when rate limited
- Thread-safe concurrent access

**Example**:
```go
// Enable rate limiting on server
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    EnableRateLimiting: true,
})

// Enqueue with rate limit
task, _ := ergon.Enqueue(client, ctx, SendEmailArgs{...},
    ergon.WithRateLimit(100),                  // Max 100 concurrent
    ergon.WithRateLimitScope("email-sender"),  // Scope
)
```

**Use Cases**:
- Prevent overwhelming external APIs (max 10 API calls/sec)
- Control email sending rate (max 100 concurrent emails)
- Limit database connections per task type
- Comply with third-party rate limits

**Demo**: See `examples/ratelimit/main.go`

### 9. Event Callbacks (Observer Pattern) ⭐ NEW
**Location**: `server.go:62-65,434-436,490-492,605-616`

Hooks for monitoring and observability at task lifecycle points.

**Implementation**:
- Simple callback functions (not complex pub/sub)
- Called at key lifecycle events
- Context propagation
- Non-blocking

**Example**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
        log.Printf("Task started: %s", task.ID)
    },

    OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
        metrics.RecordDuration(task.Kind, duration)
    },

    OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
        alerts.Send("Task failed: " + task.ID)
    },

    OnTaskRetried: func(ctx context.Context, task *ergon.InternalTask, attempt int, nextRetry time.Time) {
        log.Printf("Task %s retrying (attempt %d)", task.ID, attempt)
    },
})
```

**Use Cases**:
- Send metrics to Datadog, Prometheus, CloudWatch
- Trigger alerts on critical failures
- Track SLA compliance
- Build custom dashboards
- Audit logging
- Custom retry strategies

**Demo**: See `examples/callbacks/main.go`

## Testing

All new features include comprehensive tests:

**Rate Limiter Tests**: `internal/ratelimit/limiter_test.go`
- Basic functionality (allow/deny)
- Concurrent access safety
- Scope isolation
- Release and reset operations
- Statistics tracking
- Dynamic limit changes

**Run Tests**:
```bash
# Rate limiter tests
go test ./internal/ratelimit/... -v

# All tests
go test ./... -v
```

## Pattern Decision Matrix

| Pattern | Status | Implemented | Priority | Notes |
|---------|--------|-------------|----------|-------|
| Option Pattern | ⭐⭐⭐⭐⭐ | Yes | Core | Perfect for library API |
| Strategy (Workers) | ⭐⭐⭐⭐⭐ | Yes | Core | Type-safe with generics |
| Adapter (Stores) | ⭐⭐⭐⭐⭐ | Yes | Core | Essential for flexibility |
| Decorator (Middleware) | ⭐⭐⭐⭐ | Yes | Core | Cross-cutting concerns |
| Worker Pool | ⭐⭐⭐⭐⭐ | Yes | Core | Controlled concurrency |
| Timeout | ⭐⭐⭐⭐ | Yes | Core | Prevent hanging tasks |
| Retry/Backoff | ⭐⭐⭐⭐ | Yes | Core | Handle transient failures |
| Rate Limiting | ⭐⭐⭐⭐ | **NEW** | High | External API protection |
| Observer (Callbacks) | ⭐⭐⭐ | **NEW** | Medium | Monitoring hooks |
| Circuit Breaker | ❌ | No | Low | Let users implement |
| Pipeline | ❌ | No | Low | Users can chain tasks |
| Fan-Out/Fan-In | ❌ | No | Low | Application-level |

## Architecture Philosophy

Ergon follows these design principles:

1. **Type Safety**: Leverage Go generics for compile-time safety
2. **Composability**: Options, middleware, and workers are composable
3. **Flexibility**: Multiple storage backends, extensible workers
4. **Production-Ready**: Rate limiting, retries, timeouts, monitoring
5. **Pragmatic**: Only implement patterns that add real value
6. **Library-First**: Clean API for importing into other projects

## Performance Considerations

**Rate Limiting**:
- O(1) allow/release operations
- Lock-free for different scopes
- Minimal memory overhead per scope

**Event Callbacks**:
- Direct function calls (no goroutines unless user spawns)
- Zero overhead if callbacks not configured
- Context propagation for cancellation

## Migration Guide

### Adding Rate Limiting to Existing Code

**Before**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers: workers,
})
```

**After**:
```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers:            workers,
    EnableRateLimiting: true, // Enable feature
})

// Enqueue with rate limit
ergon.Enqueue(client, ctx, args,
    ergon.WithRateLimit(100),
    ergon.WithRateLimitScope("api-calls"),
)
```

### Adding Event Callbacks

```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers: workers,

    // Add callbacks for monitoring
    OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
        metrics.RecordDuration(task.Kind, duration)
    },

    OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
        if task.Queue == "critical" {
            alerts.Send("Critical failure: " + task.ID)
        }
    },
})
```

## Monitoring & Web UI

### Built-in Web Dashboard ⭐ PRODUCTION-READY
**Location**: `internal/jsonutil/monitor/`

Ergon includes a complete web-based monitoring interface out of the box.

**Technology Stack**:
- Backend: Go HTTP server with embedded templates
- Frontend: HTMX (no JavaScript framework needed) + Tailwind CSS
- Real-time: Auto-refresh with HTMX polling (5s intervals)

**Features**:
- 📊 **Dashboard**: Real-time stats (total, pending, running, completed, failed, success rate)
- 📋 **Task List**: Filter by queue/kind/state, pagination, clickable task details
- 🔍 **Task Details**: Full JSON payload, metadata, timeline, error messages
- 🎯 **Queue Overview**: Per-queue metrics and status
- ⚡ **Actions**: Cancel, retry, delete, reschedule tasks from UI
- 🎨 **Color-coded states**: Visual task state identification

**Quick Start**:
```go
import "github.com/hasanerken/ergon/internal/jsonutil/monitor"

monitorUI, _ := monitor.NewServer(manager, monitor.Config{
    Addr: ":8888",
    BasePath: "/monitor",
})
go monitorUI.Start()
// Open http://localhost:8888/monitor
```

**Demo**: `examples/monitor/main.go`

### Monitoring Integration

**Three-Layer Approach**:

1. **Web UI** - For human operators
   - Visual debugging
   - Interactive task management
   - Quick troubleshooting

2. **Event Callbacks** - For production metrics
   - Datadog, Prometheus, CloudWatch
   - Custom alerting (PagerDuty, Slack)
   - SLA tracking

3. **Statistics API** - For custom integrations
   - Build domain-specific dashboards
   - Integrate with existing admin panels
   - Programmatic monitoring

**Example: Combined Monitoring**:
```go
// 1. Web UI for operators
monitorUI, _ := monitor.NewServer(manager, monitor.Config{Addr: ":8888"})
go monitorUI.Start()

// 2. Event callbacks for metrics
server := ergon.NewServer(store, ergon.ServerConfig{
    OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
        statsd.Histogram("task.duration", duration.Seconds(),
            []string{"kind:" + task.Kind}, 1)
    },
    OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
        sentry.CaptureException(err)
        if task.Queue == "critical" {
            pagerduty.Alert("Critical task failed: " + task.ID)
        }
    },
})

// 3. Statistics API for custom dashboard
http.HandleFunc("/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
    stats, _ := manager.GetOverallStats(r.Context())
    json.NewEncoder(w).Encode(stats)
})
```

**Complete Guide**: See `MONITORING_GUIDE.md` for detailed examples

---

## References

- **Go Design Patterns**: Internal reference guide
- **Functional Options**: Rob Pike's pattern
- **Worker Pool**: Go concurrency pattern
- **Token Bucket**: Rate limiting algorithm
- **Observer Pattern**: Gang of Four design patterns
- **HTMX**: Hypermedia-driven UI updates
