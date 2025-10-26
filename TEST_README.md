# Ergon Test Suite

Comprehensive test suite for the Ergon distributed task queue system.

## Quick Start

```bash
# Run all tests
make test-all

# Run unit tests only (fast)
make test-unit

# Run with coverage
make test-coverage

# Run race detector
make test-race
```

## Test Structure

### Test Categories

1. **Unit Tests** (`test/unit/`) - Fast, no I/O
   - Client operations and validation
   - Worker registration and type safety
   - Task options and configuration
   - Error handling
   - Mock store integration

2. **Integration Tests** (`test/integration/`) - Real storage
   - End-to-end workflows with BadgerDB
   - Task lifecycle (enqueue → process → complete)
   - Multiple queues with priorities
   - Scheduler and retry logic

3. **Stress Tests** (`test/integration/`) - Performance
   - High-volume enqueue (10,000+ tasks)
   - Concurrent operations (100+ goroutines)
   - Memory usage patterns
   - Benchmarks

4. **Race Tests** (`test/integration/`) - Thread safety
   - Concurrent access patterns
   - Run with `-race` flag

### Test Files

```
ergon/
├── errors_test.go              # Error handling tests
├── testing_helpers_test.go     # Test utilities
├── test/
│   ├── unit/
│   │   ├── client_test.go      # Client operations (15+ tests)
│   │   ├── worker_test.go      # Worker registry (8+ tests)
│   │   ├── options_test.go     # Task options (30+ tests)
│   │   └── integration_mock_test.go  # Mock integration (15+ tests)
│   └── integration/
│       ├── badger_test.go      # BadgerDB integration (8+ tests)
│       ├── stress_test.go      # Performance tests (7+ tests, 4 benchmarks)
│       └── race_test.go        # Concurrency tests (12+ tests)
└── store/mock/
    └── postgres_mock.go        # In-memory mock store
```

## Running Tests

### By Category

```bash
# Unit tests (fast, no I/O)
go test -short ./...
make test-unit

# Integration tests (real BadgerDB)
go test ./test/integration/... -run Integration
make test-integration

# Stress tests (high volume)
go test ./test/integration/... -run Stress
make test-stress

# Race detector tests
go test -race ./test/integration/... -run Race
make test-race

# Benchmarks
go test -bench=. -benchmem -run=^$$ ./test/integration/...
make test-bench
```

### Specific Tests

```bash
# Run specific test
go test -v -run TestClient_Enqueue ./test/unit/...

# Run with verbose output
go test -v ./...

# Run tests 10 times (check for flaky tests)
go test -count=10 ./...

# Run with timeout
go test -timeout 10m ./...
```

### Coverage

```bash
# Generate coverage report
make test-coverage

# View coverage for specific file
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep client.go

# HTML coverage report (creates coverage.html)
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## Test Coverage Summary

### Unit Tests (`test/unit/`)

**Client Tests** (15+ cases)
- Basic enqueue with validation
- Task options (queue, priority, timeout, metadata)
- Scheduled tasks
- Unique tasks (deduplication)
- Batch operations (`EnqueueMany`)
- Worker validation
- Queue management (pause/resume)

**Worker Tests** (8+ cases)
- Worker registration and lookup
- Duplicate detection
- TimeoutProvider, RetryProvider, MiddlewareProvider interfaces
- Type-safe execution
- Concurrent registration safety

**Options Tests** (30+ cases)
- All task configuration options
- Uniqueness options and key computation
- Scheduling and recurring options
- Aggregation options
- Composite options (helpers)

**Mock Integration Tests** (15+ cases)
- Complete task lifecycle
- State transitions (success, failure, retry, cancel)
- Queue priorities
- Task listing, filtering, counting
- Group operations

### Integration Tests (`test/integration/`)

**BadgerDB Tests** (8+ cases)
- End-to-end workflows
- Multi-queue processing
- Retry logic with exponential backoff
- Scheduler execution
- Uniqueness enforcement
- Middleware execution

**Stress Tests** (7+ tests, 4 benchmarks)
- High-volume enqueue (10K+ tasks)
- Concurrent enqueue (100 goroutines × 100 tasks)
- High-throughput processing
- Mixed workload scenarios
- Batch operations
- Performance benchmarks

**Race Tests** (12+ cases)
- Concurrent enqueue/dequeue
- Concurrent state changes
- Worker registration races
- Queue management races
- Server lifecycle races

## Test Utilities

### Mock Store (`store/mock/postgres_mock.go`)

In-memory Store implementation:
- Thread-safe with mutex protection
- Full Store interface support
- Task priority sorting
- Queue management
- Group operations
- No external dependencies

### Test Helpers

**Main Package** (`testing_helpers_test.go`):
```go
// Cleanup management
helper := NewTestHelper(t)
defer helper.Close()

// Standard test workers
workers := CreateTestWorkers()

// Wait for task state
task := WaitForTaskState(t, store, taskID, StateCompleted, 5*time.Second)

// Wait for condition
WaitForCondition(t, func() bool {
    return completed >= expected
}, 10*time.Second, "tasks to complete")

// Assertions
AssertNoError(t, err, "operation failed")
AssertTaskState(t, store, taskID, StateCompleted)
```

**Test Task Types**:
- `TestTaskArgs` - Simple test task
- `FailingTaskArgs` - Always fails
- `SlowTaskArgs` - Delays execution

### Writing New Tests

**Unit Test Example**:
```go
func TestClient_NewFeature(t *testing.T) {
    store := mock.NewPostgresStore()
    workers := CreateTestWorkers()
    client := NewClient(store, ClientConfig{Workers: workers})
    
    ctx := context.Background()
    task, err := Enqueue(client, ctx, TestTaskArgs{Message: "test"})
    
    AssertNoError(t, err, "enqueue failed")
    AssertTaskState(t, store, task.ID, StatePending)
}
```

**Integration Test Example**:
```go
func TestIntegration_Badger_NewFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    
    helper := NewTestHelper(t)
    defer helper.Close()
    
    store := helper.NewBadgerStore()
    // Test with real storage
}
```

**Race Test Example**:
```go
func TestRace_NewConcurrency(t *testing.T) {
    var wg sync.WaitGroup
    
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Concurrent operation
        }()
    }
    
    wg.Wait()
    // Verify consistency
}
```

## Performance Benchmarks

Reference results:
```
BenchmarkEnqueue-8              50000     30000 ns/op    (~33K ops/sec)
BenchmarkEnqueueParallel-8     100000     15000 ns/op    (~66K ops/sec)
BenchmarkDequeue-8              40000     35000 ns/op    (~28K ops/sec)
BenchmarkEnqueueMany-8           5000    250000 ns/op    (100 tasks/batch)
```

## Test Organization

### Package Structure

Due to Go's import cycle restrictions:
- Unit tests with mock store: `test/unit/` package
- Integration tests with BadgerDB: `test/integration/` package
- Main package tests: `*_test.go` files in root

### Naming Conventions

- Unit: `TestClient_*`, `TestWorker_*`, `TestOptions_*`
- Integration: `TestIntegration_Badger_*`, `TestIntegration_Mock_*`
- Stress: `TestStress_*`
- Race: `TestRace_*`
- Benchmarks: `BenchmarkEnqueue*`

## CI/CD Integration

```bash
# Run all CI checks
make ci
```

Runs:
1. Code formatting (`go fmt`)
2. Static analysis (`go vet`)
3. All tests (unit, integration, stress)
4. Race detector tests

## Debugging Failed Tests

```bash
# Verbose output
go test -v -run FailingTest

# Race detector
go test -race -v -run TestRace_Specific

# Increase timeout
go test -timeout 10m -run SlowTest

# Run multiple times
go test -count=10 -run FlakyTest
```

## Coverage Goals

- **Core Package**: 80%+ coverage
- **Store Implementations**: 70%+ coverage
- **Critical Paths**: 90%+ coverage (enqueue, dequeue, state transitions)

## Known Limitations

### Mock Store
- In-memory only (no real persistence)
- No transaction support
- No pub/sub events
- Simplified unique key checking

### Test Isolation
- BadgerDB tests use temporary directories with automatic cleanup
- Mock store can be reset with `Reset()` method
- Each test should be independent

## Contributing

When adding new features:

1. Write unit tests first (TDD approach)
2. Add integration tests for end-to-end workflows
3. Consider stress tests for high-volume scenarios
4. Run race detector to verify thread safety
5. Ensure all tests pass: `make ci`

### Checklist

- [ ] Unit tests added to `test/unit/`
- [ ] Integration tests if needed in `test/integration/`
- [ ] All existing tests pass
- [ ] Race detector passes (`make test-race`)
- [ ] Coverage maintained or improved
- [ ] Documentation updated
