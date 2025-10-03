package badger

import (
	"context"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/hasanerken/ergon"
	"github.com/vmihailenco/msgpack/v5"
)

// ListQueues lists all queues
func (s *Store) ListQueues(ctx context.Context) ([]*ergon.QueueInfo, error) {
	queues := make(map[string]*ergon.QueueInfo)

	err := s.db.View(func(txn *badger.Txn) error {
		// Scan all tasks to build queue info
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

			// Get or create queue info
			qInfo, exists := queues[task.Queue]
			if !exists {
				qInfo = &ergon.QueueInfo{
					Name: task.Queue,
				}
				queues[task.Queue] = qInfo
			}

			// Count by state
			switch task.State {
			case ergon.StatePending, ergon.StateAvailable:
				qInfo.PendingCount++
			case ergon.StateRunning:
				qInfo.RunningCount++
			case ergon.StateScheduled:
				qInfo.ScheduledCount++
			case ergon.StateRetrying:
				qInfo.RetryingCount++
			case ergon.StateCompleted:
				qInfo.CompletedCount++
				qInfo.ProcessedTotal++
			case ergon.StateFailed:
				qInfo.FailedCount++
				qInfo.FailedTotal++
			default:
				// Handle other states like Cancelled, Aggregating
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert map to slice
	result := make([]*ergon.QueueInfo, 0, len(queues))
	for _, qInfo := range queues {
		result = append(result, qInfo)
	}

	return result, nil
}

// GetQueueInfo gets information about a specific queue
func (s *Store) GetQueueInfo(ctx context.Context, queueName string) (*ergon.QueueInfo, error) {
	qInfo := &ergon.QueueInfo{
		Name: queueName,
	}

	err := s.db.View(func(txn *badger.Txn) error {
		// Check if queue info is cached
		infoKey := s.keys.QueueInfo(queueName)
		item, err := txn.Get(infoKey)
		if err == nil {
			return item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, qInfo)
			})
		}

		// If not cached, scan tasks
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

			// Skip tasks from other queues
			if task.Queue != queueName {
				continue
			}

			// Count by state
			switch task.State {
			case ergon.StatePending, ergon.StateAvailable:
				qInfo.PendingCount++
			case ergon.StateRunning:
				qInfo.RunningCount++
			case ergon.StateScheduled:
				qInfo.ScheduledCount++
			case ergon.StateRetrying:
				qInfo.RetryingCount++
			case ergon.StateCompleted:
				qInfo.CompletedCount++
				qInfo.ProcessedTotal++
			case ergon.StateFailed:
				qInfo.FailedCount++
				qInfo.FailedTotal++
			default:
				// Handle other states like Cancelled, Aggregating
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return qInfo, nil
}

// PauseQueue pauses a queue
func (s *Store) PauseQueue(ctx context.Context, queueName string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		qInfo := &ergon.QueueInfo{
			Name:   queueName,
			Paused: true,
		}

		data, err := msgpack.Marshal(qInfo)
		if err != nil {
			return err
		}

		infoKey := s.keys.QueueInfo(queueName)
		return txn.Set(infoKey, data)
	})
}

// ResumeQueue resumes a paused queue
func (s *Store) ResumeQueue(ctx context.Context, queueName string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		qInfo := &ergon.QueueInfo{
			Name:   queueName,
			Paused: false,
		}

		data, err := msgpack.Marshal(qInfo)
		if err != nil {
			return err
		}

		infoKey := s.keys.QueueInfo(queueName)
		return txn.Set(infoKey, data)
	})
}

// GroupMetadata stores metadata about a task group
type GroupMetadata struct {
	GroupKey   string    `msgpack:"group_key"`
	TaskCount  int       `msgpack:"task_count"`
	FirstTask  time.Time `msgpack:"first_task"`
	LastUpdate time.Time `msgpack:"last_update"`
}

// AddToGroup adds a task to a group (for aggregation)
func (s *Store) AddToGroup(ctx context.Context, groupKey string, task *ergon.InternalTask) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Store task data
		taskKey := s.keys.Task(task.ID)
		taskData, err := msgpack.Marshal(task)
		if err != nil {
			return err
		}
		if err := txn.Set(taskKey, taskData); err != nil {
			return err
		}

		// Add to group index
		groupIndexKey := s.keys.Group(groupKey, task.ID)
		if err := txn.Set(groupIndexKey, []byte(task.ID)); err != nil {
			return err
		}

		// Update group metadata
		metaKey := s.keys.GroupMeta(groupKey)
		var meta GroupMetadata

		item, err := txn.Get(metaKey)
		if err == nil {
			// Existing group
			err = item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &meta)
			})
			if err != nil {
				return err
			}
			meta.TaskCount++
			meta.LastUpdate = time.Now()
		} else if errors.Is(err, badger.ErrKeyNotFound) {
			// New group
			meta = GroupMetadata{
				GroupKey:   groupKey,
				TaskCount:  1,
				FirstTask:  time.Now(),
				LastUpdate: time.Now(),
			}
		} else {
			return err
		}

		metaData, err := msgpack.Marshal(&meta)
		if err != nil {
			return err
		}
		return txn.Set(metaKey, metaData)
	})
}

// GetGroup gets all tasks in a group
func (s *Store) GetGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	var tasks []*ergon.InternalTask

	err := s.db.View(func(txn *badger.Txn) error {
		prefix := s.keys.GroupPrefix(groupKey)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var taskID string
			if err := item.Value(func(val []byte) error {
				taskID = string(val)
				return nil
			}); err != nil {
				return err
			}

			// Load task data
			taskKey := s.keys.Task(taskID)
			taskItem, err := txn.Get(taskKey)
			if err != nil {
				continue // Skip if task was deleted
			}

			var task ergon.InternalTask
			if err := taskItem.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			}); err != nil {
				return err
			}

			tasks = append(tasks, &task)
		}
		return nil
	})

	return tasks, err
}

// ConsumeGroup gets and removes all tasks from a group
func (s *Store) ConsumeGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	var tasks []*ergon.InternalTask

	err := s.db.Update(func(txn *badger.Txn) error {
		prefix := s.keys.GroupPrefix(groupKey)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		// Collect all task IDs and group keys
		var groupKeys [][]byte
		var taskIDs []string

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			groupKeys = append(groupKeys, item.KeyCopy(nil))

			var taskID string
			if err := item.Value(func(val []byte) error {
				taskID = string(val)
				return nil
			}); err != nil {
				return err
			}
			taskIDs = append(taskIDs, taskID)
		}

		// Load all tasks
		for _, taskID := range taskIDs {
			taskKey := s.keys.Task(taskID)
			taskItem, err := txn.Get(taskKey)
			if err != nil {
				continue // Skip if task was deleted
			}

			var task ergon.InternalTask
			if err := taskItem.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &task)
			}); err != nil {
				return err
			}

			tasks = append(tasks, &task)

			// Delete task data
			if err := txn.Delete(taskKey); err != nil {
				return err
			}
		}

		// Delete all group index entries
		for _, key := range groupKeys {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}

		// Delete group metadata
		metaKey := s.keys.GroupMeta(groupKey)
		if err := txn.Delete(metaKey); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		return nil
	})

	return tasks, err
}

// ListGroups returns all group metadata
func (s *Store) ListGroups(ctx context.Context) ([]*GroupMetadata, error) {
	var groups []*GroupMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		prefix := s.keys.GroupListPrefix()
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var meta GroupMetadata
			if err := item.Value(func(val []byte) error {
				return msgpack.Unmarshal(val, &meta)
			}); err != nil {
				return err
			}
			groups = append(groups, &meta)
		}
		return nil
	})

	return groups, err
}

// Subscribe subscribes to task events (not supported in Badger)
func (s *Store) Subscribe(ctx context.Context, events ...ergon.EventType) (<-chan *ergon.Event, error) {
	return nil, errors.New("subscribe not supported for Badger store")
}

// TryAcquireLease tries to acquire a distributed lease
func (s *Store) TryAcquireLease(ctx context.Context, leaseKey string, ttl time.Duration) (*ergon.Lease, error) {
	var lease *ergon.Lease

	err := s.db.Update(func(txn *badger.Txn) error {
		key := []byte("lease:" + leaseKey)

		// Check if lease exists
		_, err := txn.Get(key)
		if err == nil {
			return errors.New("lease already held")
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		// Create lease
		lease = &ergon.Lease{
			Key:        leaseKey,
			Value:      generateLeaseValue(),
			TTL:        ttl,
			AcquiredAt: time.Now(),
		}

		data, err := msgpack.Marshal(lease)
		if err != nil {
			return err
		}

		// Set with TTL
		entry := badger.NewEntry(key, data).WithTTL(ttl)
		return txn.SetEntry(entry)
	})

	if err != nil {
		return nil, err
	}

	return lease, nil
}

// RenewLease renews a lease
func (s *Store) RenewLease(ctx context.Context, lease *ergon.Lease) error {
	return s.db.Update(func(txn *badger.Txn) error {
		key := []byte("lease:" + lease.Key)

		// Check if lease exists and matches
		item, err := txn.Get(key)
		if err != nil {
			return errors.New("lease not found")
		}

		var existingLease ergon.Lease
		err = item.Value(func(val []byte) error {
			return msgpack.Unmarshal(val, &existingLease)
		})
		if err != nil {
			return err
		}

		if existingLease.Value != lease.Value {
			return errors.New("lease value mismatch")
		}

		// Renew lease
		data, err := msgpack.Marshal(lease)
		if err != nil {
			return err
		}

		entry := badger.NewEntry(key, data).WithTTL(lease.TTL)
		return txn.SetEntry(entry)
	})
}

// ReleaseLease releases a lease
func (s *Store) ReleaseLease(ctx context.Context, lease *ergon.Lease) error {
	return s.db.Update(func(txn *badger.Txn) error {
		key := []byte("lease:" + lease.Key)
		return txn.Delete(key)
	})
}

// generateLeaseValue generates a unique lease value
func generateLeaseValue() string {
	return time.Now().Format(time.RFC3339Nano)
}
