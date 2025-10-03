package ergon

import (
	"time"
)

// TaskState represents the lifecycle state of a task
type TaskState string

const (
	StatePending     TaskState = "pending"     // Waiting to be picked
	StateScheduled   TaskState = "scheduled"   // Scheduled for future
	StateAvailable   TaskState = "available"   // Ready to work
	StateRunning     TaskState = "running"     // Currently executing
	StateRetrying    TaskState = "retrying"    // Failed, will retry
	StateCompleted   TaskState = "completed"   // Successfully finished
	StateFailed      TaskState = "failed"      // Exhausted retries
	StateCancelled   TaskState = "cancelled"   // Manually cancelled
	StateAggregating TaskState = "aggregating" // Waiting in group
)

// TaskArgs must be implemented by all task argument types
type TaskArgs interface {
	Kind() string // Unique identifier for this task type
}

// Task[T] provides type-safe access to task data
type Task[T TaskArgs] struct {
	ID          string
	Kind        string // Same as T.Kind()
	Queue       string
	State       TaskState
	Priority    int
	MaxRetries  int
	Retried     int
	Timeout     time.Duration
	ScheduledAt *time.Time
	EnqueuedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	Error       string
	Result      []byte
	Metadata    map[string]interface{}

	Args T // Type-safe arguments - this is what workers use!

	// Internal fields
	uniqueKey      string
	groupKey       string
	rateLimitScope string
}

// InternalTask is used internally by the queue system
// Workers never see this - they only work with typed Task[T]
type InternalTask struct {
	ID          string
	Kind        string
	Queue       string
	State       TaskState
	Priority    int
	MaxRetries  int
	Retried     int
	Timeout     time.Duration
	ScheduledAt *time.Time
	EnqueuedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	Error       string
	Result      []byte
	Payload     []byte // Raw JSON payload
	Metadata    map[string]interface{}

	// Feature fields
	UniqueKey      string
	GroupKey       string
	RateLimitScope string

	// Recurring task fields
	Recurring    bool
	CronSchedule string
	Interval     time.Duration
}

// ToInternal converts a typed task to internal representation
func ToInternal[T TaskArgs](task *Task[T], payload []byte) *InternalTask {
	return &InternalTask{
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
		Payload:        payload,
		Metadata:       task.Metadata,
		UniqueKey:      task.uniqueKey,
		GroupKey:       task.groupKey,
		RateLimitScope: task.rateLimitScope,
	}
}
