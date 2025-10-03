package ergon

import (
	"context"
	"fmt"
	"time"
)

// BatchController provides bulk task operations
type BatchController interface {
	// CancelMany cancels multiple tasks by ID
	CancelMany(ctx context.Context, taskIDs []string) (int, error)

	// DeleteMany deletes multiple tasks by ID
	DeleteMany(ctx context.Context, taskIDs []string) (int, error)

	// DeleteByFilter deletes tasks matching filter criteria
	DeleteByFilter(ctx context.Context, filter TaskFilter) (int, error)

	// RetryAllFailed retries all failed tasks of a specific kind
	RetryAllFailed(ctx context.Context, kind string) (int, error)

	// RetryByFilter retries tasks matching filter criteria
	RetryByFilter(ctx context.Context, filter TaskFilter) (int, error)

	// PurgeCompleted removes old completed tasks
	PurgeCompleted(ctx context.Context, olderThan time.Duration) (int, error)

	// PurgeFailed removes old failed tasks
	PurgeFailed(ctx context.Context, olderThan time.Duration) (int, error)

	// PurgeByFilter removes tasks matching filter criteria
	PurgeByFilter(ctx context.Context, filter TaskFilter, olderThan time.Duration) (int, error)

	// UpdatePriorityByFilter updates priority for tasks matching filter
	UpdatePriorityByFilter(ctx context.Context, filter TaskFilter, priority int) (int, error)

	// RescheduleByFilter reschedules tasks matching filter to a new time
	RescheduleByFilter(ctx context.Context, filter TaskFilter, newTime time.Time) (int, error)

	// PauseQueues pauses multiple queues
	PauseQueues(ctx context.Context, queues []string) error

	// ResumeQueues resumes multiple queues
	ResumeQueues(ctx context.Context, queues []string) error

	// CancelByFilter cancels tasks matching filter criteria
	CancelByFilter(ctx context.Context, filter TaskFilter) (int, error)
}

// BatchResult contains the result of a batch operation
type BatchResult struct {
	Processed int      // Number of tasks processed
	Succeeded int      // Number of tasks that succeeded
	Failed    int      // Number of tasks that failed
	Errors    []string // List of errors encountered
}

// DefaultBatchController implements BatchController interface
type DefaultBatchController struct {
	store      Store
	controller TaskController
}

// NewBatchController creates a new batch controller
func NewBatchController(store Store) BatchController {
	return &DefaultBatchController{
		store:      store,
		controller: NewTaskController(store),
	}
}

// CancelMany cancels multiple tasks by ID
func (b *DefaultBatchController) CancelMany(ctx context.Context, taskIDs []string) (int, error) {
	count := 0
	var lastErr error

	for _, taskID := range taskIDs {
		if err := b.controller.Cancel(ctx, taskID); err != nil {
			lastErr = err
			continue
		}
		count++
	}

	if lastErr != nil && count == 0 {
		return 0, fmt.Errorf("failed to cancel all tasks: %w", lastErr)
	}

	return count, nil
}

// DeleteMany deletes multiple tasks by ID
func (b *DefaultBatchController) DeleteMany(ctx context.Context, taskIDs []string) (int, error) {
	count := 0
	var lastErr error

	for _, taskID := range taskIDs {
		if err := b.controller.Delete(ctx, taskID); err != nil {
			lastErr = err
			continue
		}
		count++
	}

	if lastErr != nil && count == 0 {
		return 0, fmt.Errorf("failed to delete all tasks: %w", lastErr)
	}

	return count, nil
}

// DeleteByFilter deletes tasks matching filter criteria
func (b *DefaultBatchController) DeleteByFilter(ctx context.Context, filter TaskFilter) (int, error) {
	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		if err := b.store.DeleteTask(ctx, task.ID); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// RetryAllFailed retries all failed tasks of a specific kind
func (b *DefaultBatchController) RetryAllFailed(ctx context.Context, kind string) (int, error) {
	filter := TaskFilter{
		Kind:  kind,
		State: StateFailed,
	}

	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list failed tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Check if task can be retried
		if task.Retried >= task.MaxRetries {
			continue
		}

		if err := b.controller.RetryNow(ctx, task.ID); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// RetryByFilter retries tasks matching filter criteria
func (b *DefaultBatchController) RetryByFilter(ctx context.Context, filter TaskFilter) (int, error) {
	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Only retry failed tasks
		if task.State != StateFailed {
			continue
		}

		// Check if task can be retried
		if task.Retried >= task.MaxRetries {
			continue
		}

		if err := b.controller.RetryNow(ctx, task.ID); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// PurgeCompleted removes old completed tasks
func (b *DefaultBatchController) PurgeCompleted(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	filter := TaskFilter{
		State: StateCompleted,
	}

	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list completed tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Check if task is older than cutoff
		if task.CompletedAt != nil && task.CompletedAt.Before(cutoff) {
			if err := b.store.DeleteTask(ctx, task.ID); err != nil {
				continue
			}
			count++
		}
	}

	return count, nil
}

// PurgeFailed removes old failed tasks
func (b *DefaultBatchController) PurgeFailed(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	filter := TaskFilter{
		State: StateFailed,
	}

	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list failed tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Check if task is older than cutoff
		if task.CompletedAt != nil && task.CompletedAt.Before(cutoff) {
			if err := b.store.DeleteTask(ctx, task.ID); err != nil {
				continue
			}
			count++
		}
	}

	return count, nil
}

// PurgeByFilter removes tasks matching filter criteria
func (b *DefaultBatchController) PurgeByFilter(ctx context.Context, filter TaskFilter, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Check if task is older than cutoff
		var taskTime time.Time
		if task.CompletedAt != nil {
			taskTime = *task.CompletedAt
		} else if task.StartedAt != nil {
			taskTime = *task.StartedAt
		} else {
			taskTime = task.EnqueuedAt
		}

		if taskTime.Before(cutoff) {
			if err := b.store.DeleteTask(ctx, task.ID); err != nil {
				continue
			}
			count++
		}
	}

	return count, nil
}

// UpdatePriorityByFilter updates priority for tasks matching filter
func (b *DefaultBatchController) UpdatePriorityByFilter(ctx context.Context, filter TaskFilter, priority int) (int, error) {
	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Only update pending or scheduled tasks
		if task.State != StatePending && task.State != StateScheduled {
			continue
		}

		if err := b.controller.UpdatePriority(ctx, task.ID, priority); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// RescheduleByFilter reschedules tasks matching filter to a new time
func (b *DefaultBatchController) RescheduleByFilter(ctx context.Context, filter TaskFilter, newTime time.Time) (int, error) {
	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Only reschedule pending or scheduled tasks
		if task.State != StatePending && task.State != StateScheduled {
			continue
		}

		if err := b.controller.Reschedule(ctx, task.ID, newTime); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// PauseQueues pauses multiple queues
func (b *DefaultBatchController) PauseQueues(ctx context.Context, queues []string) error {
	var lastErr error

	for _, queue := range queues {
		if err := b.store.PauseQueue(ctx, queue); err != nil {
			lastErr = err
			continue
		}
	}

	return lastErr
}

// ResumeQueues resumes multiple queues
func (b *DefaultBatchController) ResumeQueues(ctx context.Context, queues []string) error {
	var lastErr error

	for _, queue := range queues {
		if err := b.store.ResumeQueue(ctx, queue); err != nil {
			lastErr = err
			continue
		}
	}

	return lastErr
}

// CancelByFilter cancels tasks matching filter criteria
func (b *DefaultBatchController) CancelByFilter(ctx context.Context, filter TaskFilter) (int, error) {
	tasks, err := b.store.ListTasks(ctx, &filter)
	if err != nil {
		return 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		// Only cancel pending or scheduled tasks
		if task.State != StatePending && task.State != StateScheduled {
			continue
		}

		if err := b.controller.Cancel(ctx, task.ID); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// BatchOperations provides convenient batch operation builders
type BatchOperations struct {
	controller BatchController
}

// NewBatchOperations creates a new batch operations helper
func NewBatchOperations(controller BatchController) *BatchOperations {
	return &BatchOperations{
		controller: controller,
	}
}

// CancelAllInQueue cancels all pending/scheduled tasks in a queue
func (b *BatchOperations) CancelAllInQueue(ctx context.Context, queue string) (int, error) {
	filter := TaskFilter{
		Queue:  queue,
		States: []TaskState{StatePending, StateScheduled},
	}
	return b.controller.CancelByFilter(ctx, filter)
}

// DeleteAllInQueue deletes all tasks in a queue
func (b *BatchOperations) DeleteAllInQueue(ctx context.Context, queue string) (int, error) {
	filter := TaskFilter{
		Queue: queue,
	}
	return b.controller.DeleteByFilter(ctx, filter)
}

// RetryAllFailedInQueue retries all failed tasks in a queue
func (b *BatchOperations) RetryAllFailedInQueue(ctx context.Context, queue string) (int, error) {
	filter := TaskFilter{
		Queue: queue,
		State: StateFailed,
	}
	return b.controller.RetryByFilter(ctx, filter)
}

// CleanupOldTasks removes completed and failed tasks older than specified duration
func (b *BatchOperations) CleanupOldTasks(ctx context.Context, olderThan time.Duration) (int, error) {
	completedCount, err1 := b.controller.PurgeCompleted(ctx, olderThan)
	failedCount, err2 := b.controller.PurgeFailed(ctx, olderThan)

	if err1 != nil {
		return completedCount, err1
	}
	if err2 != nil {
		return completedCount + failedCount, err2
	}

	return completedCount + failedCount, nil
}

// BoostPriorityForKind increases priority for all pending tasks of a kind
func (b *BatchOperations) BoostPriorityForKind(ctx context.Context, kind string, priority int) (int, error) {
	filter := TaskFilter{
		Kind:  kind,
		State: StatePending,
	}
	return b.controller.UpdatePriorityByFilter(ctx, filter, priority)
}

// RescheduleAllInQueue reschedules all pending/scheduled tasks in a queue
func (b *BatchOperations) RescheduleAllInQueue(ctx context.Context, queue string, newTime time.Time) (int, error) {
	filter := TaskFilter{
		Queue:  queue,
		States: []TaskState{StatePending, StateScheduled},
	}
	return b.controller.RescheduleByFilter(ctx, filter, newTime)
}
