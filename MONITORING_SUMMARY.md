# Ergon Monitoring - Quick Reference

## 🎯 Three Ways to Monitor Ergon

### 1️⃣ **Web UI Dashboard** (Built-in, Zero Config)

```go
import "github.com/hasanerken/ergon/internal/jsonutil/monitor"

monitorUI, _ := monitor.NewServer(manager, monitor.Config{
    Addr: ":8888",
})
go monitorUI.Start()
```

**Access**: `http://localhost:8888/monitor`

**What You Get**:
- ✅ Real-time dashboard with statistics
- ✅ Task list with filters (queue, kind, state)
- ✅ Task details (JSON payload, metadata, errors)
- ✅ Queue overview
- ✅ Task actions (cancel, retry, delete)
- ✅ Auto-refresh every 5 seconds

**When to Use**: Development, debugging, manual task management

---

### 2️⃣ **Event Callbacks** (Custom Hooks) - NEW! ⭐

```go
server := ergon.NewServer(store, ergon.ServerConfig{
    OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
        // Send to Datadog
        statsd.Histogram("task.duration", duration.Seconds())
    },

    OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
        // Alert on critical failures
        if task.Queue == "critical" {
            pagerduty.Alert("Task failed: " + task.ID)
        }
    },
})
```

**Callbacks Available**:
- `OnTaskStarted` - Task execution begins
- `OnTaskCompleted` - Task succeeds (with duration)
- `OnTaskFailed` - Task fails permanently
- `OnTaskRetried` - Task is retried

**When to Use**: Production metrics, alerting, SLA tracking

---

### 3️⃣ **Statistics API** (Programmatic)

```go
// Get overall stats
stats, _ := manager.GetOverallStats(ctx)
// stats.TotalTasks, stats.PendingTasks, stats.SuccessRate, etc.

// List queues with metrics
queues, _ := manager.ListQueues(ctx)

// Filter tasks
tasks, _ := manager.ListTasks(ctx, &ergon.TaskFilter{
    Queue: "emails",
    State: ergon.StateFailed,
    Limit: 100,
})

// Count tasks
count, _ := manager.CountTasks(ctx, &ergon.TaskFilter{
    State: ergon.StateRunning,
})
```

**When to Use**: Custom dashboards, admin panels, integrations

---

## 🔥 Production Setup (All Three Combined)

```go
package main

import (
    "github.com/hasanerken/ergon"
    "github.com/hasanerken/ergon/internal/jsonutil/monitor"
)

func main() {
    // 1. Web UI for operators
    monitorUI, _ := monitor.NewServer(manager, monitor.Config{
        Addr: ":8888",
    })
    go monitorUI.Start()
    log.Println("Monitor UI: http://localhost:8888/monitor")

    // 2. Event callbacks for metrics & alerting
    server, _ := ergon.NewServer(store, ergon.ServerConfig{
        Workers: workers,
        EnableRateLimiting: true, // NEW: Rate limiting

        // Metrics
        OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
            // Datadog
            statsd.Histogram("ergon.task.duration", duration.Seconds(),
                []string{"kind:" + task.Kind, "queue:" + task.Queue}, 1)

            // Prometheus
            taskDuration.WithLabelValues(task.Queue, task.Kind).Observe(duration.Seconds())
        },

        // Alerting
        OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
            // Sentry for error tracking
            sentry.CaptureException(err, map[string]string{
                "task_id": task.ID,
                "kind": task.Kind,
            })

            // PagerDuty for critical queues
            if task.Queue == "critical" {
                pagerduty.Alert("Critical task failed", task.ID, err.Error())
            }

            // Slack for team notifications
            slack.Send("#task-failures", fmt.Sprintf(
                "Task failed: %s (kind=%s, error=%v)",
                task.ID, task.Kind, err,
            ))
        },

        // SLA tracking
        OnTaskRetried: func(ctx context.Context, task *ergon.InternalTask, attempt int, nextRetry time.Time) {
            if attempt >= task.MaxRetries/2 {
                log.Printf("⚠️  Task %s failing repeatedly (attempt %d)", task.ID, attempt)
            }
        },
    })

    // 3. Statistics API for custom dashboard
    http.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
        stats, _ := manager.GetOverallStats(r.Context())
        json.NewEncoder(w).Encode(stats)
    })

    server.Run(ctx)
}
```

---

## 📊 Integration Examples

### Datadog

```go
OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    tags := []string{"queue:" + task.Queue, "kind:" + task.Kind}
    statsd.Histogram("ergon.task.duration", duration.Seconds(), tags, 1)
    statsd.Incr("ergon.task.completed", tags, 1)
},
```

### Prometheus

```go
var taskDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "ergon_task_duration_seconds",
    },
    []string{"queue", "kind"},
)

OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    taskDuration.WithLabelValues(task.Queue, task.Kind).Observe(duration.Seconds())
},
```

### Sentry

```go
OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
    sentry.CaptureException(err, map[string]string{
        "task_id": task.ID,
        "kind": task.Kind,
        "queue": task.Queue,
    })
},
```

---

## 🎨 Web UI Screenshots (What You'll See)

### Dashboard
```
┌───────────────────────────────────────────────────┐
│  Dashboard                                         │
├───────────────────────────────────────────────────┤
│  Total: 1,234  Pending: 45   Running: 12         │
│  Completed: 1,150   Failed: 27   Rate: 97.8%     │
│                                                    │
│  Queues Overview                                   │
│  ┌────────────────────────────────────────────┐  │
│  │ default    Active   P:45  R:12  C:1k  F:10 │  │
│  │ emails     Active   P:20  R:5   C:500 F:5  │  │
│  │ critical   Active   P:0   R:3   C:100 F:2  │  │
│  └────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────┘
```

### Task List
```
┌───────────────────────────────────────────────────┐
│  Tasks   [Filters: Queue: All ▾  Kind: All ▾]    │
├───────────────────────────────────────────────────┤
│  Pending: 45  Running: 12  Completed: 1k  ...    │
├───────────────────────────────────────────────────┤
│  ID        Kind         Queue    State    Actions │
│  abc123    send_email   emails   Running  [View]  │
│  def456    process_data default  Pending  [View]  │
│  ghi789    send_sms     sms      Failed   [Retry] │
└───────────────────────────────────────────────────┘
```

---

## 🚀 Quick Start Guide

### Step 1: Add Monitoring to Your App

```bash
# 1. Import monitor package
import "github.com/hasanerken/ergon/internal/jsonutil/monitor"
```

### Step 2: Start Web UI

```go
monitorUI, _ := monitor.NewServer(manager, monitor.Config{
    Addr:     ":8888",
    BasePath: "/monitor",
})
go monitorUI.Start()
```

### Step 3: Add Event Callbacks (Optional)

```go
server := ergon.NewServer(store, ergon.ServerConfig{
    OnTaskCompleted: yourMetricsFunction,
    OnTaskFailed: yourAlertFunction,
})
```

### Step 4: Access Dashboard

```bash
# Open browser
open http://localhost:8888/monitor
```

---

## 📚 Documentation

- **Full Guide**: `MONITORING_GUIDE.md` - Complete monitoring documentation
- **Patterns**: `DESIGN_PATTERNS.md` - Design patterns including monitoring
- **Example**: `examples/monitor/main.go` - Working example
- **Callbacks Example**: `examples/callbacks/main.go` - Event callbacks demo

---

## ✅ Summary

| Feature | Use Case | Setup Time |
|---------|----------|------------|
| **Web UI** | Human monitoring, debugging | 2 lines of code |
| **Event Callbacks** | Production metrics, alerting | Add callbacks to ServerConfig |
| **Statistics API** | Custom dashboards | Use manager methods |

**Best Practice**: Use all three together for complete observability! 🎯
