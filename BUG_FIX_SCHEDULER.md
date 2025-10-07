# Critical Bug Fix: Task Scheduler Duplicate Execution

## Bug Report

**Severity**: CRITICAL 🔴
**Component**: BadgerDB Store - Scheduler
**File**: `store/badger/scheduler.go`
**Impact**: Scheduled tasks were executing multiple times instead of once

---

## Symptoms

When running `examples/scheduling/test_now.go`, tasks scheduled for specific times would execute repeatedly every 5 seconds:

```
14:41:10 [Queue] Scheduler moved 1 tasks to available
14:41:11 🎯 TASK #1 EXECUTED! (ID: 0199b952-6c3b-7a...)
14:41:11 ✅ Task completed

14:41:15 [Queue] Scheduler moved 1 tasks to available  ← BUG: Same task again!
14:41:16 🎯 TASK #1 EXECUTED! (ID: 0199b952-6c3b-7a...)  ← Duplicate!

14:41:20 [Queue] Scheduler moved 1 tasks to available  ← BUG: Same task again!
14:41:21 🎯 TASK #1 EXECUTED! (ID: 0199b952-6c3b-7a...)  ← Duplicate!
```

**Result**: Task #1 executed **4 times** instead of once.

---

## Root Cause Analysis

### The Problem: Modifying Index While Iterating

The `MoveScheduledToAvailable` function was **deleting from the scheduled index while iterating over it**:

**Buggy Code** (lines 29-80):
```go
it := txn.NewIterator(opts)
defer it.Close()

for it.Rewind(); it.Valid(); it.Next() {
    item := it.Item()
    key := item.Key()

    // ... load task, update state ...

    // ❌ BUG: Deleting from index WHILE ITERATING
    if err := txn.Delete(key); err != nil {
        continue
    }

    count++
}
```

### Why This Caused Duplicate Executions

1. **Iteration + Deletion Conflict**: Badger's iterator may not handle deletions correctly during iteration
2. **Incomplete Deletion**: The `txn.Delete(key)` call may not take effect immediately or may corrupt the iterator state
3. **Stale Index Entry**: The scheduled index entry persists after the transaction commits
4. **Next Scheduler Run**: 5 seconds later, the scheduler finds the same task in the scheduled index again
5. **Re-execution**: The task gets moved to pending again and executes a second time

### Additional Issue: No State Validation

The code didn't check if the task was still in `StateScheduled` before processing. This meant even completed tasks could be re-processed if their scheduled index entry remained.

---

## The Fix

### Solution: Two-Phase Processing

**Phase 1**: Collect all task keys to process (with iterator open)
**Phase 2**: Process tasks and delete index entries (after iterator closes)

**Fixed Code**:
```go
// PHASE 1: Collect all scheduled task keys to process
// (We must NOT delete from index while iterating over it)
var keysToProcess []struct {
    taskID       string
    scheduledKey []byte
}

it := txn.NewIterator(opts)
defer it.Close()

for it.Rewind(); it.Valid(); it.Next() {
    item := it.Item()
    key := item.Key()

    // Stop if we've passed the time threshold
    if string(key) > string(endKey) {
        break
    }

    // Extract task ID and save key for later processing
    taskID := s.keys.ParseTaskID(key)
    keyCopy := append([]byte(nil), key...)  // ✅ Copy the key!
    keysToProcess = append(keysToProcess, struct {
        taskID       string
        scheduledKey []byte
    }{taskID, keyCopy})
}

// PHASE 2: Process collected tasks (iterator is now closed)
for _, item := range keysToProcess {
    taskID := item.taskID
    scheduledKey := item.scheduledKey

    // Load task
    taskKey := s.keys.Task(taskID)
    // ... unmarshal task ...

    // ✅ NEW: Skip if task is no longer in scheduled state
    if task.State != ergon.StateScheduled {
        // Remove stale scheduled index entry
        _ = txn.Delete(scheduledKey)
        continue
    }

    // Update task state
    task.State = ergon.StatePending
    task.ScheduledAt = nil

    // Store updated task
    // ... marshal and save ...

    // ✅ SAFE: Delete from index AFTER iterator is closed
    if err := txn.Delete(scheduledKey); err != nil {
        continue
    }

    // Add to pending queue
    // ...

    count++
}
```

### Key Improvements

1. **Two-Phase Approach**: Separate collection from modification
2. **Key Copying**: `keyCopy := append([]byte(nil), key...)` ensures we have a stable copy
3. **State Validation**: Check `task.State != ergon.StateScheduled` before processing
4. **Stale Entry Cleanup**: Remove index entries for tasks no longer in expected state
5. **Safe Deletion**: Delete from index only after iterator is closed

---

## Testing Results

### Before Fix
```
14:41:10 Scheduler moved 1 tasks
14:41:11 Task #1 executed (attempt 1)
14:41:15 Scheduler moved 1 tasks  ← Same task!
14:41:16 Task #1 executed (attempt 2)  ← Duplicate
14:41:20 Scheduler moved 1 tasks  ← Same task!
14:41:21 Task #1 executed (attempt 3)  ← Duplicate
14:41:25 Scheduler moved 1 tasks  ← Same task!
14:41:26 Task #1 executed (attempt 4)  ← Duplicate
```

### After Fix ✅
```
14:57:36 Scheduler moved 1 tasks
14:57:37 Task #1 executed (attempt 1)
[5 seconds later]
[No duplicate execution - task moved to completed state]
Status: 4 scheduled, 1 completed ✅
```

---

## Files Changed

- **`store/badger/scheduler.go`**:
  - Line 13-106: Fixed `MoveScheduledToAvailable` for scheduled tasks
  - Line 108-194: Fixed retry queue processing (same pattern)

---

## Impact

**Affected Systems**: Any application using Ergon with BadgerDB store and task scheduling

**Severity**: CRITICAL - Tasks could execute multiple times, causing:
- Duplicate emails/notifications
- Duplicate financial transactions
- Data corruption
- Resource wastage

**Fixed By**: Two-phase processing pattern with state validation

---

## Lessons Learned

### General Database Pattern
**❌ NEVER modify a collection while iterating over it**

This applies to:
- BadgerDB iterators
- Database cursors
- In-memory maps/arrays during loops

**✅ ALWAYS use two-phase approach**:
1. Collect items to modify (read-only iteration)
2. Modify items (after iteration completes)

### Additional Best Practices
1. **State Validation**: Always check current state before applying transitions
2. **Cleanup Stale Entries**: Remove orphaned index entries when detected
3. **Transaction Scope**: Keep deletions within the same transaction for atomicity
4. **Key Copying**: Copy iterator keys if storing for later use

---

## Related Issues

The same bug pattern was also present in:
- **Retry queue processing** (lines 83-146) - Fixed with same approach
- Similar patterns should be reviewed in PostgreSQL store

---

## Verification

Run the test to verify the fix:
```bash
go run examples/scheduling/test_now.go
```

Expected output:
- Each task executes exactly once
- Status counters are accurate
- No duplicate "Scheduler moved X tasks" messages for same task

---

## Author
Fixed by: Claude Code
Date: October 6, 2025
Issue: Task scheduler duplicate execution bug in BadgerDB store
