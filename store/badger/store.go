package badger

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/hasanerken/ergon"
	"github.com/vmihailenco/msgpack/v5"
)

// Store implements ergon.Store using Badger KV
type Store struct {
	db   *badger.DB
	keys *KeyBuilder
	mu   sync.RWMutex
}

// NewStore creates a new Badger-based queue store
func NewStore(path string) (*Store, error) {
	opts := badger.DefaultOptions(path).
		WithLogger(nil) // Disable badger logs

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger: %w", err)
	}

	return &Store{
		db:   db,
		keys: &KeyBuilder{},
	}, nil
}

// Enqueue adds a task to the queue
func (s *Store) Enqueue(ctx context.Context, task *ergon.InternalTask) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return s.enqueueInTxn(txn, task)
	})
}

// enqueueInTxn enqueues a task within a transaction
func (s *Store) enqueueInTxn(txn *badger.Txn, task *ergon.InternalTask) error {
	// Check unique constraint
	if task.UniqueKey != "" {
		uniqueKey := s.keys.Unique(task.UniqueKey)
		_, err := txn.Get(uniqueKey)
		if err == nil {
			return ergon.ErrDuplicateTask
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		// Set unique key
		if err := txn.Set(uniqueKey, []byte(task.ID)); err != nil {
			return err
		}
	}

	// Serialize task
	data, err := msgpack.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Store task data
	taskKey := s.keys.Task(task.ID)
	if err := txn.Set(taskKey, data); err != nil {
		return err
	}

	// Add to appropriate index
	switch task.State {
	case ergon.StateScheduled:
		if task.ScheduledAt != nil {
			scheduledKey := s.keys.Scheduled(*task.ScheduledAt, task.ID)
			if err := txn.Set(scheduledKey, []byte(task.ID)); err != nil {
				return err
			}
		}
	case ergon.StatePending, ergon.StateAvailable:
		pendingKey := s.keys.QueuePending(task.Queue, task.Priority, task.ID)
		if err := txn.Set(pendingKey, []byte(task.ID)); err != nil {
			return err
		}
	default:
		// Other states don't need index entries during enqueue
	}

	// Add kind index
	kindKey := s.keys.Kind(task.Kind, task.ID)
	if err := txn.Set(kindKey, []byte(task.ID)); err != nil {
		return err
	}

	return nil
}

// EnqueueMany adds multiple tasks
func (s *Store) EnqueueMany(ctx context.Context, tasks []*ergon.InternalTask) error {
	return s.db.Update(func(txn *badger.Txn) error {
		for _, task := range tasks {
			if err := s.enqueueInTxn(txn, task); err != nil {
				return err
			}
		}
		return nil
	})
}

// EnqueueTx adds a task within a transaction (Badger supports this natively)
func (s *Store) EnqueueTx(ctx context.Context, tx ergon.Tx, task *ergon.InternalTask) error {
	// For Badger, we can't use external transactions
	// Fall back to regular enqueue
	return s.Enqueue(ctx, task)
}

// Dequeue fetches the next available task from specified queues
func (s *Store) Dequeue(ctx context.Context, queues []string, workerID string) (*ergon.InternalTask, error) {
	var task *ergon.InternalTask

	err := s.db.Update(func(txn *badger.Txn) error {
		// Try each queue in order
		for _, queueName := range queues {
			prefix := s.keys.QueuePendingPrefix(queueName)
			opts := badger.DefaultIteratorOptions
			opts.Prefix = prefix
			opts.PrefetchValues = false

			it := txn.NewIterator(opts)
			defer it.Close()

			// Get first item (highest priority due to key sorting)
			it.Rewind()
			if !it.Valid() {
				continue
			}

			// Extract task ID from key
			taskID := s.keys.ParseTaskID(it.Item().Key())

			// Load task
			taskKey := s.keys.Task(taskID)
			item, err := txn.Get(taskKey)
			if err != nil {
				continue
			}

			var t ergon.InternalTask
			err = item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &t)
			})
			if err != nil {
				continue
			}

			// Update task state
			now := time.Now()
			t.State = ergon.StateRunning
			t.StartedAt = &now

			// Store updated task
			data, err := msgpack.Marshal(&t)
			if err != nil {
				return err
			}
			if err := txn.Set(taskKey, data); err != nil {
				return err
			}

			// Remove from pending queue
			if err := txn.Delete(it.Item().Key()); err != nil {
				return err
			}

			// Add to running index
			runningKey := s.keys.Running(workerID, taskID)
			if err := txn.Set(runningKey, []byte(taskID)); err != nil {
				return err
			}

			task = &t
			return nil
		}

		return ergon.ErrTaskNotFound
	})

	if err != nil {
		return nil, err
	}

	return task, nil
}

// GetTask retrieves a task by ID
func (s *Store) GetTask(ctx context.Context, taskID string) (*ergon.InternalTask, error) {
	var task ergon.InternalTask

	err := s.db.View(func(txn *badger.Txn) error {
		taskKey := s.keys.Task(taskID)
		item, err := txn.Get(taskKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ergon.ErrTaskNotFound
			}
			return err
		}

		return item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &task)
		})
	})

	if err != nil {
		return nil, err
	}

	return &task, nil
}

// ListTasks lists tasks matching filter
func (s *Store) ListTasks(ctx context.Context, filter *ergon.TaskFilter) ([]*ergon.InternalTask, error) {
	var tasks []*ergon.InternalTask

	err := s.db.View(func(txn *badger.Txn) error {
		var prefix []byte

		// Determine prefix based on filter
		if filter.Kind != "" {
			prefix = s.keys.KindPrefix(filter.Kind)
		} else if filter.Queue != "" && filter.State == ergon.StatePending {
			prefix = s.keys.QueuePendingPrefix(filter.Queue)
		} else {
			// Scan all tasks
			prefix = []byte("task:")
		}

		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix

		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Rewind(); it.Valid(); it.Next() {
			if filter.Limit > 0 && count >= filter.Limit {
				break
			}

			item := it.Item()

			// If this is an index key, extract task ID and load task
			var taskKey []byte
			if string(prefix) != "task:" {
				taskID := s.keys.ParseTaskID(item.Key())
				taskKey = s.keys.Task(taskID)
			} else {
				taskKey = item.Key()
			}

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

			// Apply filters
			if filter.State != "" && task.State != filter.State {
				continue
			}
			if filter.Queue != "" && task.Queue != filter.Queue {
				continue
			}

			tasks = append(tasks, &task)
			count++
		}

		return nil
	})

	return tasks, err
}

// CountTasks counts tasks matching filter
func (s *Store) CountTasks(ctx context.Context, filter *ergon.TaskFilter) (int, error) {
	tasks, err := s.ListTasks(ctx, filter)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}

// UpdateTask updates a task
func (s *Store) UpdateTask(ctx context.Context, taskID string, updates *ergon.TaskUpdate) error {
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

		// Apply updates
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
			if task.Metadata == nil {
				task.Metadata = make(map[string]interface{})
			}
			for k, v := range updates.Metadata {
				task.Metadata[k] = v
			}
		}

		// Store updated task
		data, err := msgpack.Marshal(&task)
		if err != nil {
			return err
		}

		return txn.Set(taskKey, data)
	})
}

// DeleteTask deletes a task
func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Load task to get queue info
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

		// Delete task data
		if err := txn.Delete(taskKey); err != nil {
			return err
		}

		// Delete from indexes
		if task.State == ergon.StatePending {
			pendingKey := s.keys.QueuePending(task.Queue, task.Priority, taskID)
			_ = txn.Delete(pendingKey)
		}

		// Delete kind index
		kindKey := s.keys.Kind(task.Kind, taskID)
		_ = txn.Delete(kindKey)

		// Delete unique key if exists
		if task.UniqueKey != "" {
			uniqueKey := s.keys.Unique(task.UniqueKey)
			_ = txn.Delete(uniqueKey)
		}

		return nil
	})
}

// Close closes the store
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks if the store is accessible
func (s *Store) Ping(ctx context.Context) error {
	return s.db.View(func(txn *badger.Txn) error {
		return nil
	})
}

// BeginTx begins a transaction (Badger doesn't support external transactions)
func (s *Store) BeginTx(ctx context.Context) (ergon.Tx, error) {
	return nil, errors.New("badger does not support external transactions")
}
