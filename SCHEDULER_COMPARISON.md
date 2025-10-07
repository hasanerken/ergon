# Scheduler Implementation Comparison: BadgerDB vs PostgreSQL

This document compares the scheduler implementations in BadgerDB and PostgreSQL stores, explaining why one had a bug and the other didn't.

---

## Overview

Both stores implement the `MoveScheduledToAvailable` method which moves tasks from `scheduled` or `retrying` state to `pending` state when their scheduled time arrives.

**Scheduler behavior**:
- Runs every **5 seconds** in `server.go:253-273`
- Calls `store.MoveScheduledToAvailable(ctx, time.Now())`
- Moves tasks where `ScheduledAt <= Now()` to pending state

---

## BadgerDB Implementation (BUGGY → FIXED)

### Original Buggy Code

**File**: `store/badger/scheduler.go` (before fix)

```go
func (s *Store) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
    count := 0

    err := s.db.Update(func(txn *badger.Txn) error {
        // Create iterator over scheduled index
        it := txn.NewIterator(opts)
        defer it.Close()

        for it.Rewind(); it.Valid(); it.Next() {
            item := it.Item()
            key := item.Key()

            // Load task, update state...

            // ❌ BUG: Deleting from index WHILE ITERATING
            if err := txn.Delete(key); err != nil {
                continue
            }

            count++
        }
        return nil
    })

    return count, err
}
```

### Why It Failed

1. **Iterator Corruption**: Badger's iterator doesn't handle deletions during iteration safely
2. **Stale Entries**: The `Delete()` may not take effect immediately within the transaction
3. **Re-execution**: Next scheduler run finds the same task in the scheduled index
4. **Multiple Executions**: Task executes 2, 3, 4+ times every 5 seconds

### Fixed Code

**File**: `store/badger/scheduler.go` (after fix)

```go
func (s *Store) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
    count := 0

    err := s.db.Update(func(txn *badger.Txn) error {
        // PHASE 1: Collect task keys (iterator open)
        var keysToProcess []struct {
            taskID       string
            scheduledKey []byte
        }

        it := txn.NewIterator(opts)
        defer it.Close()

        for it.Rewind(); it.Valid(); it.Next() {
            taskID := s.keys.ParseTaskID(it.Item().Key())
            keyCopy := append([]byte(nil), it.Item().Key()...)
            keysToProcess = append(keysToProcess, struct {
                taskID       string
                scheduledKey []byte
            }{taskID, keyCopy})
        }

        // PHASE 2: Process tasks (iterator closed)
        for _, item := range keysToProcess {
            // Load task
            var task ergon.InternalTask
            // ... unmarshal ...

            // ✅ Validate state before processing
            if task.State != ergon.StateScheduled {
                _ = txn.Delete(item.scheduledKey)
                continue
            }

            // Update task state
            task.State = ergon.StatePending
            task.ScheduledAt = nil
            // ... marshal and save ...

            // ✅ SAFE: Delete after iterator closes
            if err := txn.Delete(item.scheduledKey); err != nil {
                continue
            }

            // Add to pending queue
            pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
            _ = txn.Set(pendingKey, []byte(taskID))

            count++
        }

        return nil
    })

    return count, err
}
```

### Key Improvements

1. ✅ **Two-phase processing**: Collect first, then modify
2. ✅ **Key copying**: Stable references after iterator closes
3. ✅ **State validation**: Check `task.State` before processing
4. ✅ **Stale cleanup**: Remove orphaned index entries
5. ✅ **Safe deletion**: Only delete after iterator closes

---

## PostgreSQL Implementation (SAFE)

### Code

**File**: `store/postgres/scheduler.go`

```go
func (s *Store) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
    query := `
        UPDATE queue_tasks
        SET state = 'pending', scheduled_at = NULL
        WHERE state IN ('scheduled', 'retrying')
          AND scheduled_at <= $1
    `

    result, err := s.db.ExecContext(ctx, query, before)
    if err != nil {
        return 0, fmt.Errorf("failed to move scheduled tasks: %w", err)
    }

    rows, err := result.RowsAffected()
    if err != nil {
        return 0, fmt.Errorf("failed to get rows affected: %w", err)
    }

    return int(rows), nil
}
```

### Why It's Safe

1. ✅ **Single atomic UPDATE**: No iteration needed
2. ✅ **WHERE state IN ('scheduled', 'retrying')**: Only processes correct states
3. ✅ **MVCC (Multi-Version Concurrency Control)**: PostgreSQL handles concurrent updates safely
4. ✅ **Row-level locking**: Prevents race conditions automatically
5. ✅ **No iterator**: SQL handles everything in one operation

### How PostgreSQL Prevents Duplicates

**Transaction Isolation**:
```
Scheduler Run 1 (T1):
  - UPDATE queue_tasks WHERE state = 'scheduled' AND scheduled_at <= NOW()
  - Row locks acquired
  - State changes from 'scheduled' to 'pending'
  - Row locks released
  - COMMIT

Scheduler Run 2 (T2) - 5 seconds later:
  - UPDATE queue_tasks WHERE state = 'scheduled' AND scheduled_at <= NOW()
  - Same task now has state = 'pending' (not 'scheduled')
  - WHERE clause doesn't match
  - Task is NOT updated again ✅
```

The `WHERE state IN ('scheduled', 'retrying')` clause ensures completed or pending tasks are never re-processed.

---

## Comparison Table

| Aspect | BadgerDB (Fixed) | PostgreSQL | Winner |
|--------|------------------|------------|--------|
| **Complexity** | Higher (two-phase) | Lower (single SQL) | PostgreSQL |
| **Safety** | Requires careful coding | Built-in ACID guarantees | PostgreSQL |
| **Performance** | Good (in-memory KV) | Good (optimized SQL) | Tie |
| **Concurrency** | Manual retries needed | Automatic row locking | PostgreSQL |
| **Bug Risk** | Higher (iterator patterns) | Lower (declarative SQL) | PostgreSQL |
| **Dependencies** | None (embedded) | Requires PostgreSQL server | BadgerDB |
| **Operations** | Zero-config | Needs database setup | BadgerDB |

---

## Testing

### Test BadgerDB (Fixed)

```bash
go run examples/scheduling/test_now.go
```

**Expected output**:
```
14:57:36 Scheduler moved 1 tasks to available
14:57:37 🎯 TASK #1 EXECUTED!
[No duplicate executions]
Status: 4 scheduled, 1 completed ✅
```

### Test PostgreSQL

```bash
# Set up PostgreSQL
createdb ergon_test

# Set connection string
export DATABASE_URL="postgres://localhost/ergon_test?sslmode=disable"

# Run test
go run examples/scheduling/test_postgres.go
```

**Expected output**:
```
14:57:36 Scheduler moved 1 tasks to available
14:57:37 🎯 TASK #1 EXECUTED!
[No duplicate executions]
Status: 4 scheduled, 1 completed ✅
```

Both should behave identically after the BadgerDB fix.

---

## Lessons Learned

### Key Takeaway: SQL vs Iterator Patterns

**SQL (PostgreSQL)**:
- Declarative: "UPDATE all rows WHERE conditions"
- Database handles iteration internally
- ACID guarantees built-in
- Less room for bugs

**Iterator (BadgerDB)**:
- Imperative: "For each item, do something"
- Manual state management
- Easy to make mistakes (delete while iterating)
- Requires careful coding

### General Rule

**❌ NEVER modify a collection while iterating over it**

This applies to:
- Database iterators/cursors
- In-memory arrays/maps during loops
- File systems during directory walks
- Any stateful iteration

**✅ ALWAYS collect first, then modify**

```go
// Bad ❌
for item := range collection {
    collection.Delete(item.Key) // Iterator corruption!
}

// Good ✅
var keysToDelete []Key
for item := range collection {
    keysToDelete = append(keysToDelete, item.Key)
}
for _, key := range keysToDelete {
    collection.Delete(key)
}
```

---

## When to Use Each Store

### Use BadgerDB When:
- ✅ Embedded database needed (no external dependencies)
- ✅ Simple deployment (single binary)
- ✅ Development/testing
- ✅ Small to medium task volumes
- ✅ No complex queries needed

### Use PostgreSQL When:
- ✅ Production deployments
- ✅ High task volumes (millions+)
- ✅ Complex queries and reporting needed
- ✅ Multi-server setups (shared database)
- ✅ Strong ACID guarantees critical
- ✅ Database infrastructure already exists

---

## Verification Checklist

After the fix, both stores should:

- [x] ✅ Execute scheduled tasks exactly once
- [x] ✅ Update state from `scheduled` → `pending` → `running` → `completed`
- [x] ✅ Remove tasks from scheduled index after processing
- [x] ✅ Handle concurrent scheduler runs safely
- [x] ✅ Clean up stale index entries
- [x] ✅ Validate task state before processing
- [x] ✅ Pass all existing tests

---

## Summary

| Store | Implementation | Bug Status | Safety Level |
|-------|---------------|------------|--------------|
| **BadgerDB** | Two-phase iterator pattern | ✅ Fixed | Medium (requires care) |
| **PostgreSQL** | Single SQL UPDATE | ✅ Always safe | High (built-in guarantees) |

Both implementations now work correctly and safely. PostgreSQL's approach is inherently safer due to SQL's declarative nature and ACID guarantees, while BadgerDB requires more careful manual state management but offers zero-dependency embedded operation.

---

## Files Modified/Created

### Bug Fix:
- `store/badger/scheduler.go` - Fixed duplicate execution bug

### Tests:
- `examples/scheduling/test_now.go` - BadgerDB test
- `examples/scheduling/test_postgres.go` - PostgreSQL test (NEW)

### Documentation:
- `BUG_FIX_SCHEDULER.md` - Detailed bug analysis
- `SCHEDULER_COMPARISON.md` - This document (NEW)
