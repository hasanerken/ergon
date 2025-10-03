package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/internal/jsonutil"
)

// AddToGroup adds a task to an aggregation group
func (s *Store) AddToGroup(ctx context.Context, groupKey string, task *ergon.InternalTask) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert task - payload is already JSON bytes
	var metadataJSON []byte
	if task.Metadata != nil {
		metadataJSON, err = jsonutil.Marshal(task.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES ($1, $2, $3, 'aggregating', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`, task.ID, task.Kind, task.Queue, task.Priority, task.MaxRetries, task.Retried,
		int(task.Timeout.Seconds()), task.ScheduledAt, task.EnqueuedAt,
		task.Payload, metadataJSON, nullString(task.UniqueKey), groupKey,
		nullString(task.RateLimitScope), task.Recurring,
		nullString(task.CronSchedule), nil,
	)
	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	// Update or insert group metadata
	_, err = tx.ExecContext(ctx, `
		INSERT INTO queue_groups (group_key, task_count, first_task_at, last_update_at)
		VALUES ($1, 1, NOW(), NOW())
		ON CONFLICT (group_key) DO UPDATE
		SET task_count = queue_groups.task_count + 1,
		    last_update_at = NOW()
	`, groupKey)
	if err != nil {
		return fmt.Errorf("failed to update group metadata: %w", err)
	}

	return tx.Commit()
}

// GetGroup retrieves all tasks in a group
func (s *Store) GetGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, queue, state, priority, max_retries, retried,
		       timeout_seconds, scheduled_at, enqueued_at, started_at,
		       completed_at, payload, metadata, error, result,
		       unique_key, group_key, rate_limit_scope,
		       recurring, cron_schedule, interval_seconds
		FROM queue_tasks
		WHERE group_key = $1 AND state = 'aggregating'
		ORDER BY enqueued_at ASC
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query group tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*ergon.InternalTask
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// ConsumeGroup retrieves and removes all tasks from a group
func (s *Store) ConsumeGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get all tasks
	rows, err := tx.QueryContext(ctx, `
		SELECT id, kind, queue, state, priority, max_retries, retried,
		       timeout_seconds, scheduled_at, enqueued_at, started_at,
		       completed_at, payload, metadata, error, result,
		       unique_key, group_key, rate_limit_scope,
		       recurring, cron_schedule, interval_seconds
		FROM queue_tasks
		WHERE group_key = $1 AND state = 'aggregating'
		ORDER BY enqueued_at ASC
		FOR UPDATE
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query group tasks: %w", err)
	}

	var tasks []*ergon.InternalTask
	var taskIDs []string

	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
		taskIDs = append(taskIDs, task.ID)
	}
	rows.Close()

	if len(taskIDs) == 0 {
		return tasks, nil
	}

	// Delete tasks (alternative: update state to 'consumed')
	_, err = tx.ExecContext(ctx, `
		DELETE FROM queue_tasks
		WHERE group_key = $1 AND state = 'aggregating'
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group tasks: %w", err)
	}

	// Delete group metadata
	_, err = tx.ExecContext(ctx, `
		DELETE FROM queue_groups WHERE group_key = $1
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group metadata: %w", err)
	}

	return tasks, tx.Commit()
}

// ListGroups returns metadata for all groups
func (s *Store) ListGroups(ctx context.Context) ([]*GroupMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT group_key, task_count, first_task_at, last_update_at
		FROM queue_groups
		ORDER BY last_update_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*GroupMetadata
	for rows.Next() {
		var g GroupMetadata
		if err := rows.Scan(&g.GroupKey, &g.TaskCount, &g.FirstTask, &g.LastUpdate); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, &g)
	}

	return groups, rows.Err()
}

// GroupMetadata stores metadata about a task group
type GroupMetadata struct {
	GroupKey   string
	TaskCount  int
	FirstTask  time.Time
	LastUpdate time.Time
}

// Helper to scan task from rows
func scanTask(scanner interface {
	Scan(dest ...interface{}) error
}) (*ergon.InternalTask, error) {
	var task ergon.InternalTask
	var timeoutSeconds int
	var payloadJSON, metadataJSON, resultBytes []byte
	var errorStr sql.NullString
	var uniqueKey, groupKey, rateLimitScope, cronSchedule sql.NullString
	var intervalSeconds sql.NullInt64

	err := scanner.Scan(
		&task.ID, &task.Kind, &task.Queue, &task.State, &task.Priority,
		&task.MaxRetries, &task.Retried, &timeoutSeconds,
		&task.ScheduledAt, &task.EnqueuedAt, &task.StartedAt, &task.CompletedAt,
		&payloadJSON, &metadataJSON, &errorStr, &resultBytes,
		&uniqueKey, &groupKey, &rateLimitScope,
		&task.Recurring, &cronSchedule, &intervalSeconds,
	)
	if err != nil {
		return nil, err
	}

	task.Timeout = time.Duration(timeoutSeconds) * time.Second
	task.Payload = payloadJSON

	if len(metadataJSON) > 0 {
		if err := jsonutil.Unmarshal(metadataJSON, &task.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	if errorStr.Valid {
		task.Error = errorStr.String
	}
	task.Result = resultBytes
	task.UniqueKey = nullStringValue(uniqueKey)
	task.GroupKey = nullStringValue(groupKey)
	task.RateLimitScope = nullStringValue(rateLimitScope)
	task.CronSchedule = nullStringValue(cronSchedule)

	if intervalSeconds.Valid {
		task.Interval = time.Duration(intervalSeconds.Int64) * time.Second
	}

	return &task, nil
}
