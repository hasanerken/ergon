package badger

import (
	"context"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/hasanerken/ergon"
	"github.com/vmihailenco/msgpack/v5"
)

// MarkRunning marks a task as running
func (s *Store) MarkRunning(ctx context.Context, taskID string, workerID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		var task ergon.InternalTask
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
		if err != nil {
			return err
		}

		// Remove from old state index
		if task.State == ergon.StatePending {
			pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
			_ = txn.Delete(pendingKey)
		}

		// Update task
		now := time.Now()
		task.State = ergon.StateRunning
		task.StartedAt = &now

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}
		if err := txn.Set(taskKey, data); err != nil {
			return err
		}

		// Add to running index
		runningKey := s.keys.Running(workerID, taskID)
		return txn.Set(runningKey, []byte(taskID))
	})
}

// MarkCompleted marks a task as completed
func (s *Store) MarkCompleted(ctx context.Context, taskID string, result []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		var task ergon.InternalTask
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
		if err != nil {
			return err
		}

		// Remove from running index (find by scanning)
		if task.State == ergon.StateRunning {
			s.removeFromRunning(txn, taskID)
		}

		// Update task
		now := time.Now()
		task.State = ergon.StateCompleted
		task.CompletedAt = &now
		task.Result = result

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}

		return txn.Set(taskKey, data)
	})
}

// MarkFailed marks a task as failed
func (s *Store) MarkFailed(ctx context.Context, taskID string, taskErr error) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		var task ergon.InternalTask
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
		if err != nil {
			return err
		}

		// Remove from running index
		if task.State == ergon.StateRunning {
			s.removeFromRunning(txn, taskID)
		}

		// Update task
		now := time.Now()
		task.State = ergon.StateFailed
		task.CompletedAt = &now
		if taskErr != nil {
			task.Error = taskErr.Error()
		}

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}

		return txn.Set(taskKey, data)
	})
}

// MarkRetrying marks a task for retry
func (s *Store) MarkRetrying(ctx context.Context, taskID string, nextRetry time.Time) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		var task ergon.InternalTask
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
		if err != nil {
			return err
		}

		// Remove from running index
		if task.State == ergon.StateRunning {
			s.removeFromRunning(txn, taskID)
		}

		// Update task
		task.State = ergon.StateRetrying
		task.ScheduledAt = &nextRetry
		task.Retried++

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}
		if err := txn.Set(taskKey, data); err != nil {
			return err
		}

		// Add to retry index
		retryKey := s.keys.Retry(nextRetry, taskID)
		return txn.Set(retryKey, []byte(taskID))
	})
}

// MarkCancelled marks a task as cancelled
func (s *Store) MarkCancelled(ctx context.Context, taskID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		var task ergon.InternalTask
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
		if err != nil {
			return err
		}

		// Remove from indexes based on current state
		switch task.State {
		case ergon.StatePending:
			pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
			_ = txn.Delete(pendingKey)
		case ergon.StateRunning:
			s.removeFromRunning(txn, taskID)
		case ergon.StateScheduled:
			if task.ScheduledAt != nil {
				scheduledKey := s.keys.Scheduled(*task.ScheduledAt, taskID)
				_ = txn.Delete(scheduledKey)
			}
		case ergon.StateRetrying:
			if task.ScheduledAt != nil {
				retryKey := s.keys.Retry(*task.ScheduledAt, taskID)
				_ = txn.Delete(retryKey)
			}
		default:
			// Other states (completed, failed, cancelled, etc.) don't have indexes to remove
		}

		// Update task
		now := time.Now()
		task.State = ergon.StateCancelled
		task.CompletedAt = &now

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}

		return txn.Set(taskKey, data)
	})
}

// removeFromRunning removes a task from the running index
func (s *Store) removeFromRunning(txn *badger.Txn, taskID string) {
	// Scan all running tasks to find the one with matching taskID
	prefix := s.keys.RunningPrefix()
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	opts.PrefetchValues = false

	it := txn.NewIterator(opts)
	defer it.Close()

	for it.Rewind(); it.Valid(); it.Next() {
		if s.keys.ParseTaskID(it.Item().Key()) == taskID {
			_ = txn.Delete(it.Item().Key())
			break
		}
	}
}
