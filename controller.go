package ergon

import (
	"context"
	"fmt"
	"time"
)

// TaskController provides task control operations
type TaskController interface {
	// Cancel cancels a pending or scheduled task
	Cancel(ctx context.Context, taskID string) error

	// Delete removes a task from the queue
	Delete(ctx context.Context, taskID string) error

	// RetryNow retries a failed task immediately
	RetryNow(ctx context.Context, taskID string) error

	// Reschedule changes when a task will run
	Reschedule(ctx context.Context, taskID string, newTime time.Time) error

	// UpdatePriority changes the priority of a pending task
	UpdatePriority(ctx context.Context, taskID string, priority int) error

	// UpdateMetadata updates task metadata
	UpdateMetadata(ctx context.Context, taskID string, metadata map[string]interface{}) error

	// ExtendTimeout extends the timeout for a running task
	ExtendTimeout(ctx context.Context, taskID string, duration time.Duration) error

	// SetMaxRetries updates the max retry count for a task
	SetMaxRetries(ctx context.Context, taskID string, maxRetries int) error
}

// DefaultController provides a default implementation of TaskController
type DefaultController struct {
	store Store
}

// NewTaskController creates a new task controller
func NewTaskController(store Store) TaskController {
	return &DefaultController{store: store}
}

// Cancel cancels a pending or scheduled task
func (c *DefaultController) Cancel(ctx context.Context, taskID string) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Only cancel if in cancellable state
	if !isStateCancellable(task.State) {
		return fmt.Errorf("cannot cancel task in state %s", task.State)
	}

	return c.store.MarkCancelled(ctx, taskID)
}

// Delete removes a task from the queue
func (c *DefaultController) Delete(ctx context.Context, taskID string) error {
	return c.store.DeleteTask(ctx, taskID)
}

// RetryNow retries a failed task immediately
func (c *DefaultController) RetryNow(ctx context.Context, taskID string) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Can only retry failed tasks
	if task.State != StateFailed {
		return fmt.Errorf("cannot retry task in state %s, must be failed", task.State)
	}

	// Check if max retries exceeded
	if task.Retried >= task.MaxRetries {
		return fmt.Errorf("task has exceeded max retries (%d/%d)", task.Retried, task.MaxRetries)
	}

	// Move to retrying state with immediate execution
	return c.store.MarkRetrying(ctx, taskID, time.Now())
}

// Reschedule changes when a task will run
func (c *DefaultController) Reschedule(ctx context.Context, taskID string, newTime time.Time) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Can only reschedule pending or scheduled tasks
	if !isStateReschedulable(task.State) {
		return fmt.Errorf("cannot reschedule task in state %s", task.State)
	}

	// Update scheduled time
	update := &TaskUpdate{
		ScheduledAt: &newTime,
	}

	// If rescheduling to future, set state to scheduled
	if newTime.After(time.Now()) {
		state := StateScheduled
		update.State = &state
	} else {
		// If rescheduling to past/now, set to pending
		state := StatePending
		update.State = &state
		update.ScheduledAt = nil
	}

	return c.store.UpdateTask(ctx, taskID, update)
}

// UpdatePriority changes the priority of a pending task
func (c *DefaultController) UpdatePriority(ctx context.Context, taskID string, priority int) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Can only update priority for pending/scheduled tasks
	if task.State != StatePending && task.State != StateScheduled {
		return fmt.Errorf("cannot update priority for task in state %s", task.State)
	}

	update := &TaskUpdate{
		Priority: &priority,
	}

	return c.store.UpdateTask(ctx, taskID, update)
}

// UpdateMetadata updates task metadata
func (c *DefaultController) UpdateMetadata(ctx context.Context, taskID string, metadata map[string]interface{}) error {
	update := &TaskUpdate{
		Metadata: metadata,
	}

	return c.store.UpdateTask(ctx, taskID, update)
}

// ExtendTimeout extends the timeout for a running task
func (c *DefaultController) ExtendTimeout(ctx context.Context, taskID string, duration time.Duration) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Can only extend timeout for running tasks
	if task.State != StateRunning {
		return fmt.Errorf("cannot extend timeout for task in state %s, must be running", task.State)
	}

	// Extend the lease
	return c.store.ExtendLease(ctx, taskID, duration)
}

// SetMaxRetries updates the max retry count for a task
func (c *DefaultController) SetMaxRetries(ctx context.Context, taskID string, maxRetries int) error {
	// Get task to check state
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Can set max retries for any non-completed task
	if task.State == StateCompleted {
		return fmt.Errorf("cannot update max retries for completed task")
	}

	update := &TaskUpdate{
		MaxRetries: &maxRetries,
	}

	return c.store.UpdateTask(ctx, taskID, update)
}

// Helper functions

func isStateCancellable(state TaskState) bool {
	return state == StatePending ||
		state == StateScheduled ||
		state == StateRetrying ||
		state == StateAggregating
}

func isStateReschedulable(state TaskState) bool {
	return state == StatePending ||
		state == StateScheduled
}
