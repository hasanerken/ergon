# Ergon Test Suite

Comprehensive test suite for the Ergon distributed task queue system.

## Test Structure

### Unit Tests
- **`client_test.go`**: Client enqueue operations, validation, options
- **`worker_test.go`**: Worker registration, type safety, middleware
- **`options_test.go`**: Task options and configuration
- **`errors_test.go`**: Error handling, custom errors, error wrapping

### Integration Tests
- **`integration_badger_test.go`**: End-to-end tests with real BadgerDB
  - Task lifecycle (enqueue → process → complete)
  - Multiple queues with priorities
  - Retry logic and scheduling
  - Pause/resume queues
  - Middleware execution
  - Batch operations

- **`integration_mock_test.go`**: Integration tests with mock PostgreSQL
  - Task state transitions
  - Queue priority handling
  - Task listing and filtering
  - Queue management
  - Group operations

### Stress Tests
- **`stress_test.go`**: High-volume and performance tests
  - High-volume enqueue (10,000+ tasks)
  - Concurrent enqueue (100 goroutines)
  - High-throughput processing
  - Mixed workload scenarios
  - Batch operations performance
  - Queue saturation testing
  - Memory usage patterns
  - Benchmarks

### Race Condition Tests
- **`race_test.go`**: Concurrent access tests (run with `-race`)
  - Concurrent enqueue operations
  - Concurrent dequeue operations
  - Concurrent state changes
  - Worker registration races
  - Queue pause/resume races
  - Concurrent updates
  - List and modify races
  - Group operation races
  - Server start/stop races
  - Metadata access races

### Test Utilities
- **`testing.go`**: Helper functions and test utilities
  - `TestHelper`: Cleanup management
  - `NewBadgerStore`: Temporary BadgerDB for tests
  - `CreateTestWorkers`: Standard test worker registry
  - Test task types: `TestTaskArgs`, `FailingTaskArgs`, `SlowTaskArgs`
  - Wait helpers: `WaitForTaskState`, `WaitForCondition`
  - Assertion helpers

- **`store/mock/postgres_mock.go`**: In-memory PostgreSQL mock
  - Full Store interface implementation
  - Thread-safe operations
  - Task priority sorting
  - Queue management
  - Group operations
  - No external dependencies

## Running Tests

### Quick Start

```bash
# Run all unit tests
make test

# Run all tests (unit + integration + stress + race)
make test-all

# Run with coverage
make test-coverage
```

### Specific Test Categories

```bash
# Unit tests only (fast)
make test-unit

# Integration tests (requires disk I/O for BadgerDB)
make test-integration

# Stress tests (slow, high volume)
make test-stress

# Race detector tests
make test-race
```

### Advanced Testing

```bash
# Run specific test
go test -v -run TestClient_Enqueue

# Run specific integration test
go test -v -run Integration_Badger_EndToEnd

# Run specific stress test
go test -v -run Stress_HighVolume

# Run specific race test
go test -race -v -run Race_ConcurrentEnqueue

# Run benchmarks
make test-bench

# Run tests 10 times (check for flaky tests)
make test-count

# Run tests in parallel
make test-parallel
```

### Short Mode

Skip slow integration and stress tests:

```bash
go test -short ./...
```

## Test Coverage

Generate coverage report:

```bash
make test-coverage
```

This creates `coverage.html` with detailed line-by-line coverage.

Target coverage: **80%+** for core packages

## CI/CD Integration

Run all CI checks:

```bash
make ci
```

This runs:
1. Code formatting (`go fmt`)
2. Static analysis (`go vet`)
3. All tests (unit, integration, stress)
4. Race detector tests

## Test Organization

### Test Categories

1. **Unit Tests** (fast, no I/O)
   - Pure logic testing
   - Mock dependencies
   - Run with `go test -short`

2. **Integration Tests** (moderate, real storage)
   - Real BadgerDB store
   - Mock PostgreSQL store
   - End-to-end workflows
   - Skip with `-short` flag

3. **Stress Tests** (slow, high volume)
   - Performance testing
   - High concurrency
   - Memory usage
   - Skip with `-short` flag

4. **Race Tests** (moderate, requires `-race`)
   - Concurrent access patterns
   - Thread safety
   - Run separately with `-race`

### Naming Conventions

- Unit tests: `TestClient_*`, `TestWorker_*`, `TestOptions_*`
- Integration tests: `TestIntegration_Badger_*`, `TestIntegration_Mock_*`
- Stress tests: `TestStress_*`
- Race tests: `TestRace_*`
- Benchmarks: `BenchmarkEnqueue*`

## Writing New Tests

### Example Unit Test

```go
func TestClient_NewFeature(t *testing.T) {
    store := mock.NewPostgresStore()
    workers := CreateTestWorkers()
    client := NewClient(store, ClientConfig{Workers: workers})
    
    ctx := context.Background()
    
    // Test implementation
    task, err := Enqueue(client, ctx, TestTaskArgs{Message: "test"})
    AssertNoError(t, err, "enqueue failed")
    
    // Assertions
    AssertTaskState(t, store, task.ID, StatePending)
}
```

### Example Integration Test

```go
func TestIntegration_Badger_NewFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    
    helper := NewTestHelper(t)
    defer helper.Close()
    
    store := helper.NewBadgerStore()
    
    // Test implementation
    // ...
}
```

### Example Stress Test

```go
func TestStress_NewScenario(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }
    
    // High-volume test implementation
    const numTasks = 10000
    // ...
}
```

### Example Race Test

```go
func TestRace_NewConcurrency(t *testing.T) {
    var wg sync.WaitGroup
    
    // Concurrent operations
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

## Test Helpers

### Common Patterns

```go
// Create test helper
helper := NewTestHelper(t)
defer helper.Close()

// Create temporary BadgerDB store
store := helper.NewBadgerStore()

// Create test workers
workers := CreateTestWorkers()

// Wait for task state
task := WaitForTaskState(t, store, taskID, StateCompleted, 5*time.Second)

// Wait for condition
WaitForCondition(t, func() bool {
    return completed >= expected
}, 10*time.Second, "tasks to complete")

// Assertions
AssertNoError(t, err, "operation failed")
AssertError(t, err, "should have failed")
AssertTaskState(t, store, taskID, StateCompleted)
AssertTaskCount(t, store, filter, expectedCount)
```

## Performance Benchmarks

Current benchmarks (reference):

```
BenchmarkEnqueue-8              50000     30000 ns/op
BenchmarkEnqueueParallel-8     100000     15000 ns/op
BenchmarkDequeue-8              40000     35000 ns/op
BenchmarkEnqueueMany-8           5000    250000 ns/op (100 tasks/batch)
```

Run benchmarks:

```bash
make test-bench
```

Compare benchmarks:

```bash
make test-bench-compare
# Make changes
go test -bench=. -benchmem -run=^$$ ./... > bench-new.txt
benchcmp bench.txt bench-new.txt
```

## Debugging Failed Tests

### Enable verbose output

```bash
go test -v -run FailingTest
```

### Run single test with race detector

```bash
go test -race -v -run TestRace_Specific
```

### Increase timeout for slow tests

```bash
go test -timeout 10m -run SlowTest
```

### View test coverage for specific file

```bash
go test -coverprofile=coverage.out
go tool cover -func=coverage.out | grep client.go
```

## Common Issues

### Race Detector Failures

If race detector reports issues:
1. Run specific test: `go test -race -run TestRace_Specific`
2. Review concurrent access patterns
3. Check for missing mutex locks
4. Verify atomic operations

### Flaky Tests

If tests fail intermittently:
1. Run multiple times: `go test -count=10`
2. Check for timing assumptions
3. Review wait conditions and timeouts
4. Ensure proper cleanup in defer blocks

### BadgerDB Permission Issues

If BadgerDB tests fail with permission errors:
1. Check temp directory permissions
2. Verify cleanup in `defer helper.Close()`
3. Ensure no other process is using the directory

## Contributing

When adding new features:

1. **Write unit tests first** (TDD approach)
2. **Add integration tests** for end-to-end workflows
3. **Consider stress tests** for high-volume scenarios
4. **Run race detector** to verify thread safety
5. **Update coverage** target if needed

Ensure all tests pass before submitting PR:

```bash
make ci
```
