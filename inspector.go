package ergon

import (
	"context"
	"time"
)

// TaskInspector provides read-only task inspection capabilities
type TaskInspector interface {
	// GetTaskDetails returns detailed information about a task
	GetTaskDetails(ctx context.Context, taskID string) (*TaskDetails, error)

	// ListTasks returns a paginated list of tasks matching the options
	ListTasks(ctx context.Context, opts ListOptions) (*TaskList, error)

	// CountByState returns task counts grouped by state for a queue
	CountByState(ctx context.Context, queue string) (map[TaskState]int, error)

	// CountByKind returns task counts grouped by kind
	CountByKind(ctx context.Context) (map[string]int, error)

	// GetTasksByState returns all tasks in a specific state
	GetTasksByState(ctx context.Context, queue string, state TaskState, limit int) ([]*TaskDetails, error)

	// SearchTasks searches tasks by text in payload/metadata
	SearchTasks(ctx context.Context, query SearchQuery) (*TaskList, error)
}

// TaskDetails contains comprehensive task information
type TaskDetails struct {
	// Basic task info
	Task *InternalTask

	// Computed fields
	RetryAttempts int           // Number of retry attempts so far
	NextRetryAt   *time.Time    // When the next retry is scheduled
	TimeInState   time.Duration // How long task has been in current state
	TotalDuration time.Duration // Total time from enqueue to completion
	LastError     string        // Most recent error message
	WorkerID      string        // ID of worker processing/processed this task

	// Flags
	IsStuck       bool // Task has been running too long
	IsExpired     bool // Task exceeded max retries
	CanRetry      bool // Task can be retried
	CanReschedule bool // Task can be rescheduled
}

// ListOptions provides filtering and pagination for task lists
type ListOptions struct {
	// Filters
	Queue    string
	State    TaskState
	States   []TaskState
	Kind     string
	Kinds    []string
	Priority *int

	// Time filters
	EnqueuedAfter  *time.Time
	EnqueuedBefore *time.Time
	CompletedAfter *time.Time
	ScheduledAfter *time.Time

	// Metadata filters (simple key-value matching)
	MetadataFilters map[string]interface{}

	// Text search
	SearchText string

	// Pagination
	Limit  int
	Offset int
	Cursor string // For cursor-based pagination (future)

	// Sorting
	OrderBy  string // "enqueued_at", "priority", "started_at", "completed_at"
	OrderDir string // "asc" or "desc"
}

// TaskList contains paginated task results
type TaskList struct {
	Tasks      []*TaskDetails
	Total      int    // Total count matching filter
	Limit      int    // Page size
	Offset     int    // Current offset
	HasMore    bool   // Whether there are more results
	NextCursor string // Cursor for next page (future)
}

// SearchQuery for searching tasks
type SearchQuery struct {
	// Search terms
	Text string // Search in payload and metadata

	// Filters
	Queue  string
	States []TaskState
	Kinds  []string

	// Time range
	TimeRange *TimeRange

	// Pagination
	Limit  int
	Offset int
}

// TimeRange specifies a time range
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// DefaultInspector provides a default implementation of TaskInspector
type DefaultInspector struct {
	store Store
}

// NewTaskInspector creates a new task inspector
func NewTaskInspector(store Store) TaskInspector {
	return &DefaultInspector{store: store}
}

// GetTaskDetails returns detailed information about a task
func (i *DefaultInspector) GetTaskDetails(ctx context.Context, taskID string) (*TaskDetails, error) {
	task, err := i.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	details := &TaskDetails{
		Task:          task,
		RetryAttempts: task.Retried,
		LastError:     task.Error,
	}

	// Calculate time in state
	now := time.Now()
	switch task.State {
	case StatePending:
		details.TimeInState = now.Sub(task.EnqueuedAt)
	case StateRunning:
		if task.StartedAt != nil {
			details.TimeInState = now.Sub(*task.StartedAt)
		}
	case StateScheduled, StateRetrying:
		if task.ScheduledAt != nil {
			details.NextRetryAt = task.ScheduledAt
		}
	case StateCompleted, StateFailed, StateCancelled:
		if task.CompletedAt != nil {
			details.TotalDuration = task.CompletedAt.Sub(task.EnqueuedAt)
		}
	default:
		// Handle any unexpected states
	}

	// Check if stuck (running too long)
	if task.State == StateRunning && task.StartedAt != nil {
		runningTime := now.Sub(*task.StartedAt)
		details.IsStuck = runningTime > task.Timeout*2
	}

	// Check if expired
	details.IsExpired = task.Retried >= task.MaxRetries

	// Check capabilities
	details.CanRetry = task.State == StateFailed && !details.IsExpired
	details.CanReschedule = task.State == StatePending || task.State == StateScheduled

	return details, nil
}

// ListTasks returns a paginated list of tasks
func (i *DefaultInspector) ListTasks(ctx context.Context, opts ListOptions) (*TaskList, error) {
	// Convert ListOptions to TaskFilter
	filter := &TaskFilter{
		Queue:    opts.Queue,
		State:    opts.State,
		States:   opts.States,
		Kind:     opts.Kind,
		Kinds:    opts.Kinds,
		Priority: opts.Priority,
		Limit:    opts.Limit,
		Offset:   opts.Offset,
		OrderBy:  opts.OrderBy,
		OrderDir: opts.OrderDir,
	}

	// Apply defaults
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	if filter.OrderBy == "" {
		filter.OrderBy = "enqueued_at"
	}
	if filter.OrderDir == "" {
		filter.OrderDir = "desc"
	}

	// Get tasks from store
	tasks, err := i.store.ListTasks(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get total count
	countFilter := &TaskFilter{
		Queue:  opts.Queue,
		State:  opts.State,
		States: opts.States,
		Kind:   opts.Kind,
		Kinds:  opts.Kinds,
	}
	total, err := i.store.CountTasks(ctx, countFilter)
	if err != nil {
		total = len(tasks) // Fallback
	}

	// Convert to TaskDetails
	details := make([]*TaskDetails, len(tasks))
	for idx, task := range tasks {
		details[idx] = &TaskDetails{
			Task:          task,
			RetryAttempts: task.Retried,
			LastError:     task.Error,
		}
	}

	return &TaskList{
		Tasks:   details,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
		HasMore: (filter.Offset + len(tasks)) < total,
	}, nil
}

// CountByState returns task counts grouped by state
func (i *DefaultInspector) CountByState(ctx context.Context, queue string) (map[TaskState]int, error) {
	counts := make(map[TaskState]int)

	states := []TaskState{
		StatePending,
		StateRunning,
		StateScheduled,
		StateRetrying,
		StateCompleted,
		StateFailed,
		StateCancelled,
		StateAggregating,
	}

	for _, state := range states {
		filter := &TaskFilter{
			Queue: queue,
			State: state,
		}
		count, err := i.store.CountTasks(ctx, filter)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			counts[state] = count
		}
	}

	return counts, nil
}

// CountByKind returns task counts grouped by kind
func (i *DefaultInspector) CountByKind(ctx context.Context) (map[string]int, error) {
	// This requires fetching all tasks and grouping by kind
	// For efficiency, this should be implemented in the store layer
	// For now, we'll return an error indicating it needs store support
	return nil, ErrNotImplemented
}

// GetTasksByState returns tasks in a specific state
func (i *DefaultInspector) GetTasksByState(ctx context.Context, queue string, state TaskState, limit int) ([]*TaskDetails, error) {
	if limit == 0 {
		limit = 100
	}

	filter := &TaskFilter{
		Queue:    queue,
		State:    state,
		Limit:    limit,
		OrderBy:  "enqueued_at",
		OrderDir: "desc",
	}

	tasks, err := i.store.ListTasks(ctx, filter)
	if err != nil {
		return nil, err
	}

	details := make([]*TaskDetails, len(tasks))
	for idx, task := range tasks {
		details[idx] = &TaskDetails{
			Task:          task,
			RetryAttempts: task.Retried,
			LastError:     task.Error,
		}
	}

	return details, nil
}

// SearchTasks searches tasks by text
func (i *DefaultInspector) SearchTasks(ctx context.Context, query SearchQuery) (*TaskList, error) {
	// Basic implementation - convert to ListOptions
	// Full-text search would need to be implemented in store layer
	opts := ListOptions{
		Queue:      query.Queue,
		States:     query.States,
		Kinds:      query.Kinds,
		SearchText: query.Text,
		Limit:      query.Limit,
		Offset:     query.Offset,
	}

	return i.ListTasks(ctx, opts)
}
