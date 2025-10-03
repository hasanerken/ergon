package badger

import (
	"context"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/hasanerken/ergon"
	"github.com/vmihailenco/msgpack/v5"
)

// MoveScheduledToAvailable moves scheduled tasks that are ready to run
func (s *Store) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
	count := 0

	err := s.db.Update(func(txn *badger.Txn) error {
		// Scan scheduled tasks before the given time
		prefix := s.keys.ScheduledPrefix()
		endKey := s.keys.ScheduledBefore(before)

		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// Stop if we've passed the time threshold
			if string(key) > string(endKey) {
				break
			}

			// Extract task ID
			taskID := s.keys.ParseTaskID(key)

			// Load task
			taskKey := s.keys.Task(taskID)
			taskItem, err := txn.Get(taskKey)
			if err != nil {
				continue
			}

			var task ergon.InternalTask
			err = taskItem.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			})
			if err != nil {
				continue
			}

			// Update task state
			task.State = ergon.StatePending
			task.ScheduledAt = nil

			// Store updated task
			data, err := msgpack.Marshal(&task)
			if err != nil {
				continue
			}
			if err := txn.Set(taskKey, data); err != nil {
				continue
			}

			// Remove from scheduled index
			if err := txn.Delete(key); err != nil {
				continue
			}

			// Add to pending queue
			pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
			if err := txn.Set(pendingKey, []byte(taskID)); err != nil {
				continue
			}

			count++
		}

		// Also process retry queue
		retryPrefix := s.keys.RetryPrefix()
		retryEndKey := s.keys.RetryBefore(before)

		retryOpts := badger.DefaultIteratorOptions
		retryOpts.Prefix = retryPrefix
		retryOpts.PrefetchValues = false

		retryIt := txn.NewIterator(retryOpts)
		defer retryIt.Close()

		for retryIt.Rewind(); retryIt.Valid(); retryIt.Next() {
			item := retryIt.Item()
			key := item.Key()

			// Stop if we've passed the time threshold
			if string(key) > string(retryEndKey) {
				break
			}

			// Extract task ID
			taskID := s.keys.ParseTaskID(key)

			// Load task
			taskKey := s.keys.Task(taskID)
			taskItem, err := txn.Get(taskKey)
			if err != nil {
				continue
			}

			var task ergon.InternalTask
			err = taskItem.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			})
			if err != nil {
				continue
			}

			// Update task state
			task.State = ergon.StatePending
			task.ScheduledAt = nil

			// Store updated task
			data, err := msgpack.Marshal(&task)
			if err != nil {
				continue
			}
			if err := txn.Set(taskKey, data); err != nil {
				continue
			}

			// Remove from retry index
			if err := txn.Delete(key); err != nil {
				continue
			}

			// Add to pending queue
			pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
			if err := txn.Set(pendingKey, []byte(taskID)); err != nil {
				continue
			}

			count++
		}

		return nil
	})

	return count, err
}

// RecoverStuckTasks recovers tasks that have been running too long
func (s *Store) RecoverStuckTasks(ctx context.Context, timeout time.Duration) (int, error) {
	count := 0
	cutoff := time.Now().Add(-timeout)

	err := s.db.Update(func(txn *badger.Txn) error {
		prefix := s.keys.RunningPrefix()
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			// Extract task ID
			taskID := s.keys.ParseTaskID(it.Item().Key())

			// Load task
			taskKey := s.keys.Task(taskID)
			taskItem, err := txn.Get(taskKey)
			if err != nil {
				continue
			}

			var task ergon.InternalTask
			err = taskItem.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			})
			if err != nil {
				continue
			}

			// Check if task has been running too long
			if task.StartedAt != nil && task.StartedAt.Before(cutoff) {
				// Retry or fail based on retry count
				if task.Retried < task.MaxRetries {
					// Retry
					task.State = ergon.StateRetrying
					nextRetry := time.Now().Add(time.Duration(task.Retried+1) * time.Second)
					task.ScheduledAt = &nextRetry
					task.Retried++

					// Store updated task
					data, err := msgpack.Marshal(&task)
					if err != nil {
						continue
					}
					if err := txn.Set(taskKey, data); err != nil {
						continue
					}

					// Remove from running index
					if err := txn.Delete(it.Item().Key()); err != nil {
						continue
					}

					// Add to retry index
					retryKey := s.keys.Retry(nextRetry, taskID)
					if err := txn.Set(retryKey, []byte(taskID)); err != nil {
						continue
					}
				} else {
					// Failed
					now := time.Now()
					task.State = ergon.StateFailed
					task.CompletedAt = &now
					task.Error = "task exceeded timeout"

					// Store updated task
					data, err := msgpack.Marshal(&task)
					if err != nil {
						continue
					}
					if err := txn.Set(taskKey, data); err != nil {
						continue
					}

					// Remove from running index
					if err := txn.Delete(it.Item().Key()); err != nil {
						continue
					}
				}

				count++
			}
		}

		return nil
	})

	return count, err
}

// ExtendLease extends a task's lease (for heartbeat)
func (s *Store) ExtendLease(ctx context.Context, taskID string, duration time.Duration) error {
	return s.db.Update(func(txn *badger.Txn) error {
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

		// Update started time to extend lease
		if task.StartedAt != nil {
			now := time.Now()
			task.StartedAt = &now

			data, err := msgpack.Marshal(&task)
			if err != nil {
				return err
			}

			return txn.Set(taskKey, data)
		}

		return nil
	})
}

// ArchiveTasks moves old completed tasks to archive (just delete them for now)
func (s *Store) ArchiveTasks(ctx context.Context, before time.Time) (int, error) {
	// For Badger, we'll just delete old completed tasks
	// In a more sophisticated implementation, we could move them to an archive prefix
	count := 0

	err := s.db.Update(func(txn *badger.Txn) error {
		prefix := []byte("task:")
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var task ergon.InternalTask
			err := item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			})
			if err != nil {
				continue
			}

			// Archive completed or failed tasks older than before
			if (task.State == ergon.StateCompleted || task.State == ergon.StateFailed) &&
				task.CompletedAt != nil && task.CompletedAt.Before(before) {

				// Delete task
				if err := txn.Delete(item.Key()); err != nil {
					continue
				}

				// Delete from kind index
				kindKey := s.keys.Kind(task.Kind, task.ID)
				_ = txn.Delete(kindKey)

				// Delete unique key if exists
				if task.UniqueKey != "" {
					uniqueKey := s.keys.Unique(task.UniqueKey)
					_ = txn.Delete(uniqueKey)
				}

				count++
			}
		}

		return nil
	})

	return count, err
}

// DeleteArchivedTasks deletes archived tasks (same as archive for Badger)
func (s *Store) DeleteArchivedTasks(ctx context.Context, before time.Time) (int, error) {
	return s.ArchiveTasks(ctx, before)
}
