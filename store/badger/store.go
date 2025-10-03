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
	db       *badger.DB
	keys     *KeyBuilder
	mu       sync.RWMutex
	gcCancel context.CancelFunc
}

// NewStore creates a new Badger-based queue store with optimized configuration
func NewStore(path string) (*Store, error) {
	opts := badger.DefaultOptions(path).
		WithLogger(nil).               // Disable badger logs
		WithNumVersionsToKeep(1).      // We don't need MVCC history
		WithDetectConflicts(true).     // Keep enabled for correctness
		WithSyncWrites(false).         // Faster writes, acceptable for queue
		WithValueLogFileSize(64 << 20) // 64MB value log files

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger: %w", err)
	}

	store := &Store{
		db:   db,
		keys: &KeyBuilder{},
	}

	// Start background GC
	ctx, cancel := context.WithCancel(context.Background())
	store.gcCancel = cancel
	go store.runGC(ctx)

	return store, nil
}

// retryOnConflict retries a function on transaction conflicts with exponential backoff
func (s *Store) retryOnConflict(fn func() error) error {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		if errors.Is(err, badger.ErrConflict) {
			// Exponential backoff: 1ms, 2ms, 4ms
			time.Sleep(time.Millisecond * time.Duration(1<<uint(i)))
			continue
		}
		return err
	}
	return badger.ErrConflict
}

// runGC runs periodic garbage collection on the value log
func (s *Store) runGC(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Run GC with 50% discard threshold
			_ = s.db.RunValueLogGC(0.5)
		}
	}
}

// Enqueue adds a task to the queue
func (s *Store) Enqueue(ctx context.Context, task *ergon.InternalTask) error {
	return s.retryOnConflict(func() error {
		return s.db.Update(func(txn *badger.Txn) error {
			return s.enqueueInTxn(txn, task)
		})
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

// EnqueueMany adds multiple tasks with automatic batching on ErrTxnTooBig
func (s *Store) EnqueueMany(ctx context.Context, tasks []*ergon.InternalTask) error {
	return s.retryOnConflict(func() error {
		txn := s.db.NewTransaction(true)
		defer txn.Discard()

		for _, task := range tasks {
			if err := s.enqueueInTxn(txn, task); err != nil {
				if errors.Is(err, badger.ErrTxnTooBig) {
					// Commit current batch and start new transaction
					if commitErr := txn.Commit(); commitErr != nil {
						return commitErr
					}
					txn = s.db.NewTransaction(true)
					defer txn.Discard()

					// Retry this task in new transaction
					if retryErr := s.enqueueInTxn(txn, task); retryErr != nil {
						return retryErr
					}
				} else {
					return err
				}
			}
		}
		return txn.Commit()
	})
}

// EnqueueTx adds a task within a transaction (Badger supports this natively)
func (s *Store) EnqueueTx(ctx context.Context, tx ergon.Tx, task *ergon.InternalTask) error {
	// For Badger, we can't use external transactions
	// Fall back to regular enqueue
	return s.Enqueue(ctx, task)
}

// Dequeue fetches the next available task from specified queues
// Uses read-then-write pattern to minimize lock contention
func (s *Store) Dequeue(ctx context.Context, queues []string, workerID string) (*ergon.InternalTask, error) {
	var taskID string
	var pendingKey []byte

	// Phase 1: Find available task (read-only transaction - fast, no conflicts)
	err := s.db.View(func(txn *badger.Txn) error {
		for _, queue := range queues {
			prefix := s.keys.QueuePendingPrefix(queue)
			opts := badger.DefaultIteratorOptions
			opts.Prefix = prefix
			opts.PrefetchValues = false

			it := txn.NewIterator(opts)
			defer it.Close()

			it.Rewind()
			if it.Valid() {
				taskID = s.keys.ParseTaskID(it.Item().Key())
				pendingKey = append([]byte(nil), it.Item().Key()...)
				return nil
			}
		}
		return ergon.ErrTaskNotFound
	})

	if err != nil {
		return nil, err
	}

	// Phase 2: Claim task (write transaction with retry)
	var task *ergon.InternalTask
	err = s.retryOnConflict(func() error {
		return s.db.Update(func(txn *badger.Txn) error {
			// Verify task still exists and is available
			taskKey := s.keys.Task(taskID)
			item, err := txn.Get(taskKey)
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					return ergon.ErrTaskNotFound
				}
				return err
			}

			var t ergon.InternalTask
			err = item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &t)
			})
			if err != nil {
				return err
			}

			// Check if still in pending/available state
			if t.State != ergon.StatePending && t.State != ergon.StateAvailable {
				return ergon.ErrTaskNotFound
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
			if err := txn.Delete(pendingKey); err != nil {
				return err
			}

			// Add to running index
			runningKey := s.keys.Running(workerID, taskID)
			if err := txn.Set(runningKey, []byte(taskID)); err != nil {
				return err
			}

			task = &t
			return nil
		})
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
	return s.retryOnConflict(func() error {
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
	})
}

// DeleteTask deletes a task
func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	return s.retryOnConflict(func() error {
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
	})
}

// Close closes the store
func (s *Store) Close() error {
	if s.gcCancel != nil {
		s.gcCancel()
	}
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
