# Ergon Monitoring & Web UI Guide

Complete guide to monitoring and observability features in Ergon task queue library.

## Table of Contents

1. [Overview](#overview)
2. [Web UI Dashboard](#web-ui-dashboard)
3. [Event Callbacks](#event-callbacks)
4. [Statistics API](#statistics-api)
5. [Integration Examples](#integration-examples)
6. [Architecture](#architecture)

---

## Overview

Ergon provides **three layers of monitoring**:

### 1. **Web-Based Dashboard** (Built-in UI)
- Real-time task monitoring
- Interactive dashboards
- Task management (cancel, retry, delete)
- Queue performance metrics
- **Technology**: Go templates + HTMX + Tailwind CSS

### 2. **Event Callbacks** (NEW - Custom Hooks)
- Lifecycle event hooks
- Integration with metrics systems (Datadog, Prometheus, CloudWatch)
- Custom alerting
- Audit logging

### 3. **Statistics API** (Programmatic Access)
- Real-time metrics
- Queue statistics
- Task kind analytics
- Success rates and duration percentiles

---

## Web UI Dashboard

### Quick Start

```go
package main

import (
    "github.com/hasanerken/ergon"
    "github.com/hasanerken/ergon/internal/jsonutil/monitor"
    "github.com/hasanerken/ergon/store/badger"
)

func main() {
    ctx := context.Background()

    // 1. Create store
    store, _ := badger.NewStore("./data")
    defer store.Close()

    // 2. Create manager
    manager := ergon.NewManager(store, ergon.ClientConfig{})

    // 3. Create and start monitor web UI
    monitorServer, _ := monitor.NewServer(manager, monitor.Config{
        Addr:     ":8888",              // Listen address
        BasePath: "/monitor",            // URL path prefix
    })

    // 4. Start monitor in background
    go func() {
        log.Println("Monitor UI: http://localhost:8888/monitor")
        monitorServer.Start()
    }()
    defer monitorServer.Stop()

    // Your application continues...
}
```

### Dashboard Features

#### 📊 **Main Dashboard** (`/monitor/dashboard`)

**Real-Time Statistics Cards**:
- Total Tasks
- Pending Tasks
- Running Tasks
- Completed Tasks
- Failed Tasks
- Success Rate (%)

**Queues Overview**:
- Queue status (Active/Paused)
- Per-queue task counts
- Click to filter tasks by queue

**Auto-Refresh**: Updates every 5 seconds with HTMX polling

#### 📋 **Task List** (`/monitor/tasks`)

**Features**:
- **State Boxes**: Quick counts for all states (pending, running, completed, failed, etc.)
- **Filters**:
  - By Queue
  - By Task Kind
  - By State
- **Pagination**: 50 tasks per page
- **Auto-refresh**: Toggle on/off

**Task Information Displayed**:
- Task ID (clickable for details)
- Kind (task type)
- Queue name
- State (color-coded badges)
- Priority
- Retry count
- Created/Started/Completed times
- Error messages (if failed)

**Actions Per Task**:
- View Details (modal popup)
- Cancel Task
- Retry Task
- Delete Task
- Reschedule Task

#### 🔍 **Task Details Modal**

Click any task to see:
- Complete task information
- Full JSON payload (formatted)
- Metadata
- Error details
- Timeline (enqueued → started → completed)
- Action buttons

#### 🎯 **Queues Page** (`/monitor/queues`)

**Queue Statistics**:
- Pending count
- Running count
- Completed count
- Failed count
- Queue status (active/paused)

### URL Routes

| Route | Description |
|-------|-------------|
| `/monitor/` | Main dashboard |
| `/monitor/dashboard` | Dashboard (same as above) |
| `/monitor/tasks` | Task list with filters |
| `/monitor/tasks/:id` | Task detail modal |
| `/monitor/queues` | Queue overview |
| `/monitor/api/stats` | JSON stats endpoint |
| `/monitor/api/tasks` | JSON tasks endpoint |
| `/monitor/api/tasks/cancel` | Cancel task action |
| `/monitor/api/tasks/retry` | Retry task action |
| `/monitor/api/tasks/delete` | Delete task action |

### UI Technology Stack

- **Backend**: Go HTTP server with embedded templates
- **Templates**: Go `html/template` with custom functions
- **Frontend**:
  - **HTMX**: For dynamic updates without JavaScript
  - **Tailwind CSS**: For styling
  - **Iconify**: For icons
- **Auto-refresh**: HTMX polling every 5 seconds

### Color-Coded States

```go
// State color badges
pending    -> Blue   (bg-blue-100 text-blue-800)
scheduled  -> Purple (bg-purple-100 text-purple-800)
running    -> Yellow (bg-yellow-100 text-yellow-800)
completed  -> Green  (bg-green-100 text-green-800)
failed     -> Red    (bg-red-100 text-red-800)
retrying   -> Orange (bg-orange-100 text-orange-800)
cancelled  -> Gray   (bg-gray-100 text-gray-800)
```

---

## Event Callbacks

### Overview

Event callbacks provide hooks into the task lifecycle for custom monitoring, metrics, and alerting.

### Available Callbacks

```go
type ServerConfig struct {
    // Lifecycle event callbacks
    OnTaskStarted   func(ctx context.Context, task *InternalTask)
    OnTaskCompleted func(ctx context.Context, task *InternalTask, duration time.Duration)
    OnTaskFailed    func(ctx context.Context, task *InternalTask, err error)
    OnTaskRetried   func(ctx context.Context, task *InternalTask, attempt int, nextRetry time.Time)
}
```

### Usage Example

```go
server, _ := ergon.NewServer(store, ergon.ServerConfig{
    Workers: workers,

    // Track when tasks start
    OnTaskStarted: func(ctx context.Context, task *ergon.InternalTask) {
        log.Printf("Task started: %s (kind=%s)", task.ID, task.Kind)
        // Send to APM
        apm.StartSpan(task.ID, task.Kind)
    },

    // Track successful completions
    OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
        log.Printf("Task completed: %s in %v", task.ID, duration)

        // Send metrics to Datadog
        statsd.Histogram("task.duration", duration.Seconds(),
            []string{"kind:" + task.Kind, "queue:" + task.Queue}, 1)
        statsd.Incr("task.completed",
            []string{"kind:" + task.Kind}, 1)
    },

    // Track failures and alert
    OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
        log.Printf("Task failed: %s - %v", task.ID, err)

        // Alert on critical failures
        if task.Queue == "critical" {
            pagerduty.Alert("Critical task failed", task.ID, err.Error())
        }

        // Send to error tracking
        sentry.CaptureException(err, map[string]string{
            "task_id": task.ID,
            "kind": task.Kind,
            "queue": task.Queue,
        })
    },

    // Track retry patterns
    OnTaskRetried: func(ctx context.Context, task *ergon.InternalTask, attempt int, nextRetry time.Time) {
        log.Printf("Task retrying: %s (attempt %d/%d)",
            task.ID, attempt, task.MaxRetries)

        // Warn if task is repeatedly failing
        if attempt >= task.MaxRetries/2 {
            log.Printf("⚠️  Task %s is failing repeatedly (attempt %d)",
                task.ID, attempt)
        }
    },
})
```

### Integration Examples

#### 🔹 **Datadog Metrics**

```go
OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    tags := []string{
        "queue:" + task.Queue,
        "kind:" + task.Kind,
        "state:completed",
    }

    statsd.Histogram("ergon.task.duration", duration.Seconds(), tags, 1)
    statsd.Incr("ergon.task.completed", tags, 1)
},

OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
    tags := []string{
        "queue:" + task.Queue,
        "kind:" + task.Kind,
        "state:failed",
    }

    statsd.Incr("ergon.task.failed", tags, 1)
},
```

#### 🔹 **Prometheus Metrics**

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    taskDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "ergon_task_duration_seconds",
            Help: "Task execution duration",
        },
        []string{"queue", "kind"},
    )

    taskTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "ergon_tasks_total",
            Help: "Total tasks processed",
        },
        []string{"queue", "kind", "status"},
    )
)

// In server config
OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    taskDuration.WithLabelValues(task.Queue, task.Kind).Observe(duration.Seconds())
    taskTotal.WithLabelValues(task.Queue, task.Kind, "completed").Inc()
},
```

#### 🔹 **CloudWatch Metrics**

```go
import "github.com/aws/aws-sdk-go/service/cloudwatch"

OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    cw.PutMetricData(&cloudwatch.PutMetricDataInput{
        Namespace: aws.String("Ergon"),
        MetricData: []*cloudwatch.MetricDatum{
            {
                MetricName: aws.String("TaskDuration"),
                Value: aws.Float64(duration.Seconds()),
                Unit: aws.String("Seconds"),
                Dimensions: []*cloudwatch.Dimension{
                    {Name: aws.String("Queue"), Value: aws.String(task.Queue)},
                    {Name: aws.String("Kind"), Value: aws.String(task.Kind)},
                },
            },
        },
    })
},
```

#### 🔹 **Custom Alerting**

```go
OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
    // Alert logic
    if task.Queue == "critical" {
        slack.Send("#alerts", fmt.Sprintf(
            "🚨 Critical task failed: %s\nKind: %s\nError: %v",
            task.ID, task.Kind, err,
        ))
    }

    // Track failure rate
    failureRate := getFailureRate(task.Kind)
    if failureRate > 0.5 { // 50% failure rate
        pagerduty.Alert("High failure rate for " + task.Kind)
    }
},
```

---

## Statistics API

### Programmatic Access

Ergon provides a statistics API for building custom dashboards or monitoring.

#### Overall Statistics

```go
stats, err := manager.GetOverallStats(ctx)
// Returns: *OverallStatistics

type OverallStatistics struct {
    TotalTasks     int     // Total tasks in system
    PendingTasks   int     // Waiting to run
    RunningTasks   int     // Currently executing
    CompletedTasks int     // Successfully completed
    FailedTasks    int     // Permanently failed
    SuccessRate    float64 // Percentage (0-100)
}
```

#### Queue Statistics

```go
queues, err := manager.ListQueues(ctx)
// Returns: []*QueueInfo

type QueueInfo struct {
    Name           string
    Paused         bool
    PendingCount   int
    RunningCount   int
    ScheduledCount int
    CompletedCount int
    FailedCount    int
}
```

#### Task Filtering

```go
// Filter tasks
tasks, err := manager.ListTasks(ctx, &ergon.TaskFilter{
    Queue:  "emails",
    State:  ergon.StateRunning,
    Kind:   "send_email",
    Limit:  100,
    Offset: 0,
})

// Count tasks
count, err := manager.CountTasks(ctx, &ergon.TaskFilter{
    State: ergon.StateFailed,
    Queue: "critical",
})
```

---

## Integration Examples

### Example 1: Metrics Dashboard with Web UI + Datadog

```go
package main

import (
    "github.com/DataDog/datadog-go/statsd"
    "github.com/hasanerken/ergon"
    "github.com/hasanerken/ergon/internal/jsonutil/monitor"
)

func main() {
    // Initialize Datadog
    dd, _ := statsd.New("127.0.0.1:8125")
    defer dd.Close()

    // Create Ergon manager
    manager := ergon.NewManager(store, ergon.ClientConfig{})

    // Start server with Datadog callbacks
    server, _ := ergon.NewServer(store, ergon.ServerConfig{
        Workers: workers,

        OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
            dd.Histogram("ergon.duration", duration.Seconds(),
                []string{"kind:" + task.Kind}, 1)
        },

        OnTaskFailed: func(ctx context.Context, task *ergon.InternalTask, err error) {
            dd.Incr("ergon.failures",
                []string{"kind:" + task.Kind}, 1)
        },
    })

    // Start web UI for human monitoring
    monitorUI, _ := monitor.NewServer(manager, monitor.Config{
        Addr: ":8888",
    })
    go monitorUI.Start()

    // Now you have:
    // - Web UI at http://localhost:8888/monitor
    // - Metrics flowing to Datadog
}
```

### Example 2: SLA Monitoring

```go
OnTaskCompleted: func(ctx context.Context, task *ergon.InternalTask, duration time.Duration) {
    // Check SLA compliance
    sla := getSLA(task.Kind) // e.g., 5 seconds

    if duration > sla {
        log.Printf("⚠️  SLA violated for %s: %v > %v",
            task.Kind, duration, sla)

        // Alert
        slack.Send("#sla-violations", fmt.Sprintf(
            "Task %s exceeded SLA (%v > %v)",
            task.Kind, duration, sla,
        ))
    }
},
```

### Example 3: Custom Monitoring Dashboard

```go
// Build custom dashboard with statistics API
func getCustomMetrics(manager *ergon.Manager) map[string]interface{} {
    ctx := context.Background()

    stats, _ := manager.GetOverallStats(ctx)
    queues, _ := manager.ListQueues(ctx)

    // Failed tasks in last hour
    failedRecent, _ := manager.CountTasks(ctx, &ergon.TaskFilter{
        State: ergon.StateFailed,
        // Add time filter if available
    })

    return map[string]interface{}{
        "success_rate": stats.SuccessRate,
        "total_tasks": stats.TotalTasks,
        "running_now": stats.RunningTasks,
        "failed_1h": failedRecent,
        "queues": queues,
    }
}
```

---

## Architecture

### Monitoring Components

```
┌─────────────────────────────────────────────────────────┐
│                   ERGON MONITORING                       │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌─────────────────┐       ┌──────────────────────┐    │
│  │   Web UI        │       │  Event Callbacks     │    │
│  │  (HTMX + Go)    │       │  (Lifecycle Hooks)   │    │
│  │                 │       │                      │    │
│  │ • Dashboard     │       │ • OnTaskStarted     │    │
│  │ • Task List     │       │ • OnTaskCompleted   │    │
│  │ • Task Details  │       │ • OnTaskFailed      │    │
│  │ • Queues        │       │ • OnTaskRetried     │    │
│  └────────┬────────┘       └──────────┬───────────┘    │
│           │                           │                 │
│           └───────────┬───────────────┘                 │
│                       │                                 │
│              ┌────────▼─────────┐                      │
│              │   Manager API    │                      │
│              │                  │                      │
│              │ • GetOverallStats│                      │
│              │ • ListQueues     │                      │
│              │ • ListTasks      │                      │
│              │ • CountTasks     │                      │
│              └────────┬─────────┘                      │
│                       │                                 │
│              ┌────────▼─────────┐                      │
│              │  Store Backend   │                      │
│              │ (Postgres/Badger)│                      │
│              └──────────────────┘                      │
│                                                           │
└─────────────────────────────────────────────────────────┘

        External Integrations
        ─────────────────────
        • Datadog       (via callbacks)
        • Prometheus    (via callbacks)
        • CloudWatch    (via callbacks)
        • Sentry        (via callbacks)
        • PagerDuty     (via callbacks)
        • Slack         (via callbacks)
```

### Data Flow

1. **Task Execution** → Server processes task
2. **Lifecycle Events** → Callbacks triggered
3. **Metrics Collection** → Sent to external systems
4. **Web UI Polling** → HTMX requests stats every 5s
5. **Statistics API** → Returns real-time data
6. **Template Rendering** → Updates UI

---

## Best Practices

### 1. **Use Web UI for Development/Debugging**
- Quick visual feedback
- Interactive task management
- Easy troubleshooting

### 2. **Use Event Callbacks for Production Monitoring**
- Integrate with your existing metrics stack
- Real-time alerting
- Custom business logic

### 3. **Use Statistics API for Custom Dashboards**
- Build domain-specific views
- Integrate with existing admin panels
- Programmatic access

### 4. **Combine All Three**
```go
// Web UI for operators
monitorUI.Start()

// Event callbacks for metrics
server := ergon.NewServer(store, ergon.ServerConfig{
    OnTaskCompleted: sendToDatadog,
    OnTaskFailed: sendToSentry,
})

// Statistics API for custom dashboard
http.HandleFunc("/admin/tasks", func(w http.ResponseWriter, r *http.Request) {
    stats, _ := manager.GetOverallStats(r.Context())
    json.NewEncoder(w).Encode(stats)
})
```

---

## Complete Example

See `examples/monitor/main.go` for a complete working example:

```bash
# Run the monitor example
go run examples/monitor/main.go

# Open browser
open http://localhost:8888/monitor
```

Features demonstrated:
- Web UI with auto-refresh
- Multiple queues
- Task filtering
- Real-time statistics
- Task management actions

---

## Summary

**Ergon provides three monitoring approaches**:

| Feature | Best For | Technology |
|---------|----------|------------|
| **Web UI** | Human operators, debugging | HTMX + Go templates |
| **Event Callbacks** | Production metrics, alerting | Go functions |
| **Statistics API** | Custom dashboards, integrations | Manager methods |

**Choose based on your needs**:
- **Need visual monitoring?** → Use Web UI
- **Need production metrics?** → Use Event Callbacks
- **Need custom integration?** → Use Statistics API
- **Need everything?** → Use all three together! ✅
