package mock

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/hasanerken/ergon"
)

// PostgresStore is a mock implementation of the Store interface for testing
type PostgresStore struct {
	mu     sync.RWMutex
	tasks  map[string]*ergon.InternalTask
	queues map[string]*queueState
	groups map[string][]*ergon.InternalTask
	closed bool
}

type queueState struct {
	paused bool
}

// NewPostgresStore creates a new mock Postgres store
func NewPostgresStore() *PostgresStore {
	return &PostgresStore{
		tasks:  make(map[string]*ergon.InternalTask),
		queues: make(map[string]*queueState),
		groups: make(map[string][]*ergon.InternalTask),
	}
}

// Enqueue adds a task to the queue
func (s *PostgresStore) Enqueue(ctx context.Context, task *ergon.InternalTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("store closed")
	}

	// Check for duplicate unique key
	if task.UniqueKey != "" {
		for _, t := range s.tasks {
			if t.UniqueKey == task.UniqueKey && t.State != ergon.StateCompleted && t.State != ergon.StateFailed {
				return ergon.ErrDuplicateTask
			}
		}
	}

	// Clone task to avoid external mutations
	s.tasks[task.ID] = s.cloneTask(task)

	// Initialize queue state if needed
	if _, exists := s.queues[task.Queue]; !exists {
		s.queues[task.Queue] = &queueState{}
	}

	return nil
}

// EnqueueMany adds multiple tasks
func (s *PostgresStore) EnqueueMany(ctx context.Context, tasks []*ergon.InternalTask) error {
	for _, task := range tasks {
		if err := s.Enqueue(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueTx adds a task within a transaction (mock - no real transaction)
func (s *PostgresStore) EnqueueTx(ctx context.Context, tx ergon.Tx, task *ergon.InternalTask) error {
	return s.Enqueue(ctx, task)
}

// Dequeue retrieves the next available task
func (s *PostgresStore) Dequeue(ctx context.Context, queues []string, workerID string) (*ergon.InternalTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errors.New("store closed")
	}

	var candidates []*ergon.InternalTask
	for _, task := range s.tasks {
		// Skip if not in requested queues
		if !contains(queues, task.Queue) {
			continue
		}

		// Skip if queue is paused
		if qState, exists := s.queues[task.Queue]; exists && qState.paused {
			continue
		}

		// Only dequeue available tasks
		if task.State == ergon.StatePending {
			candidates = append(candidates, task)
		}
	}

	if len(candidates) == 0 {
		return nil, ergon.ErrTaskNotFound
	}

	// Sort by priority (highest first), then by enqueued time (oldest first)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].EnqueuedAt.Before(candidates[j].EnqueuedAt)
	})

	task := candidates[0]
	task.State = ergon.StateRunning
	now := time.Now()
	task.StartedAt = &now

	return s.cloneTask(task), nil
}

// GetTask retrieves a task by ID
func (s *PostgresStore) GetTask(ctx context.Context, taskID string) (*ergon.InternalTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, ergon.ErrTaskNotFound
	}

	return s.cloneTask(task), nil
}

// ListTasks returns tasks matching filter
func (s *PostgresStore) ListTasks(ctx context.Context, filter *ergon.TaskFilter) ([]*ergon.InternalTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ergon.InternalTask
	for _, task := range s.tasks {
		if s.matchesFilter(task, filter) {
			result = append(result, s.cloneTask(task))
		}
	}

	// Apply limit and offset
	if filter != nil {
		if filter.Offset > 0 && filter.Offset < len(result) {
			result = result[filter.Offset:]
		}
		if filter.Limit > 0 && filter.Limit < len(result) {
			result = result[:filter.Limit]
		}
	}

	return result, nil
}

// CountTasks counts tasks matching filter
func (s *PostgresStore) CountTasks(ctx context.Context, filter *ergon.TaskFilter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, task := range s.tasks {
		if s.matchesFilter(task, filter) {
			count++
		}
	}
	return count, nil
}

// UpdateTask updates task fields
func (s *PostgresStore) UpdateTask(ctx context.Context, taskID string, updates *ergon.TaskUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return ergon.ErrTaskNotFound
	}

	if updates.State != nil {
		task.State = *updates.State
	}
	if updates.Priority != nil {
		task.Priority = *updates.Priority
	}
	if updates.MaxRetries != nil {
		task.MaxRetries = *updates.MaxRetries
	}
	if updates.Timeout != nil {
		task.Timeout = *updates.Timeout
	}
	if updates.ScheduledAt != nil {
		task.ScheduledAt = updates.ScheduledAt
	}
	if updates.Metadata != nil {
		task.Metadata = updates.Metadata
	}

	return nil
}

// DeleteTask removes a task
func (s *PostgresStore) DeleteTask(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tasks, taskID)
	return nil
}

// MarkRunning marks a task as running
func (s *PostgresStore) MarkRunning(ctx context.Context, taskID string, workerID string) error {
	return s.updateState(taskID, ergon.StateRunning)
}

// MarkCompleted marks a task as completed
func (s *PostgresStore) MarkCompleted(ctx context.Context, taskID string, result []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return ergon.ErrTaskNotFound
	}

	task.State = ergon.StateCompleted
	task.Result = result
	now := time.Now()
	task.CompletedAt = &now

	return nil
}

// MarkFailed marks a task as failed
func (s *PostgresStore) MarkFailed(ctx context.Context, taskID string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return ergon.ErrTaskNotFound
	}

	task.State = ergon.StateFailed
	if err != nil {
		task.Error = err.Error()
	}
	now := time.Now()
	task.CompletedAt = &now

	return nil
}

// MarkRetrying marks a task for retry
func (s *PostgresStore) MarkRetrying(ctx context.Context, taskID string, nextRetry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return ergon.ErrTaskNotFound
	}

	task.State = ergon.StateRetrying
	task.ScheduledAt = &nextRetry
	task.Retried++

	return nil
}

// MarkCancelled marks a task as cancelled
func (s *PostgresStore) MarkCancelled(ctx context.Context, taskID string) error {
	return s.updateState(taskID, ergon.StateCancelled)
}

// MoveScheduledToAvailable moves scheduled tasks to available state
func (s *PostgresStore) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, task := range s.tasks {
		if (task.State == ergon.StateScheduled || task.State == ergon.StateRetrying) &&
			task.ScheduledAt != nil &&
			task.ScheduledAt.Before(before) {
			task.State = ergon.StatePending
			count++
		}
	}

	return count, nil
}

// ListQueues returns all queues
func (s *PostgresStore) ListQueues(ctx context.Context) ([]*ergon.QueueInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queueMap := make(map[string]*ergon.QueueInfo)

	for queueName, qState := range s.queues {
		queueMap[queueName] = &ergon.QueueInfo{
			Name:   queueName,
			Paused: qState.paused,
		}
	}

	// Count tasks per queue
	for _, task := range s.tasks {
		if _, exists := queueMap[task.Queue]; !exists {
			queueMap[task.Queue] = &ergon.QueueInfo{Name: task.Queue}
		}

		qi := queueMap[task.Queue]
		switch task.State {
		case ergon.StatePending:
			qi.PendingCount++
		case ergon.StateRunning:
			qi.RunningCount++
		case ergon.StateScheduled:
			qi.ScheduledCount++
		case ergon.StateRetrying:
			qi.RetryingCount++
		case ergon.StateCompleted:
			qi.CompletedCount++
		case ergon.StateFailed:
			qi.FailedCount++
		}
	}

	result := make([]*ergon.QueueInfo, 0, len(queueMap))
	for _, qi := range queueMap {
		result = append(result, qi)
	}

	return result, nil
}

// GetQueueInfo returns info for a specific queue
func (s *PostgresStore) GetQueueInfo(ctx context.Context, queue string) (*ergon.QueueInfo, error) {
	queues, err := s.ListQueues(ctx)
	if err != nil {
		return nil, err
	}

	for _, q := range queues {
		if q.Name == queue {
			return q, nil
		}
	}

	return nil, ergon.ErrQueueNotFound
}

// PauseQueue pauses a queue
func (s *PostgresStore) PauseQueue(ctx context.Context, queue string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.queues[queue]; !exists {
		s.queues[queue] = &queueState{}
	}
	s.queues[queue].paused = true

	return nil
}

// ResumeQueue resumes a paused queue
func (s *PostgresStore) ResumeQueue(ctx context.Context, queue string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qState, exists := s.queues[queue]; exists {
		qState.paused = false
	}

	return nil
}

// AddToGroup adds a task to a group
func (s *PostgresStore) AddToGroup(ctx context.Context, groupKey string, task *ergon.InternalTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task.GroupKey = groupKey
	task.State = ergon.StateAggregating
	s.tasks[task.ID] = s.cloneTask(task)
	s.groups[groupKey] = append(s.groups[groupKey], s.cloneTask(task))

	return nil
}

// GetGroup retrieves all tasks in a group
func (s *PostgresStore) GetGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks, exists := s.groups[groupKey]
	if !exists {
		return nil, nil
	}

	result := make([]*ergon.InternalTask, len(tasks))
	for i, task := range tasks {
		result[i] = s.cloneTask(task)
	}

	return result, nil
}

// ConsumeGroup retrieves and removes all tasks from a group
func (s *PostgresStore) ConsumeGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, exists := s.groups[groupKey]
	if !exists {
		return nil, nil
	}

	result := make([]*ergon.InternalTask, len(tasks))
	for i, task := range tasks {
		result[i] = s.cloneTask(task)
		delete(s.tasks, task.ID)
	}

	delete(s.groups, groupKey)

	return result, nil
}

// ArchiveTasks archives old tasks
func (s *PostgresStore) ArchiveTasks(ctx context.Context, before time.Time) (int, error) {
	// Mock doesn't implement archiving
	return 0, nil
}

// DeleteArchivedTasks deletes archived tasks
func (s *PostgresStore) DeleteArchivedTasks(ctx context.Context, before time.Time) (int, error) {
	// Mock doesn't implement archiving
	return 0, nil
}

// RecoverStuckTasks recovers tasks that have been running too long
func (s *PostgresStore) RecoverStuckTasks(ctx context.Context, timeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	cutoff := time.Now().Add(-timeout)

	for _, task := range s.tasks {
		if task.State == ergon.StateRunning && task.StartedAt != nil && task.StartedAt.Before(cutoff) {
			task.State = ergon.StatePending
			task.StartedAt = nil
			count++
		}
	}

	return count, nil
}

// ExtendLease extends a task's lease
func (s *PostgresStore) ExtendLease(ctx context.Context, taskID string, duration time.Duration) error {
	// Mock implementation - just verify task exists
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.tasks[taskID]; !exists {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// Subscribe to task events (not implemented in mock)
func (s *PostgresStore) Subscribe(ctx context.Context, events ...ergon.EventType) (<-chan *ergon.Event, error) {
	return nil, ergon.ErrNotImplemented
}

// TryAcquireLease tries to acquire a distributed lease
func (s *PostgresStore) TryAcquireLease(ctx context.Context, leaseKey string, ttl time.Duration) (*ergon.Lease, error) {
	return nil, ergon.ErrNotImplemented
}

// RenewLease renews a lease
func (s *PostgresStore) RenewLease(ctx context.Context, lease *ergon.Lease) error {
	return ergon.ErrNotImplemented
}

// ReleaseLease releases a lease
func (s *PostgresStore) ReleaseLease(ctx context.Context, lease *ergon.Lease) error {
	return ergon.ErrNotImplemented
}

// BeginTx begins a transaction
func (s *PostgresStore) BeginTx(ctx context.Context) (ergon.Tx, error) {
	return &mockTx{}, nil
}

// Close closes the store
func (s *PostgresStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// Ping checks store health
func (s *PostgresStore) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return errors.New("store closed")
	}
	return nil
}

// Helper methods

func (s *PostgresStore) updateState(taskID string, state ergon.TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return ergon.ErrTaskNotFound
	}

	task.State = state
	return nil
}

func (s *PostgresStore) cloneTask(task *ergon.InternalTask) *ergon.InternalTask {
	clone := *task

	// Clone metadata map
	if task.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range task.Metadata {
			clone.Metadata[k] = v
		}
	}

	// Clone byte slices
	if task.Payload != nil {
		clone.Payload = make([]byte, len(task.Payload))
		copy(clone.Payload, task.Payload)
	}
	if task.Result != nil {
		clone.Result = make([]byte, len(task.Result))
		copy(clone.Result, task.Result)
	}

	// Clone time pointers
	if task.ScheduledAt != nil {
		t := *task.ScheduledAt
		clone.ScheduledAt = &t
	}
	if task.StartedAt != nil {
		t := *task.StartedAt
		clone.StartedAt = &t
	}
	if task.CompletedAt != nil {
		t := *task.CompletedAt
		clone.CompletedAt = &t
	}

	return &clone
}

func (s *PostgresStore) matchesFilter(task *ergon.InternalTask, filter *ergon.TaskFilter) bool {
	if filter == nil {
		return true
	}

	if filter.Queue != "" && task.Queue != filter.Queue {
		return false
	}

	if filter.State != "" && task.State != filter.State {
		return false
	}

	if len(filter.States) > 0 {
		found := false
		for _, state := range filter.States {
			if task.State == state {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if filter.Kind != "" && task.Kind != filter.Kind {
		return false
	}

	if len(filter.Kinds) > 0 && !contains(filter.Kinds, task.Kind) {
		return false
	}

	if filter.Priority != nil && task.Priority != *filter.Priority {
		return false
	}

	return true
}

// mockTx is a mock transaction
type mockTx struct{}

func (tx *mockTx) Commit(ctx context.Context) error {
	return nil
}

func (tx *mockTx) Rollback(ctx context.Context) error {
	return nil
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetTasksByState returns all tasks in a specific state (test helper)
func (s *PostgresStore) GetTasksByState(state ergon.TaskState) []*ergon.InternalTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ergon.InternalTask
	for _, task := range s.tasks {
		if task.State == state {
			result = append(result, s.cloneTask(task))
		}
	}
	return result
}

// Reset clears all data (test helper)
func (s *PostgresStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks = make(map[string]*ergon.InternalTask)
	s.queues = make(map[string]*queueState)
	s.groups = make(map[string][]*ergon.InternalTask)
	s.closed = false
}
