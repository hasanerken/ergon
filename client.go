package ergon

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hasanerken/ergon/internal/jsonutil"
)

// Client enqueues tasks to the queue
type Client struct {
	store   Store
	config  ClientConfig
	workers *Workers // Optional: for validation at enqueue time
}

// ClientConfig configures the client
type ClientConfig struct {
	DefaultQueue   string
	DefaultRetries int
	DefaultTimeout time.Duration
	Workers        *Workers // Optional: validates task kinds at enqueue
}

// NewClient creates a new queue client
func NewClient(store Store, cfg ClientConfig) *Client {
	// Apply defaults
	if cfg.DefaultQueue == "" {
		cfg.DefaultQueue = "default"
	}
	if cfg.DefaultRetries == 0 {
		cfg.DefaultRetries = 25
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}

	return &Client{
		store:   store,
		config:  cfg,
		workers: cfg.Workers,
	}
}

// Enqueue adds a typed task to the queue (type-safe!)
func Enqueue[T TaskArgs](c *Client, ctx context.Context, args T, opts ...Option) (*Task[T], error) {
	kind := args.Kind()

	// Optional: Validate worker is registered (fail fast)
	if c.workers != nil {
		if !c.workers.Has(kind) {
			return nil, fmt.Errorf("%w: %s", ErrWorkerNotFound, kind)
		}
	}

	// Marshal args
	payload, err := jsonutil.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task args: %w", err)
	}

	// Build config
	cfg := newTaskConfig()
	cfg.queue = c.config.DefaultQueue
	cfg.maxRetries = c.config.DefaultRetries
	cfg.timeout = c.config.DefaultTimeout

	for _, opt := range opts {
		opt(cfg)
	}

	// Generate unique key if enabled
	var uniqueKey string
	if cfg.unique {
		uniqueKey = computeUniqueKey(kind, payload, cfg.queue, cfg)
	}

	// Generate partition key if needed
	var rateLimitScope string
	if cfg.rateLimitScope != "" {
		rateLimitScope = cfg.rateLimitScope
		if cfg.partitionKeyFunc != nil || len(cfg.partitionByArgs) > 0 {
			rateLimitScope += ":" + computePartitionKey(payload, cfg)
		}
	}

	// Determine initial state
	state := StatePending
	if cfg.scheduledAt != nil && cfg.scheduledAt.After(time.Now()) {
		state = StateScheduled
	}
	if cfg.groupKey != "" {
		state = StateAggregating
	}

	// Create internal task
	now := time.Now()
	internalTask := &InternalTask{
		ID:             generateID(),
		Kind:           kind,
		Queue:          cfg.queue,
		State:          state,
		Priority:       cfg.priority,
		MaxRetries:     cfg.maxRetries,
		Timeout:        cfg.timeout,
		ScheduledAt:    cfg.scheduledAt,
		EnqueuedAt:     now,
		Payload:        payload,
		Metadata:       cfg.metadata,
		UniqueKey:      uniqueKey,
		GroupKey:       cfg.groupKey,
		RateLimitScope: rateLimitScope,
		Recurring:      cfg.recurring,
		CronSchedule:   cfg.cronSchedule,
		Interval:       cfg.interval,
	}

	// Store task
	if cfg.groupKey != "" {
		// Add to group for aggregation
		if err := c.store.AddToGroup(ctx, cfg.groupKey, internalTask); err != nil {
			return nil, err
		}
	} else {
		// Normal enqueue
		if err := c.store.Enqueue(ctx, internalTask); err != nil {
			return nil, err
		}
	}

	// Return typed task
	return &Task[T]{
		ID:             internalTask.ID,
		Kind:           kind,
		Queue:          cfg.queue,
		State:          state,
		Priority:       cfg.priority,
		MaxRetries:     cfg.maxRetries,
		Timeout:        cfg.timeout,
		ScheduledAt:    cfg.scheduledAt,
		EnqueuedAt:     now,
		Args:           args,
		Metadata:       cfg.metadata,
		uniqueKey:      uniqueKey,
		groupKey:       cfg.groupKey,
		rateLimitScope: rateLimitScope,
	}, nil
}

// EnqueueTx adds a typed task within a transaction
func EnqueueTx[T TaskArgs](c *Client, ctx context.Context, tx Tx, args T, opts ...Option) (*Task[T], error) {
	kind := args.Kind()

	// Optional: Validate worker is registered
	if c.workers != nil {
		if !c.workers.Has(kind) {
			return nil, fmt.Errorf("%w: %s", ErrWorkerNotFound, kind)
		}
	}

	// Marshal args
	payload, err := jsonutil.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task args: %w", err)
	}

	// Build config
	cfg := newTaskConfig()
	cfg.queue = c.config.DefaultQueue
	cfg.maxRetries = c.config.DefaultRetries
	cfg.timeout = c.config.DefaultTimeout

	for _, opt := range opts {
		opt(cfg)
	}

	// Generate unique key if enabled
	var uniqueKey string
	if cfg.unique {
		uniqueKey = computeUniqueKey(kind, payload, cfg.queue, cfg)
	}

	// Determine initial state
	state := StatePending
	if cfg.scheduledAt != nil && cfg.scheduledAt.After(time.Now()) {
		state = StateScheduled
	}

	// Create internal task
	now := time.Now()
	internalTask := &InternalTask{
		ID:          generateID(),
		Kind:        kind,
		Queue:       cfg.queue,
		State:       state,
		Priority:    cfg.priority,
		MaxRetries:  cfg.maxRetries,
		Timeout:     cfg.timeout,
		ScheduledAt: cfg.scheduledAt,
		EnqueuedAt:  now,
		Payload:     payload,
		Metadata:    cfg.metadata,
		UniqueKey:   uniqueKey,
		GroupKey:    cfg.groupKey,
	}

	// Store task in transaction
	if err := c.store.EnqueueTx(ctx, tx, internalTask); err != nil {
		return nil, err
	}

	// Return typed task
	return &Task[T]{
		ID:          internalTask.ID,
		Kind:        kind,
		Queue:       cfg.queue,
		State:       state,
		Priority:    cfg.priority,
		MaxRetries:  cfg.maxRetries,
		Timeout:     cfg.timeout,
		ScheduledAt: cfg.scheduledAt,
		EnqueuedAt:  now,
		Args:        args,
		Metadata:    cfg.metadata,
		uniqueKey:   uniqueKey,
		groupKey:    cfg.groupKey,
	}, nil
}

// EnqueueMany adds multiple typed tasks (all same type)
func EnqueueMany[T TaskArgs](c *Client, ctx context.Context, argsList []T, opts ...Option) ([]*Task[T], error) {
	if len(argsList) == 0 {
		return nil, nil
	}

	// Build config once
	cfg := newTaskConfig()
	cfg.queue = c.config.DefaultQueue
	cfg.maxRetries = c.config.DefaultRetries
	cfg.timeout = c.config.DefaultTimeout

	for _, opt := range opts {
		opt(cfg)
	}

	internalTasks := make([]*InternalTask, 0, len(argsList))
	tasks := make([]*Task[T], 0, len(argsList))
	now := time.Now()

	for _, args := range argsList {
		kind := args.Kind()

		// Optional: Validate worker
		if c.workers != nil {
			if !c.workers.Has(kind) {
				return nil, fmt.Errorf("%w: %s", ErrWorkerNotFound, kind)
			}
		}

		// Marshal args
		payload, err := jsonutil.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal task args: %w", err)
		}

		// Generate unique key if enabled
		var uniqueKey string
		if cfg.unique {
			uniqueKey = computeUniqueKey(kind, payload, cfg.queue, cfg)
		}

		// Determine state
		state := StatePending
		if cfg.scheduledAt != nil && cfg.scheduledAt.After(time.Now()) {
			state = StateScheduled
		}

		internalTask := &InternalTask{
			ID:          generateID(),
			Kind:        kind,
			Queue:       cfg.queue,
			State:       state,
			Priority:    cfg.priority,
			MaxRetries:  cfg.maxRetries,
			Timeout:     cfg.timeout,
			ScheduledAt: cfg.scheduledAt,
			EnqueuedAt:  now,
			Payload:     payload,
			Metadata:    cfg.metadata,
			UniqueKey:   uniqueKey,
			GroupKey:    cfg.groupKey,
		}

		internalTasks = append(internalTasks, internalTask)
		tasks = append(tasks, &Task[T]{
			ID:          internalTask.ID,
			Kind:        kind,
			Queue:       cfg.queue,
			State:       state,
			Priority:    cfg.priority,
			MaxRetries:  cfg.maxRetries,
			Timeout:     cfg.timeout,
			ScheduledAt: cfg.scheduledAt,
			EnqueuedAt:  now,
			Args:        args,
			Metadata:    cfg.metadata,
			uniqueKey:   uniqueKey,
			groupKey:    cfg.groupKey,
		})
	}

	// Store all tasks
	if err := c.store.EnqueueMany(ctx, internalTasks); err != nil {
		return nil, err
	}

	return tasks, nil
}

// CancelTask cancels a task
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	return c.store.MarkCancelled(ctx, taskID)
}

// DeleteTask deletes a task
func (c *Client) DeleteTask(ctx context.Context, taskID string) error {
	return c.store.DeleteTask(ctx, taskID)
}

// GetTask retrieves a task by ID (untyped)
func (c *Client) GetTask(ctx context.Context, taskID string) (*InternalTask, error) {
	return c.store.GetTask(ctx, taskID)
}

// PauseQueue pauses a queue
func (c *Client) PauseQueue(ctx context.Context, queue string) error {
	return c.store.PauseQueue(ctx, queue)
}

// ResumeQueue resumes a paused queue
func (c *Client) ResumeQueue(ctx context.Context, queue string) error {
	return c.store.ResumeQueue(ctx, queue)
}

// Close closes the client
func (c *Client) Close() error {
	return c.store.Close()
}

// generateID generates a unique task ID using UUID v7 (time-ordered)
// UUID v7 provides better database performance due to sequential ordering
func generateID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to v4 if v7 fails (shouldn't happen)
		return uuid.New().String()
	}
	return id.String()
}
