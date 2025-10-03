# Task Control Operations Guide

This guide explains the **Task Control Operations** demonstrated in the comprehensive example, specifically:

1. **Cancelling Pending (Not Yet Executed) Tasks**
2. **Manual Retry After Complete Failure**

---

## 1. Cancelling Pending Tasks ❌

### Scenario
You want to cancel a task **before it executes**. For example:
- A scheduled email that's no longer needed
- A delayed notification that's been superseded
- A batch job that's been cancelled by user

### How It Works

```go
// Enqueue a task with delay (won't execute for 30 seconds)
pendingTask, _ := ergon.Enqueue(client, ctx, EmailTask{
    To:      "user@example.com",
    Subject: "Delayed email",
}, ergon.WithDelay(30*time.Second))

// Cancel it immediately (before it executes)
err := manager.Control().Cancel(ctx, pendingTask.ID)
if err == nil {
    log.Println("✅ Task cancelled successfully")
}

// Verify cancellation
details, _ := manager.Tasks().GetTaskDetails(ctx, pendingTask.ID)
// details.Task.State == "cancelled"
```

### Key Points
- ✅ Works for tasks in `pending`, `scheduled`, or `retrying` states
- ✅ Task will **never execute** after cancellation
- ✅ Can cancel tasks with delays, scheduled times, or waiting for retry
- ❌ Cannot cancel tasks already `running` or `completed`

### Output Example
```
🔴 Cancelling Pending Task
   Enqueued task with 30s delay: 0199a9f7-8394-75fb-9a5d-bad55867a4e2
   ✅ Cancelled pending task: 0199a9f7-8394-75fb-9a5d-bad55867a4e2
   📊 Task state: cancelled (never executed)
```

---

## 2. Manual Retry After Complete Failure 🔄

### Scenario
A task has **exhausted all automatic retries** and is now in `failed` state. The underlying issue is fixed (e.g., server is back up), and you want to **manually retry** the task.

**Real-world examples:**
- Database was down for 1 hour → Now it's back up
- External API was rate-limiting → Limit has reset
- Network issue resolved → Retry failed sync tasks

### How It Works

```go
// 1. Create a task that will fail all retries
failedTask, _ := ergon.Enqueue(client, ctx, DataSyncTask{
    SourceID: "db-primary",
    DestinationID: "db-replica",
}, ergon.WithMaxRetries(2))

// 2. Wait for it to fail completely
time.Sleep(10 * time.Second)

// 3. Check it's in failed state
details, _ := manager.Tasks().GetTaskDetails(ctx, failedTask.ID)
// details.Task.State == "failed"
// details.Task.Retried == 2 (all retries exhausted)

// 4. Manual retry - server is back up!
err := manager.Control().RetryNow(ctx, failedTask.ID)
if err == nil {
    log.Println("✅ Task manually retried")
    // Task moves to "pending" and will be executed immediately
}
```

### Retry Flow

1. **Task fails** → Auto-retry (attempt 1)
2. **Fails again** → Auto-retry with delay (attempt 2)  
3. **Fails final time** → State becomes `failed` ❌
4. **You fix the issue** (server restart, API fix, etc.)
5. **Manual retry** → `manager.Control().RetryNow(ctx, taskID)` ✅
6. **Task executes again** → Success! 🎉

### Key Points
- ✅ Works **only** for tasks in `failed` state
- ✅ Resets the task to `pending` state
- ✅ Task will be picked up immediately by workers
- ✅ Previous retry count is preserved for tracking
- ❌ Cannot retry tasks in `retrying`, `pending`, or `completed` states

### Output Example
```
💥 Creating Task That Will Fail All Retries
   Enqueued task that will fail: 0199a9f7-8394-7eb5-8f6d-3746a704c76c
   ⏳ Waiting for it to fail all retries...

❌ [FAIL] Attempt 1 - Task failing: Server is down
❌ [FAIL] Attempt 2 - Task failing: Server is down  
❌ [FAIL] Attempt 3 - Task failing: Server is down

   📊 Task state after retries: failed
   📊 Retry count: 2

🔄 Manual Retry After Complete Failure
   💡 Scenario: Server is back up, manually retrying failed task
   ✅ Manually triggered retry for task: 0199a9f7-8394-7eb5-8f6d-3746a704c76c
   ⏳ Task will be retried immediately...

✅ [SYNC] Successfully synced data
   📊 Task state after manual retry: completed
```

---

## API Reference

### Cancel Task
```go
// Cancel a pending/scheduled task
err := manager.Control().Cancel(ctx, taskID)

// Check for errors
if errors.Is(err, ergon.ErrTaskNotFound) {
    // Task doesn't exist
} else if err != nil {
    // Other error (e.g., task already running)
}
```

### Manual Retry
```go
// Retry a failed task
err := manager.Control().RetryNow(ctx, taskID)

// Check for errors
if err != nil {
    // Task not in failed state, or doesn't exist
}
```

### Check Task State Before Control
```go
// Get task details first
details, err := manager.Tasks().GetTaskDetails(ctx, taskID)
if err != nil {
    log.Fatal(err)
}

// Decide action based on state
switch details.Task.State {
case ergon.StatePending, ergon.StateScheduled:
    // Can cancel
    manager.Control().Cancel(ctx, taskID)
    
case ergon.StateFailed:
    // Can manually retry
    manager.Control().RetryNow(ctx, taskID)
    
case ergon.StateCompleted:
    // Can delete
    manager.Control().Delete(ctx, taskID)
}
```

---

## Batch Control Operations

### Cancel Multiple Tasks
```go
// Cancel all pending tasks in a queue
cancelled, err := manager.Batch().CancelMany(ctx, ergon.TaskFilter{
    Queue: "notifications",
    State: ergon.StatePending,
})
log.Printf("Cancelled %d tasks", cancelled)
```

### Retry All Failed Tasks
```go
// Retry all failed tasks (e.g., after fixing infrastructure)
retried, err := manager.Batch().RetryAllFailed(ctx, "default")
log.Printf("Retried %d failed tasks", retried)
```

---

## Complete Example

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/hasanerken/ergon"
    "github.com/hasanerken/ergon/store/badger"
)

func main() {
    ctx := context.Background()
    
    // Setup
    store, _ := badger.NewStore("./queue")
    defer store.Close()
    
    workers := ergon.NewWorkers()
    // ... register workers ...
    
    manager := ergon.NewManager(store, ergon.ClientConfig{Workers: workers})
    
    // Example 1: Cancel pending task
    task1, _ := ergon.Enqueue(manager, ctx, MyTask{}, 
        ergon.WithDelay(1*time.Hour))
    
    manager.Control().Cancel(ctx, task1.ID)
    log.Println("Task cancelled before execution")
    
    // Example 2: Manual retry after failure
    task2, _ := ergon.Enqueue(manager, ctx, SyncTask{},
        ergon.WithMaxRetries(3))
    
    // ... task fails all 3 retries ...
    time.Sleep(10 * time.Second)
    
    // Fix the issue, then retry manually
    manager.Control().RetryNow(ctx, task2.ID)
    log.Println("Task retried manually after infrastructure fix")
}
```

---

## Use Cases

### Cancel Pending Tasks
- 📧 User unsubscribes before scheduled email
- 🔔 Notification superseded by newer one
- 📊 Report cancelled before generation
- ⏰ Scheduled task no longer needed

### Manual Retry Failed Tasks
- 🗄️ Database was down → Now restored
- 🌐 API rate limit → Limit reset
- 🔌 Network outage → Connection restored
- 🔧 Configuration error → Fixed and need to reprocess
- 📦 Dependency service → Back online after maintenance

---

## Best Practices

1. **Always check task state** before control operations
2. **Log control operations** for audit trail
3. **Use batch operations** for multiple tasks
4. **Handle errors gracefully** - tasks may change state
5. **Monitor failed tasks** - set up alerts for manual intervention
6. **Document retry procedures** for ops team

---

## Running the Example

```bash
cd examples/comprehensive
go run main.go
```

Look for the **"🎮 TASK CONTROL OPERATIONS"** section in the output to see both features in action!
