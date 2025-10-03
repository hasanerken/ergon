package ergon

import (
	"context"
	"time"
)

// Store defines the storage backend interface
type Store interface {
	// Task operations
	Enqueue(ctx context.Context, task *InternalTask) error
	EnqueueMany(ctx context.Context, tasks []*InternalTask) error
	EnqueueTx(ctx context.Context, tx Tx, task *InternalTask) error

	Dequeue(ctx context.Context, queues []string, workerID string) (*InternalTask, error)

	GetTask(ctx context.Context, taskID string) (*InternalTask, error)
	ListTasks(ctx context.Context, filter *TaskFilter) ([]*InternalTask, error)
	CountTasks(ctx context.Context, filter *TaskFilter) (int, error)

	UpdateTask(ctx context.Context, taskID string, updates *TaskUpdate) error
	DeleteTask(ctx context.Context, taskID string) error

	// State transitions
	MarkRunning(ctx context.Context, taskID string, workerID string) error
	MarkCompleted(ctx context.Context, taskID string, result []byte) error
	MarkFailed(ctx context.Context, taskID string, err error) error
	MarkRetrying(ctx context.Context, taskID string, nextRetry time.Time) error
	MarkCancelled(ctx context.Context, taskID string) error

	// Scheduling
	MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error)

	// Queue management
	ListQueues(ctx context.Context) ([]*QueueInfo, error)
	GetQueueInfo(ctx context.Context, queue string) (*QueueInfo, error)
	PauseQueue(ctx context.Context, queue string) error
	ResumeQueue(ctx context.Context, queue string) error

	// Aggregation
	AddToGroup(ctx context.Context, groupKey string, task *InternalTask) error
	GetGroup(ctx context.Context, groupKey string) ([]*InternalTask, error)
	ConsumeGroup(ctx context.Context, groupKey string) ([]*InternalTask, error)

	// Maintenance
	ArchiveTasks(ctx context.Context, before time.Time) (int, error)
	DeleteArchivedTasks(ctx context.Context, before time.Time) (int, error)
	RecoverStuckTasks(ctx context.Context, timeout time.Duration) (int, error)
	ExtendLease(ctx context.Context, taskID string, duration time.Duration) error

	// Subscriptions (optional, not all backends support)
	Subscribe(ctx context.Context, events ...EventType) (<-chan *Event, error)

	// Leader election (optional)
	TryAcquireLease(ctx context.Context, leaseKey string, ttl time.Duration) (*Lease, error)
	RenewLease(ctx context.Context, lease *Lease) error
	ReleaseLease(ctx context.Context, lease *Lease) error

	// Transactions (optional)
	BeginTx(ctx context.Context) (Tx, error)

	// Lifecycle
	Close() error
	Ping(ctx context.Context) error
}

// Tx represents a transaction
type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TaskFilter defines filters for listing tasks
type TaskFilter struct {
	Queue    string
	State    TaskState
	States   []TaskState
	Kind     string
	Kinds    []string
	Priority *int
	Limit    int
	Offset   int
	OrderBy  string
	OrderDir string // "asc" or "desc"
}

// TaskUpdate defines fields that can be updated
type TaskUpdate struct {
	State       *TaskState
	Priority    *int
	MaxRetries  *int
	Timeout     *time.Duration
	ScheduledAt *time.Time
	Metadata    map[string]interface{}
}

// QueueInfo contains queue statistics
type QueueInfo struct {
	Name           string
	Paused         bool
	PendingCount   int
	RunningCount   int
	ScheduledCount int
	RetryingCount  int
	CompletedCount int
	FailedCount    int
	ProcessedTotal int64
	FailedTotal    int64
}

// EventType represents different task events
type EventType string

const (
	EventTaskEnqueued  EventType = "task.enqueued"
	EventTaskStarted   EventType = "task.started"
	EventTaskCompleted EventType = "task.completed"
	EventTaskFailed    EventType = "task.failed"
	EventTaskCancelled EventType = "task.cancelled"
	EventTaskRetrying  EventType = "task.retrying"
	EventTaskSnoozed   EventType = "task.snoozed"
)

// Event represents a task event
type Event struct {
	Type      EventType
	Task      *InternalTask
	Timestamp time.Time
	Metadata  map[string]interface{}
}

// Lease represents a distributed lease
type Lease struct {
	Key        string
	Value      string
	TTL        time.Duration
	AcquiredAt time.Time
}
