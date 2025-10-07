# Storage Backend Consistency Test

Testing whether BadgerDB and PostgreSQL implementations are fully consistent.

## Test Plan

We'll run the same test with both stores and compare:
1. Task scheduling behavior
2. Task execution timing
3. State transitions
4. Payload handling
5. Retry logic
6. Error handling

---

## Running the Tests

### BadgerDB Test
```bash
go run examples/scheduling/test_now.go
```

### PostgreSQL Test
```bash
go run examples/scheduling/test_postgres.go
```

### Multi-Task Tests
```bash
# BadgerDB (need to create)
go run examples/scheduling/test_multiple_badger.go

# PostgreSQL
go run examples/scheduling/test_multiple_tasks.go
```

---

## Expected Consistency

Both stores should:
- ✅ Schedule tasks for specific dates
- ✅ Execute tasks at correct times (±5 seconds)
- ✅ Handle same payload types (strings, numbers, arrays, nested objects)
- ✅ Transition through same states
- ✅ Retry failed tasks with same logic
- ✅ Clear `scheduled_at` when moving to pending
- ✅ Support same API methods

---

## Known Differences

### Implementation Details

| Feature | BadgerDB | PostgreSQL |
|---------|----------|------------|
| **Storage Type** | Embedded KV store | Relational database |
| **Scheduler** | Two-phase iterator | Single SQL UPDATE |
| **Transactions** | Badger transactions | SQL transactions |
| **Concurrency** | Manual retry on conflict | Row-level locking |
| **Indexing** | Key prefixes | SQL indexes |

### Performance Characteristics

| Operation | BadgerDB | PostgreSQL |
|-----------|----------|------------|
| **Enqueue** | Fast (in-memory) | Fast (network + disk) |
| **Dequeue** | Two-phase read-write | FOR UPDATE SKIP LOCKED |
| **Queries** | Iteration-based | SQL queries |
| **Scale** | Single node | Multi-node capable |

---

## Consistency Verification Status

Let me verify each operation...
