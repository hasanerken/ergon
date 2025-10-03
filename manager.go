package ergon

import (
	"context"
	"time"
)

// Manager provides unified access to all task management capabilities
type Manager struct {
	client     *Client
	inspector  TaskInspector
	controller TaskController
	batch      BatchController
	statistics TaskStatistics
	store      Store
}

// NewManager creates a new task manager
func NewManager(store Store, config ClientConfig) *Manager {
	return &Manager{
		client:     NewClient(store, config),
		inspector:  NewTaskInspector(store),
		controller: NewTaskController(store),
		batch:      NewBatchController(store),
		statistics: NewTaskStatistics(store),
		store:      store,
	}
}

// Client returns the task client for enqueueing
func (m *Manager) Client() *Client {
	return m.client
}

// Tasks returns the task inspector for querying tasks
func (m *Manager) Tasks() TaskInspector {
	return m.inspector
}

// Control returns the task controller for managing tasks
func (m *Manager) Control() TaskController {
	return m.controller
}

// Batch returns the batch controller for bulk operations
func (m *Manager) Batch() BatchController {
	return m.batch
}

// Stats returns the task statistics for analytics
func (m *Manager) Stats() TaskStatistics {
	return m.statistics
}

// Store returns the underlying store (for advanced operations)
func (m *Manager) Store() Store {
	return m.store
}

// Close closes the manager and underlying connections
func (m *Manager) Close() error {
	return m.store.Close()
}

// Direct access to commonly used methods (delegating to sub-components)

// ListQueues lists all queues with statistics
func (m *Manager) ListQueues(ctx context.Context) ([]*QueueInfo, error) {
	return m.store.ListQueues(ctx)
}

// CountTasks counts tasks matching the filter
func (m *Manager) CountTasks(ctx context.Context, filter *TaskFilter) (int, error) {
	return m.store.CountTasks(ctx, filter)
}

// ListTasks lists tasks matching the filter
func (m *Manager) ListTasks(ctx context.Context, filter *TaskFilter) ([]*InternalTask, error) {
	return m.store.ListTasks(ctx, filter)
}

// GetTaskDetails gets detailed information about a task
func (m *Manager) GetTaskDetails(ctx context.Context, taskID string) (*InternalTask, error) {
	return m.store.GetTask(ctx, taskID)
}

// GetOverallStats returns overall task statistics
func (m *Manager) GetOverallStats(ctx context.Context) (*OverallStatistics, error) {
	return m.statistics.GetOverallStats(ctx)
}

// Cancel cancels a task
func (m *Manager) Cancel(ctx context.Context, taskID string) error {
	return m.controller.Cancel(ctx, taskID)
}

// Delete deletes a task
func (m *Manager) Delete(ctx context.Context, taskID string) error {
	return m.controller.Delete(ctx, taskID)
}

// RetryNow retries a task immediately
func (m *Manager) RetryNow(ctx context.Context, taskID string) error {
	return m.controller.RetryNow(ctx, taskID)
}

// Reschedule reschedules a task
func (m *Manager) Reschedule(ctx context.Context, taskID string, scheduledAt time.Time) error {
	return m.controller.Reschedule(ctx, taskID, scheduledAt)
}
