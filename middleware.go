package ergon

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"
)

// MiddlewareFunc wraps task execution
type MiddlewareFunc func(next WorkerFunc) WorkerFunc

// WorkerFunc is the internal execution function
type WorkerFunc func(ctx context.Context, task *InternalTask) error

// LoggingMiddleware logs task execution
func LoggingMiddleware() MiddlewareFunc {
	return func(next WorkerFunc) WorkerFunc {
		return func(ctx context.Context, task *InternalTask) error {
			start := time.Now()
			log.Printf("[Queue] Starting task %s (kind=%s, queue=%s)", task.ID, task.Kind, task.Queue)

			err := next(ctx, task)

			duration := time.Since(start)
			if err != nil {
				log.Printf("[Queue] Task %s failed: %v (duration=%v)", task.ID, err, duration)
			} else {
				log.Printf("[Queue] Task %s completed successfully (duration=%v)", task.ID, duration)
			}

			return err
		}
	}
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware() MiddlewareFunc {
	return func(next WorkerFunc) WorkerFunc {
		return func(ctx context.Context, task *InternalTask) (err error) {
			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()
					log.Printf("[Queue] PANIC in task %s: %v\n%s", task.ID, r, stack)
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next(ctx, task)
		}
	}
}

// MetricsMiddleware records metrics (placeholder - integrate with your metrics package)
func MetricsMiddleware() MiddlewareFunc {
	return func(next WorkerFunc) WorkerFunc {
		return func(ctx context.Context, task *InternalTask) error {
			start := time.Now()

			// Record task started
			// metrics.TaskStarted(task.Kind, task.Queue)

			err := next(ctx, task)

			_ = time.Since(start) // duration - used for metrics when enabled

			// Record task completed/failed
			if err != nil {
				// metrics.TaskFailed(task.Kind, task.Queue, duration)
			} else {
				// metrics.TaskCompleted(task.Kind, task.Queue, duration)
			}

			return err
		}
	}
}

// RetryMiddleware handles retry logic (this would be built into the server)
func RetryMiddleware(retryDelayFunc func(*InternalTask, int, error) time.Duration) MiddlewareFunc {
	return func(next WorkerFunc) WorkerFunc {
		return func(ctx context.Context, task *InternalTask) error {
			err := next(ctx, task)
			if err != nil && task.Retried < task.MaxRetries {
				// Calculate retry delay
				if retryDelayFunc != nil {
					_ = retryDelayFunc(task, task.Retried+1, err)
					// Note: Actual retry scheduling is handled by the server
				}
			}
			return err
		}
	}
}
