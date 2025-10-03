package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkers_AddWorker(t *testing.T) {
	workers := ergon.NewWorkers()

	t.Run("add simple worker", func(t *testing.T) {
		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
			return nil
		})

		if !workers.Has("test_task") {
			t.Error("worker not registered")
		}
	})

	t.Run("panic on duplicate worker", func(t *testing.T) {
		workers := ergon.NewWorkers()

		// Add first worker
		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
			return nil
		})

		// Adding duplicate should panic
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on duplicate worker")
			}
		}()

		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
			return nil
		})
	})

	t.Run("get worker entry", func(t *testing.T) {
		workers := ergon.NewWorkers()

		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
			return nil
		})

		entry, ok := workers.Get("test_task")
		if !ok {
			t.Fatal("worker not found")
		}

		if entry.kind != "test_task" {
			t.Errorf("expected kind 'test_task', got %s", entry.kind)
		}
	})

	t.Run("list worker kinds", func(t *testing.T) {
		workers := ergon.NewWorkers()

		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
			return nil
		})

		ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[FailingTaskArgs]) error {
			return task.Args.Error
		})

		kinds := workers.Kinds()
		if len(kinds) != 2 {
			t.Errorf("expected 2 kinds, got %d", len(kinds))
		}

		hasTestTask := false
		hasFailingTask := false
		for _, kind := range kinds {
			if kind == "test_task" {
				hasTestTask = true
			}
			if kind == "failing_task" {
				hasFailingTask = true
			}
		}

		if !hasTestTask || !hasFailingTask {
			t.Error("missing expected worker kinds")
		}
	})
}

type TimeoutWorker struct {
	ergon.WorkerDefaults[TestTaskArgs]
	timeout time.Duration
}

func (w *TimeoutWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
	return nil
}

func (w *TimeoutWorker) Timeout(task *ergon.Task[TestTaskArgs]) time.Duration {
	return w.timeout
}

func TestWorker_WithTimeout(t *testing.T) {
	workers := ergon.NewWorkers()
	worker := &TimeoutWorker{timeout: 5 * time.Second}
	ergon.AddWorker(workers, worker)

	entry, ok := workers.Get("test_task")
	if !ok {
		t.Fatal("worker not found")
	}

	if entry.timeout == nil {
		t.Fatal("timeout function not set")
	}

	// Create a test task
	task := &ergon.InternalTask{
		Kind:    "test_task",
		Payload: []byte(`{"message":"test"}`),
	}

	timeout := entry.timeout(task)
	if timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", timeout)
	}
}

func TestWorker_WithRetryProvider(t *testing.T) {
	type RetryWorker struct {
		ergon.WorkerDefaults[TestTaskArgs]
	}

	func (w *RetryWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		return errors.New("test error")
	}

	func (w *RetryWorker) MaxRetries(task *ergon.Task[TestTaskArgs]) int {
		return 5
	}

	func (w *RetryWorker) RetryDelay(task *ergon.Task[TestTaskArgs], attempt int, err error) time.Duration {
		return time.Duration(attempt) * time.Second
	}

	workers := ergon.NewWorkers()
	worker := &RetryWorker{}
	ergon.AddWorker(workers, worker)

	entry, ok := workers.Get("test_task")
	if !ok {
		t.Fatal("worker not found")
	}

	if entry.maxRetries == nil {
		t.Fatal("maxRetries function not set")
	}

	if entry.retryDelay == nil {
		t.Fatal("retryDelay function not set")
	}

	// Create a test task
	task := &ergon.InternalTask{
		Kind:    "test_task",
		Payload: []byte(`{"message":"test"}`),
	}

	maxRetries := entry.maxRetries(task)
	if maxRetries != 5 {
		t.Errorf("expected max retries 5, got %d", maxRetries)
	}

	delay := entry.retryDelay(task, 3, errors.New("test"))
	if delay != 3*time.Second {
		t.Errorf("expected delay 3s, got %v", delay)
	}
}

func TestWorker_WithMiddleware(t *testing.T) {
	type MiddlewareWorker struct {
		ergon.WorkerDefaults[TestTaskArgs]
		executed bool
	}

	func (w *MiddlewareWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		w.executed = true
		return nil
	}

	func (w *MiddlewareWorker) Middleware() []ergon.MiddlewareFunc {
		return []ergon.MiddlewareFunc{
			func(next ergon.WorkerFunc) ergon.WorkerFunc {
				return func(ctx context.Context, task *ergon.InternalTask) error {
					// Add custom header or something
					return next(ctx, task)
				}
			},
		}
	}

	workers := ergon.NewWorkers()
	worker := &MiddlewareWorker{}
	ergon.AddWorker(workers, worker)

	entry, ok := workers.Get("test_task")
	if !ok {
		t.Fatal("worker not found")
	}

	if len(entry.middleware) != 1 {
		t.Errorf("expected 1 middleware, got %d", len(entry.middleware))
	}
}

func Testergon.WorkerFunc(t *testing.T) {
	executed := false

	fn := ergon.WorkFunc[TestTaskArgs](func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		executed = true
		if task.Args.Message != "test" {
			t.Error("task args not passed correctly")
		}
		return nil
	})

	task := &ergon.Task[TestTaskArgs]{
		Args: TestTaskArgs{Message: "test"},
	}

	err := fn.Work(context.Background(), task)
	AssertNoError(t, err, "work func failed")

	if !executed {
		t.Error("work func not executed")
	}
}

func TestWorker_TypeSafety(t *testing.T) {
	workers := ergon.NewWorkers()

	// Add typed worker
	ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
		// This should compile and provide type-safe access to Args
		_ = task.Args.Message
		return nil
	})

	// Verify type safety at runtime
	entry, _ := workers.Get("test_task")

	task := &ergon.InternalTask{
		Kind:    "test_task",
		Payload: []byte(`{"message":"hello"}`),
	}

	err := entry.execute(context.Background(), task)
	AssertNoError(t, err, "execute failed")
}

func TestWorker_ConcurrentRegistration(t *testing.T) {
	workers := ergon.NewWorkers()

	// Try to register workers concurrently (only first should succeed)
	done := make(chan bool, 10)
	panics := 0

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					panics++
				}
				done <- true
			}()

			type ConcurrentTask struct {
				ID int
			}
			func (ConcurrentTask) Kind() string { return "concurrent" }

			ergon.AddWorkerFunc(workers, func(ctx context.Context, task *ergon.Task[ConcurrentTask]) error {
				return nil
			})
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 9 panics (only first registration succeeds)
	if panics != 9 {
		t.Errorf("expected 9 panics, got %d", panics)
	}

	if !workers.Has("concurrent") {
		t.Error("worker should be registered")
	}
}
