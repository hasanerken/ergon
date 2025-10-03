package ergon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Worker processes tasks of a specific type
type Worker[T TaskArgs] interface {
	Work(ctx context.Context, task *Task[T]) error
}

// WorkerDefaults provides default implementations for optional methods
type WorkerDefaults[T TaskArgs] struct{}

func (w WorkerDefaults[T]) Work(ctx context.Context, task *Task[T]) error {
	panic("Work method must be implemented")
}

// TimeoutProvider allows workers to specify custom timeout
type TimeoutProvider[T TaskArgs] interface {
	Timeout(task *Task[T]) time.Duration
}

// RetryProvider allows workers to customize retry behavior
type RetryProvider[T TaskArgs] interface {
	MaxRetries(task *Task[T]) int
	RetryDelay(task *Task[T], attempt int, err error) time.Duration
}

// MiddlewareProvider allows workers to add their own middleware
type MiddlewareProvider interface {
	Middleware() []MiddlewareFunc
}

// WorkFunc is a function adapter for simple workers
type WorkFunc[T TaskArgs] func(ctx context.Context, task *Task[T]) error

func (f WorkFunc[T]) Work(ctx context.Context, task *Task[T]) error {
	return f(ctx, task)
}

// Workers holds all registered workers
type Workers struct {
	mu       sync.RWMutex
	registry map[string]*workerEntry
}

type workerEntry struct {
	kind       string
	execute    WorkerFunc
	timeout    func(*InternalTask) time.Duration
	maxRetries func(*InternalTask) int
	retryDelay func(*InternalTask, int, error) time.Duration
	middleware []MiddlewareFunc
}

// NewWorkers creates a new worker registry
func NewWorkers() *Workers {
	return &Workers{
		registry: make(map[string]*workerEntry),
	}
}

// AddWorker registers a typed worker
func AddWorker[T TaskArgs](w *Workers, worker Worker[T]) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Get task kind from args type
	var args T
	kind := args.Kind()

	if _, exists := w.registry[kind]; exists {
		panic(fmt.Sprintf("worker for kind %q already registered", kind))
	}

	entry := &workerEntry{
		kind:    kind,
		execute: wrapWorker(worker),
	}

	// Extract optional timeout
	if tp, ok := any(worker).(TimeoutProvider[T]); ok {
		entry.timeout = func(task *InternalTask) time.Duration {
			typedTask := unmarshalTask[T](task)
			return tp.Timeout(typedTask)
		}
	}

	// Extract optional retry config
	if rp, ok := any(worker).(RetryProvider[T]); ok {
		entry.maxRetries = func(task *InternalTask) int {
			typedTask := unmarshalTask[T](task)
			return rp.MaxRetries(typedTask)
		}
		entry.retryDelay = func(task *InternalTask, attempt int, err error) time.Duration {
			typedTask := unmarshalTask[T](task)
			return rp.RetryDelay(typedTask, attempt, err)
		}
	}

	// Extract optional middleware
	if mp, ok := any(worker).(MiddlewareProvider); ok {
		entry.middleware = mp.Middleware()
	}

	w.registry[kind] = entry
}

// AddWorkerFunc registers a work function
func AddWorkerFunc[T TaskArgs](w *Workers, fn WorkFunc[T]) {
	AddWorker(w, fn)
}

// wrapWorker converts typed worker to internal WorkerFunc
func wrapWorker[T TaskArgs](worker Worker[T]) WorkerFunc {
	return func(ctx context.Context, task *InternalTask) error {
		typedTask := unmarshalTask[T](task)
		return worker.Work(ctx, typedTask)
	}
}

// unmarshalTask converts internal task to typed task
func unmarshalTask[T TaskArgs](task *InternalTask) *Task[T] {
	var args T
	if err := json.Unmarshal(task.Payload, &args); err != nil {
		// This should never happen if validation is done at enqueue time
		panic(fmt.Sprintf("failed to unmarshal task payload for kind %q: %v", task.Kind, err))
	}

	return &Task[T]{
		ID:             task.ID,
		Kind:           task.Kind,
		Queue:          task.Queue,
		State:          task.State,
		Priority:       task.Priority,
		MaxRetries:     task.MaxRetries,
		Retried:        task.Retried,
		Timeout:        task.Timeout,
		ScheduledAt:    task.ScheduledAt,
		EnqueuedAt:     task.EnqueuedAt,
		StartedAt:      task.StartedAt,
		CompletedAt:    task.CompletedAt,
		Error:          task.Error,
		Result:         task.Result,
		Args:           args,
		Metadata:       task.Metadata,
		uniqueKey:      task.UniqueKey,
		groupKey:       task.GroupKey,
		rateLimitScope: task.RateLimitScope,
	}
}

// Get retrieves worker entry for a kind
func (w *Workers) Get(kind string) (*workerEntry, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	entry, ok := w.registry[kind]
	return entry, ok
}

// Kinds returns all registered worker kinds
func (w *Workers) Kinds() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	kinds := make([]string, 0, len(w.registry))
	for k := range w.registry {
		kinds = append(kinds, k)
	}
	return kinds
}

// Has checks if a worker is registered for the given kind
func (w *Workers) Has(kind string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.registry[kind]
	return ok
}
