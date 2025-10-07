# Scheduling Examples

This directory contains examples demonstrating Ergon's task scheduling capabilities with both BadgerDB and PostgreSQL stores.

---

## Examples Overview

| File | Store | Description |
|------|-------|-------------|
| `main.go` | BadgerDB | Comprehensive scheduling demo with multiple patterns |
| `test_now.go` | BadgerDB | Live test - schedules tasks in next 5 minutes |
| `test_postgres.go` | PostgreSQL | Same live test using PostgreSQL store |

---

## Quick Start

### 1. Basic Scheduling Demo (BadgerDB)

Shows various scheduling patterns: specific dates, relative delays, business hours, etc.

```bash
go run examples/scheduling/main.go
```

**Features demonstrated**:
- Schedule for October 15, 2025 at 14:13
- Schedule for holidays (Christmas, New Year, Thanksgiving)
- Relative delays (30 seconds, 5 minutes)
- Business hours scheduling
- View all scheduled tasks

---

### 2. Live Test - BadgerDB (Next 5 Minutes)

Tests scheduling with real execution in the next few minutes using **specific dates** (not relative delays).

```bash
go run examples/scheduling/test_now.go
```

**What it does**:
- Schedules 5 tasks for 1, 2, 3, 4, 5 minutes from now
- Uses `time.Date()` with specific timestamps
- Watches tasks execute in real-time
- Shows scheduler behavior every 5 seconds
- Displays status updates

**Expected output**:
```
🧪 ERGON SCHEDULING TEST - NEXT FEW MINUTES
======================================================================
⏰ Current time: 14:56:36
======================================================================

📅 Scheduling tasks with SPECIFIC DATES:
----------------------------------------------------------------------
  ✓ Task #1 scheduled for: 14:57:36 (in 1 min)
    Task ID: 0199b961-77e3-70...
    State: scheduled

  ✓ Task #2 scheduled for: 14:58:36 (in 2 min)
    Task ID: 0199b961-77e5-70...
    State: scheduled

...

======================================================================
🚀 Starting server...
======================================================================
✅ Server running!
📊 The scheduler checks for scheduled tasks every 5 seconds
⏰ Watch tasks execute at their scheduled times...

[Wait ~1 minute]

2025/10/06 14:57:36 [Queue] Scheduler moved 1 tasks to available
2025/10/06 14:57:37 ▶️  Task started: 0199b961-77e3-70...

======================================================================
🎯 TASK #1 EXECUTED!
📋 Message: First task - 1 minute from start
⏰ Scheduled for: 14:57:36
✅ Executed at:   14:57:37
⏱️  Delay: 1s
======================================================================

2025/10/06 14:57:37 ✅ Task completed: 0199b961-77e3-70... (took 7ms)

📊 Status: 4 scheduled, 1 completed
```

**Verification**:
- ✅ Each task executes exactly once
- ✅ Execution time matches scheduled time (±5 seconds)
- ✅ Status counters are accurate
- ✅ No duplicate executions

---

### 3. Live Test - PostgreSQL (Next 5 Minutes)

Same test as above, but using PostgreSQL store to verify both implementations work identically.

#### Prerequisites

1. **Install PostgreSQL** (if not already installed):
   ```bash
   # macOS
   brew install postgresql@14
   brew services start postgresql@14

   # Ubuntu/Debian
   sudo apt-get install postgresql
   sudo service postgresql start

   # Or use Docker
   docker run --name postgres -e POSTGRES_PASSWORD=postgres -p 5432:5432 -d postgres:14
   ```

2. **Create test database**:
   ```bash
   createdb ergon_test

   # Or with Docker
   docker exec -it postgres createdb -U postgres ergon_test
   ```

3. **Set connection string**:
   ```bash
   export DATABASE_URL="postgres://localhost/ergon_test?sslmode=disable"

   # Or with custom credentials
   export DATABASE_URL="postgres://user:password@localhost:5432/ergon_test?sslmode=disable"

   # Or with Docker
   export DATABASE_URL="postgres://postgres:postgres@localhost:5432/ergon_test?sslmode=disable"
   ```

#### Run Test

```bash
go run examples/scheduling/test_postgres.go
```

**Expected output**: Same as BadgerDB test above.

---

## Scheduling Patterns

### 1. Specific Date/Time

```go
// October 15, 2025 at 14:13:00 UTC
targetTime := time.Date(2025, time.October, 15, 14, 13, 0, 0, time.UTC)

task, _ := ergon.Enqueue(client, ctx, YourTaskArgs{...},
    ergon.WithScheduledAt(targetTime),
)
```

### 2. Relative Delay

```go
// Execute in 30 minutes
task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithDelay(30 * time.Minute),
)
```

### 3. Business Hours

```go
// Next business day at 9 AM
tomorrow := time.Now().AddDate(0, 0, 1)
businessTime := time.Date(
    tomorrow.Year(), tomorrow.Month(), tomorrow.Day(),
    9, 0, 0, 0,
    time.Local,
)

task, _ := ergon.Enqueue(client, ctx, args,
    ergon.WithScheduledAt(businessTime),
)
```

---

## How Scheduling Works

### Architecture

```
1. Task Enqueued (State: scheduled)
   ↓
2. Stored in database with ScheduledAt timestamp
   ↓
3. Server Scheduler (runs every 5 seconds)
   ├─ Checks for tasks where ScheduledAt <= Now()
   └─ Moves them to State: pending
   ↓
4. Workers pick up and execute
   ↓
5. Task completes (State: completed)
```

### Scheduler Component

- **Interval**: Runs every **5 seconds**
- **Precision**: Tasks execute within **0-5 seconds** of scheduled time
- **Query**: `MoveScheduledToAvailable(ctx, time.Now())`
- **State Transition**: `scheduled` → `pending` → `running` → `completed`

### BadgerDB vs PostgreSQL

| Aspect | BadgerDB | PostgreSQL |
|--------|----------|------------|
| **Implementation** | Two-phase iterator pattern | Single SQL UPDATE |
| **Dependencies** | None (embedded) | PostgreSQL server |
| **Setup** | Zero config | Database creation needed |
| **Performance** | Excellent (in-memory) | Excellent (optimized SQL) |
| **Concurrency** | Manual retry logic | Automatic ACID guarantees |
| **Production** | Good for small-medium scale | Preferred for large scale |

Both implementations are now safe and execute tasks exactly once.

---

## Verification Checklist

After running tests, verify:

- [x] ✅ Each task executes **exactly once**
- [x] ✅ Execution time matches scheduled time (±5 seconds)
- [x] ✅ Status counters are accurate
- [x] ✅ No "Scheduler moved X tasks" duplicates for same task
- [x] ✅ Task state transitions correctly
- [x] ✅ Server handles Ctrl+C gracefully

---

## Troubleshooting

### BadgerDB Test

**Error: "Failed to open badger"**
- Check disk space
- Ensure `./data/schedule_test` directory is writable
- Try: `rm -rf ./data/schedule_test` and run again

### PostgreSQL Test

**Error: "connection refused"**
- Check PostgreSQL is running: `pg_isready`
- Start PostgreSQL: `brew services start postgresql@14` (macOS)

**Error: "database does not exist"**
- Create database: `createdb ergon_test`

**Error: "password authentication failed"**
- Check connection string credentials
- Verify with: `psql $DATABASE_URL`

**Error: "relation does not exist"**
- Tables are auto-created on first run
- If issues persist, check `store/postgres/schema.sql`

---

## Key Files

### Test Files
- `test_now.go` - Live BadgerDB test (this is what revealed the bug!)
- `test_postgres.go` - Live PostgreSQL test
- `main.go` - Comprehensive scheduling demo

### Documentation
- `SCHEDULING_GUIDE.md` - Complete scheduling guide
- `BUG_FIX_SCHEDULER.md` - Bug analysis and fix
- `SCHEDULER_COMPARISON.md` - BadgerDB vs PostgreSQL comparison

### Implementation
- `store/badger/scheduler.go` - BadgerDB scheduler (fixed)
- `store/postgres/scheduler.go` - PostgreSQL scheduler (safe)
- `server.go:253-273` - Server scheduler component

---

## Learn More

For complete scheduling documentation, see:
- **[SCHEDULING_GUIDE.md](../../SCHEDULING_GUIDE.md)** - Complete guide with patterns and examples
- **[SCHEDULER_COMPARISON.md](../../SCHEDULER_COMPARISON.md)** - Technical comparison of implementations
- **[BUG_FIX_SCHEDULER.md](../../BUG_FIX_SCHEDULER.md)** - Critical bug fix details

---

## Contributing

Found a bug or have suggestions? Please:
1. Run the test to reproduce: `go run examples/scheduling/test_now.go`
2. Check the output for duplicate executions or incorrect behavior
3. Report with test output and system details

**Tests that revealed bugs**:
- `test_now.go` - Revealed duplicate execution bug in BadgerDB scheduler (Oct 2025)
  - Task #1 executed 4 times instead of once
  - Fixed by implementing two-phase processing pattern
