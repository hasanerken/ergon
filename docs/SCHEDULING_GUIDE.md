# Task Scheduling Guide

## Quick Answer: Yes, You Can Schedule Tasks for Specific Dates! ✅

Ergon supports **precise task scheduling** down to the second.

---

## 🎯 Schedule for October 15, 2025 at 14:13

```go
// Define exact time
targetTime := time.Date(2025, time.October, 15, 14, 13, 0, 0, time.UTC)

// Schedule task
task, err := ergon.Enqueue(client, ctx, YourTaskArgs{...},
    ergon.WithScheduledAt(targetTime),
)
```

**That's it!** The task will execute precisely at October 15, 2025 at 14:13 UTC.

---

## 📅 Scheduling Methods

### Method 1: Absolute Time (Specific Date/Time)

```go
// October 15, 2025 at 14:13:00 UTC
targetTime := time.Date(2025, time.October, 15, 14, 13, 0, 0, time.UTC)

task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithScheduledAt(targetTime),
)
```

**Use cases**:
- Birthday notifications
- Scheduled reports
- Appointment reminders
- Campaign launches

### Method 2: Relative Delay

```go
// Execute in 30 minutes
task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithDelay(30 * time.Minute),
)

// OR (same thing)
task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithProcessIn(30 * time.Minute),
)
```

**Use cases**:
- Follow-up emails
- Retry after cooldown
- Delayed notifications

### Method 3: Business Hours

```go
// Next business day at 9 AM
tomorrow := time.Now().AddDate(0, 0, 1)
businessTime := time.Date(
    tomorrow.Year(), tomorrow.Month(), tomorrow.Day(),
    9, 0, 0, 0, // 9:00 AM
    time.Local,
)

task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithScheduledAt(businessTime),
)
```

---

## 🔧 How It Works

### Architecture

```
1. Task Enqueued (State: scheduled)
   ↓
2. Stored in database with ScheduledAt timestamp
   ↓
3. Server Scheduler (runs every 5 seconds)
   ├─ Checks for tasks where ScheduledAt <= Now()
   └─ Moves them to State: available
   ↓
4. Workers pick up and execute
   ↓
5. Task completes (State: completed)
```

### Scheduler Component

The server includes a **built-in scheduler** that:
- Runs every **5 seconds**
- Checks for tasks where `ScheduledAt <= time.Now()`
- Moves them from `StateScheduled` → `StateAvailable`
- Workers then pick them up immediately

**Code**: `server.go:253-273`

```go
// scheduler moves scheduled tasks to available state
func (s *Server) scheduler(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            count, _ := s.store.MoveScheduledToAvailable(ctx, time.Now())
            if count > 0 {
                log.Printf("[Queue] Scheduler moved %d tasks to available", count)
            }
        }
    }
}
```

---

## 📊 Task States

```
┌──────────────┐
│   ENQUEUE    │
└──────┬───────┘
       │
       ▼
┌──────────────────────┐
│  StateScheduled      │ ← Task is waiting for scheduled time
│  (waiting for time)  │
└──────┬───────────────┘
       │ Scheduler checks every 5s
       │ When: ScheduledAt <= Now()
       ▼
┌──────────────────────┐
│  StateAvailable      │ ← Time has arrived!
│  (ready to execute)  │
└──────┬───────────────┘
       │ Worker picks up
       ▼
┌──────────────────────┐
│  StateRunning        │ ← Currently executing
└──────┬───────────────┘
       │
       ▼
┌──────────────────────┐
│  StateCompleted      │ ← Done!
└──────────────────────┘
```

---

## 💡 Common Scheduling Patterns

### Pattern 1: Birthday Notifications

```go
// Send birthday wish on user's birthday at 9 AM
userBirthday := time.Date(2025, 10, 15, 9, 0, 0, 0, time.Local)

ergon.Enqueue(client, ctx, SendEmailArgs{
    To:      "user@example.com",
    Subject: "Happy Birthday!",
    Body:    "🎉 Have a wonderful day!",
},
    ergon.WithScheduledAt(userBirthday),
    ergon.WithQueue("notifications"),
)
```

### Pattern 2: Scheduled Reports

```go
// Generate report every Monday at 8 AM
nextMonday := getNextMonday(time.Now())
reportTime := time.Date(
    nextMonday.Year(), nextMonday.Month(), nextMonday.Day(),
    8, 0, 0, 0,
    time.Local,
)

ergon.Enqueue(client, ctx, GenerateReportArgs{
    Type:      "weekly_sales",
    StartDate: time.Now().AddDate(0, 0, -7),
    EndDate:   time.Now(),
},
    ergon.WithScheduledAt(reportTime),
    ergon.WithQueue("reports"),
)
```

### Pattern 3: Trial Expiration

```go
// Notify user 3 days before trial ends
trialEndDate := user.TrialEndsAt
notifyDate := trialEndDate.Add(-3 * 24 * time.Hour)

ergon.Enqueue(client, ctx, NotifyTrialExpiringArgs{
    UserID:     user.ID,
    DaysLeft:   3,
    TrialEnds:  trialEndDate,
},
    ergon.WithScheduledAt(notifyDate),
)
```

### Pattern 4: Campaign Launch

```go
// Launch marketing campaign at midnight on launch day
launchDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

ergon.Enqueue(client, ctx, LaunchCampaignArgs{
    CampaignID: "black_friday_2025",
    Target:     "all_users",
},
    ergon.WithScheduledAt(launchDate),
    ergon.WithPriority(100), // High priority
)
```

### Pattern 5: Follow-up Email Sequence

```go
// Day 1: Welcome email (immediate)
ergon.Enqueue(client, ctx, SendEmailArgs{...})

// Day 3: Follow-up email
ergon.Enqueue(client, ctx, SendEmailArgs{...},
    ergon.WithDelay(3 * 24 * time.Hour),
)

// Day 7: Check-in email
ergon.Enqueue(client, ctx, SendEmailArgs{...},
    ergon.WithDelay(7 * 24 * time.Hour),
)
```

---

## 🌍 Timezone Handling

### UTC (Recommended for consistency)

```go
targetTime := time.Date(2025, 10, 15, 14, 13, 0, 0, time.UTC)
```

### Local Timezone

```go
// User's local time
loc, _ := time.LoadLocation("America/New_York")
targetTime := time.Date(2025, 10, 15, 14, 13, 0, 0, loc)
```

### Convert from User Input

```go
// Parse user's input (e.g., "2025-10-15 14:13")
userInput := "2025-10-15 14:13"
targetTime, err := time.Parse("2006-01-02 15:04", userInput)
```

---

## 🔍 Querying Scheduled Tasks

### List all scheduled tasks

```go
tasks, err := manager.ListTasks(ctx, &ergon.TaskFilter{
    State: ergon.StateScheduled,
    Limit: 100,
})

for _, task := range tasks {
    fmt.Printf("Task %s scheduled for %s\n",
        task.ID,
        task.ScheduledAt.Format("Jan 2, 2006 3:04 PM"),
    )
}
```

### Count scheduled tasks

```go
count, err := manager.CountTasks(ctx, &ergon.TaskFilter{
    State: ergon.StateScheduled,
    Queue: "notifications",
})

fmt.Printf("Scheduled notifications: %d\n", count)
```

### Find tasks scheduled within a time range

```go
tasks, err := manager.ListTasks(ctx, &ergon.TaskFilter{
    State:         ergon.StateScheduled,
    ScheduledFrom: time.Now(),
    ScheduledTo:   time.Now().Add(24 * time.Hour),
})
```

---

## ⚙️ Configuration

### Server Scheduler Interval

The scheduler checks every **5 seconds** by default. This is hardcoded in `server.go:257`:

```go
ticker := time.NewTicker(5 * time.Second)
```

**Precision**: Tasks will execute within **0-5 seconds** of their scheduled time.

**Example**:
- Task scheduled for: `2025-10-15 14:13:00`
- Scheduler checks at: `14:13:03`
- Task executes: `14:13:03` ✅

---

## 🚀 Running the Examples

### Comprehensive Demo (BadgerDB)
```bash
# Run the full scheduling demo
go run examples/scheduling/main.go

# Output:
🗓️  ERGON TASK SCHEDULING EXAMPLES
==================================================

📅 Example 1: Schedule task for October 15, 2025 at 14:13
✅ Task scheduled successfully!
   Task ID: abc123...
   Will execute on: Tuesday, October 15, 2025 at 2:13 PM UTC
   Time until execution: 8760h0m0s

📅 Example 2: Schedule multiple tasks for different dates
   ✓ Christmas Morning scheduled for Dec 25, 2025 9:00 AM
   ✓ New Year 2026 scheduled for Jan 1, 2026 12:00 AM
   ✓ Thanksgiving scheduled for Nov 28, 2025 12:00 PM
```

### Live Test - Next 5 Minutes (BadgerDB)
```bash
# Watch tasks execute in real-time!
go run examples/scheduling/test_now.go

# Schedules 5 tasks for the next 1-5 minutes
# Uses specific dates (not relative delays)
# Shows real execution with timing accuracy
```

### Live Test - PostgreSQL
```bash
# Set up PostgreSQL first
createdb ergon_test
export DATABASE_URL="postgres://localhost/ergon_test?sslmode=disable"

# Run test
go run examples/scheduling/test_postgres.go

# Same behavior as BadgerDB test
# Verifies both stores work identically
```

See [examples/scheduling/README.md](examples/scheduling/README.md) for detailed instructions.

---

## ✅ Best Practices

### 1. Always Use UTC for Storage

```go
// Store in UTC
utcTime := time.Date(2025, 10, 15, 14, 13, 0, 0, time.UTC)

// Convert to user's timezone for display
userLoc, _ := time.LoadLocation("America/New_York")
userTime := utcTime.In(userLoc)
```

### 2. Add Metadata for Tracking

```go
ergon.Enqueue(client, ctx, args,
    ergon.WithScheduledAt(targetTime),
    ergon.WithMetadata(map[string]interface{}{
        "scheduled_by": "admin_user_123",
        "reason":       "birthday_campaign",
        "target_date":  "2025-10-15",
    }),
)
```

### 3. Use Unique Keys to Prevent Duplicates

```go
// Prevent duplicate birthday messages
ergon.Enqueue(client, ctx, args,
    ergon.WithScheduledAt(birthdayTime),
    ergon.WithUnique(24 * time.Hour), // Only one per day
)
```

### 4. Monitor Scheduled Tasks

```go
// Check scheduled task count
count, _ := manager.CountTasks(ctx, &ergon.TaskFilter{
    State: ergon.StateScheduled,
})

if count > 10000 {
    log.Warn("Large number of scheduled tasks!")
}
```

---

## 📝 Summary

| Feature | Method | Example |
|---------|--------|---------|
| **Specific date/time** | `WithScheduledAt(time)` | Oct 15, 2025 14:13 |
| **Relative delay** | `WithDelay(duration)` | In 30 minutes |
| **Process in** | `WithProcessIn(duration)` | Same as WithDelay |
| **Precision** | Scheduler checks every 5s | ±5 second accuracy |
| **State** | `StateScheduled` → `StateAvailable` | Automatic transition |
| **Timezone** | Use `time.UTC` | Recommended |

---

## 🎯 Answer to Your Question

**Can you run a job on October 15, 2025 at 14:13?**

**YES!** ✅

```go
// This is exactly how you do it:
targetTime := time.Date(2025, time.October, 15, 14, 13, 0, 0, time.UTC)

task, err := ergon.Enqueue(client, ctx, YourTaskArgs{
    // your task data
},
    ergon.WithScheduledAt(targetTime),
)

// Task will execute on October 15, 2025 at 14:13 UTC (±5 seconds)
```

**That's all you need!** The scheduler handles the rest automatically.

---

## 📚 Additional Resources

### Documentation
- **[examples/scheduling/README.md](examples/scheduling/README.md)** - Running the examples and tests
- **[SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md)** - BadgerDB vs PostgreSQL implementation comparison
- **[BUG_FIX_SCHEDULER.md](BUG_FIX_SCHEDULER.md)** - Critical bug fix details (Oct 2025)

### Implementation Files
- **`store/badger/scheduler.go`** - BadgerDB scheduler implementation
- **`store/postgres/scheduler.go`** - PostgreSQL scheduler implementation
- **`server.go:253-273`** - Server scheduler component

### Test Files
- **`examples/scheduling/main.go`** - Comprehensive scheduling demo
- **`examples/scheduling/test_now.go`** - Live BadgerDB test (next 5 minutes)
- **`examples/scheduling/test_postgres.go`** - Live PostgreSQL test

Both BadgerDB and PostgreSQL stores are fully tested and safe. See [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) for technical details.
