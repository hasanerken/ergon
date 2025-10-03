package badger

import (
	"fmt"
	"strings"
	"time"
)

// KeyBuilder builds storage keys for the queue system
type KeyBuilder struct{}

// Task returns the key for a task's data
func (k *KeyBuilder) Task(id string) []byte {
	return []byte("task:" + id)
}

// QueuePending returns the key for a pending task in a queue
// Priority is reversed (9999999999-priority) for descending sort
func (k *KeyBuilder) QueuePending(queue string, priority int, id string) []byte {
	p := fmt.Sprintf("%010d", 9999999999-priority)
	return []byte(fmt.Sprintf("queue:%s:pending:%s:%s", queue, p, id))
}

// QueuePendingPrefix returns the prefix for all pending tasks in a queue
func (k *KeyBuilder) QueuePendingPrefix(queue string) []byte {
	return []byte(fmt.Sprintf("queue:%s:pending:", queue))
}

// Scheduled returns the key for a scheduled task
func (k *KeyBuilder) Scheduled(at time.Time, id string) []byte {
	ts := fmt.Sprintf("%019d", at.UnixNano())
	return []byte(fmt.Sprintf("scheduled:%s:%s", ts, id))
}

// ScheduledPrefix returns the prefix for all scheduled tasks
func (k *KeyBuilder) ScheduledPrefix() []byte {
	return []byte("scheduled:")
}

// ScheduledBefore returns the key for scanning scheduled tasks before a time
func (k *KeyBuilder) ScheduledBefore(before time.Time) []byte {
	ts := fmt.Sprintf("%019d", before.UnixNano())
	return []byte(fmt.Sprintf("scheduled:%s:", ts))
}

// Running returns the key for a running task
func (k *KeyBuilder) Running(workerID, taskID string) []byte {
	return []byte(fmt.Sprintf("running:%s:%s", workerID, taskID))
}

// RunningPrefix returns the prefix for all running tasks
func (k *KeyBuilder) RunningPrefix() []byte {
	return []byte("running:")
}

// Retry returns the key for a task waiting to retry
func (k *KeyBuilder) Retry(at time.Time, id string) []byte {
	ts := fmt.Sprintf("%019d", at.UnixNano())
	return []byte(fmt.Sprintf("retry:%s:%s", ts, id))
}

// RetryPrefix returns the prefix for all retry tasks
func (k *KeyBuilder) RetryPrefix() []byte {
	return []byte("retry:")
}

// RetryBefore returns the key for scanning retry tasks before a time
func (k *KeyBuilder) RetryBefore(before time.Time) []byte {
	ts := fmt.Sprintf("%019d", before.UnixNano())
	return []byte(fmt.Sprintf("retry:%s:", ts))
}

// Unique returns the key for a unique task constraint
func (k *KeyBuilder) Unique(uniqueKey string) []byte {
	return []byte("unique:" + uniqueKey)
}

// Kind returns the key for a task kind index
func (k *KeyBuilder) Kind(kind, id string) []byte {
	return []byte(fmt.Sprintf("kind:%s:%s", kind, id))
}

// KindPrefix returns the prefix for all tasks of a kind
func (k *KeyBuilder) KindPrefix(kind string) []byte {
	return []byte(fmt.Sprintf("kind:%s:", kind))
}

// QueueInfo returns the key for queue information
func (k *KeyBuilder) QueueInfo(queue string) []byte {
	return []byte("queue:info:" + queue)
}

// QueueListPrefix returns the prefix for all queue info
func (k *KeyBuilder) QueueListPrefix() []byte {
	return []byte("queue:info:")
}

// ParseTaskID extracts the task ID from a key
func (k *KeyBuilder) ParseTaskID(key []byte) string {
	parts := strings.Split(string(key), ":")
	return parts[len(parts)-1]
}

// ParseQueueFromPendingKey extracts the queue name from a pending key
func (k *KeyBuilder) ParseQueueFromPendingKey(key []byte) string {
	parts := strings.Split(string(key), ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// Group returns the key for a task in a group
func (k *KeyBuilder) Group(groupKey string, taskID string) []byte {
	return []byte(fmt.Sprintf("group:%s:%s", groupKey, taskID))
}

// GroupPrefix returns the prefix for all tasks in a group
func (k *KeyBuilder) GroupPrefix(groupKey string) []byte {
	return []byte(fmt.Sprintf("group:%s:", groupKey))
}

// GroupMeta returns the key for group metadata
func (k *KeyBuilder) GroupMeta(groupKey string) []byte {
	return []byte(fmt.Sprintf("groupmeta:%s", groupKey))
}

// GroupListPrefix returns the prefix for all group metadata
func (k *KeyBuilder) GroupListPrefix() []byte {
	return []byte("groupmeta:")
}
