package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hasanerken/ergon"
)

// ListTasks lists tasks matching the filter
func (s *Store) ListTasks(ctx context.Context, filter *ergon.TaskFilter) ([]*ergon.InternalTask, error) {
	query := `
		SELECT id, kind, queue, state, priority, max_retries, retried,
		       timeout_seconds, scheduled_at, enqueued_at, started_at,
		       completed_at, payload, metadata, error, result,
		       unique_key, group_key, rate_limit_scope,
		       recurring, cron_schedule, interval_seconds
		FROM queue_tasks
		WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if filter != nil {
		if filter.Queue != "" {
			query += fmt.Sprintf(" AND queue = $%d", argNum)
			args = append(args, filter.Queue)
			argNum++
		}

		if filter.State != "" {
			query += fmt.Sprintf(" AND state = $%d", argNum)
			args = append(args, filter.State)
			argNum++
		}

		if len(filter.States) > 0 {
			query += fmt.Sprintf(" AND state = ANY($%d)", argNum)
			args = append(args, filter.States)
			argNum++
		}

		if filter.Kind != "" {
			query += fmt.Sprintf(" AND kind = $%d", argNum)
			args = append(args, filter.Kind)
			argNum++
		}

		if len(filter.Kinds) > 0 {
			query += fmt.Sprintf(" AND kind = ANY($%d)", argNum)
			args = append(args, filter.Kinds)
			argNum++
		}

		if filter.Priority != nil {
			query += fmt.Sprintf(" AND priority = $%d", argNum)
			args = append(args, *filter.Priority)
			argNum++
		}

		// Order by
		if filter.OrderBy != "" {
			orderDir := "ASC"
			if filter.OrderDir == "desc" {
				orderDir = "DESC"
			}
			query += fmt.Sprintf(" ORDER BY %s %s", filter.OrderBy, orderDir)
		} else {
			query += " ORDER BY enqueued_at DESC"
		}

		// Limit and offset
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", argNum)
			args = append(args, filter.Limit)
			argNum++
		}

		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argNum)
			args = append(args, filter.Offset)
			argNum++
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
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

// CountTasks counts tasks matching the filter
func (s *Store) CountTasks(ctx context.Context, filter *ergon.TaskFilter) (int, error) {
	query := "SELECT COUNT(*) FROM queue_tasks WHERE 1=1"
	args := []interface{}{}
	argNum := 1

	if filter != nil {
		if filter.Queue != "" {
			query += fmt.Sprintf(" AND queue = $%d", argNum)
			args = append(args, filter.Queue)
			argNum++
		}

		if filter.State != "" {
			query += fmt.Sprintf(" AND state = $%d", argNum)
			args = append(args, filter.State)
			argNum++
		}

		if len(filter.States) > 0 {
			query += fmt.Sprintf(" AND state = ANY($%d)", argNum)
			args = append(args, filter.States)
			argNum++
		}

		if filter.Kind != "" {
			query += fmt.Sprintf(" AND kind = $%d", argNum)
			args = append(args, filter.Kind)
			argNum++
		}

		if len(filter.Kinds) > 0 {
			query += fmt.Sprintf(" AND kind = ANY($%d)", argNum)
			args = append(args, filter.Kinds)
			argNum++
		}

		if filter.Priority != nil {
			query += fmt.Sprintf(" AND priority = $%d", argNum)
			args = append(args, *filter.Priority)
			argNum++
		}
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	return count, nil
}

// ListQueues lists all queues
func (s *Store) ListQueues(ctx context.Context) ([]*ergon.QueueInfo, error) {
	// Get unique queues from tasks
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			queue,
			COUNT(*) FILTER (WHERE state = 'pending') as pending,
			COUNT(*) FILTER (WHERE state = 'running') as running,
			COUNT(*) FILTER (WHERE state = 'scheduled') as scheduled,
			COUNT(*) FILTER (WHERE state = 'retrying') as retrying,
			COUNT(*) FILTER (WHERE state = 'completed') as completed,
			COUNT(*) FILTER (WHERE state = 'failed') as failed
		FROM queue_tasks
		GROUP BY queue
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}
	defer rows.Close()

	var queues []*ergon.QueueInfo
	for rows.Next() {
		var q ergon.QueueInfo
		if err := rows.Scan(
			&q.Name, &q.PendingCount, &q.RunningCount,
			&q.ScheduledCount, &q.RetryingCount,
			&q.CompletedCount, &q.FailedCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan queue info: %w", err)
		}

		// Check if paused
		var paused sql.NullBool
		err := s.db.QueryRowContext(ctx, `
			SELECT paused FROM queue_info WHERE queue_name = $1
		`, q.Name).Scan(&paused)
		if err == nil && paused.Valid {
			q.Paused = paused.Bool
		}

		queues = append(queues, &q)
	}

	return queues, rows.Err()
}

// GetQueueInfo gets info for a specific queue
func (s *Store) GetQueueInfo(ctx context.Context, queueName string) (*ergon.QueueInfo, error) {
	var q ergon.QueueInfo
	q.Name = queueName

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE state = 'pending') as pending,
			COUNT(*) FILTER (WHERE state = 'running') as running,
			COUNT(*) FILTER (WHERE state = 'scheduled') as scheduled,
			COUNT(*) FILTER (WHERE state = 'retrying') as retrying,
			COUNT(*) FILTER (WHERE state = 'completed') as completed,
			COUNT(*) FILTER (WHERE state = 'failed') as failed
		FROM queue_tasks
		WHERE queue = $1
	`, queueName).Scan(
		&q.PendingCount, &q.RunningCount, &q.ScheduledCount,
		&q.RetryingCount, &q.CompletedCount, &q.FailedCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}

	// Check if paused
	var paused sql.NullBool
	err = s.db.QueryRowContext(ctx, `
		SELECT paused FROM queue_info WHERE queue_name = $1
	`, queueName).Scan(&paused)
	if err == nil && paused.Valid {
		q.Paused = paused.Bool
	}

	return &q, nil
}

// PauseQueue pauses a queue
func (s *Store) PauseQueue(ctx context.Context, queueName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO queue_info (queue_name, paused)
		VALUES ($1, true)
		ON CONFLICT (queue_name) DO UPDATE
		SET paused = true
	`, queueName)

	if err != nil {
		return fmt.Errorf("failed to pause queue: %w", err)
	}

	return nil
}

// ResumeQueue resumes a paused queue
func (s *Store) ResumeQueue(ctx context.Context, queueName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO queue_info (queue_name, paused)
		VALUES ($1, false)
		ON CONFLICT (queue_name) DO UPDATE
		SET paused = false
	`, queueName)

	if err != nil {
		return fmt.Errorf("failed to resume queue: %w", err)
	}

	return nil
}

// Subscribe subscribes to task events (PostgreSQL LISTEN/NOTIFY)
func (s *Store) Subscribe(ctx context.Context, events ...ergon.EventType) (<-chan *ergon.Event, error) {
	// PostgreSQL LISTEN/NOTIFY implementation
	// This requires a persistent connection
	// For simplicity, returning not implemented for now
	return nil, fmt.Errorf("subscribe not yet implemented for PostgreSQL store")
}

// TryAcquireLease attempts to acquire a distributed lease
func (s *Store) TryAcquireLease(ctx context.Context, leaseKey string, ttl time.Duration) (*ergon.Lease, error) {
	expiresAt := time.Now().Add(ttl)
	leaseValue := fmt.Sprintf("%d", time.Now().UnixNano())

	// Try to insert lease
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO queue_leases (lease_key, lease_value, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (lease_key) DO NOTHING
	`, leaseKey, leaseValue, expiresAt)

	if err != nil {
		return nil, fmt.Errorf("failed to acquire lease: %w", err)
	}

	// Check if we got the lease
	var currentValue string
	var currentExpires time.Time
	err = s.db.QueryRowContext(ctx, `
		SELECT lease_value, expires_at
		FROM queue_leases
		WHERE lease_key = $1
	`, leaseKey).Scan(&currentValue, &currentExpires)

	if err != nil {
		return nil, fmt.Errorf("failed to check lease: %w", err)
	}

	// If expired, try to claim it
	if currentExpires.Before(time.Now()) {
		_, err = s.db.ExecContext(ctx, `
			UPDATE queue_leases
			SET lease_value = $2, expires_at = $3
			WHERE lease_key = $1 AND expires_at < NOW()
		`, leaseKey, leaseValue, expiresAt)

		if err != nil {
			return nil, fmt.Errorf("failed to claim expired lease: %w", err)
		}
		currentValue = leaseValue
	}

	// Check if we own the lease
	if currentValue != leaseValue {
		return nil, fmt.Errorf("lease already held by another process")
	}

	return &ergon.Lease{
		Key:        leaseKey,
		Value:      leaseValue,
		TTL:        ttl,
		AcquiredAt: time.Now(),
	}, nil
}

// RenewLease renews an existing lease
func (s *Store) RenewLease(ctx context.Context, lease *ergon.Lease) error {
	expiresAt := time.Now().Add(lease.TTL)

	result, err := s.db.ExecContext(ctx, `
		UPDATE queue_leases
		SET expires_at = $3
		WHERE lease_key = $1 AND lease_value = $2
	`, lease.Key, lease.Value, expiresAt)

	if err != nil {
		return fmt.Errorf("failed to renew lease: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("lease not found or not owned")
	}

	return nil
}

// ReleaseLease releases a lease
func (s *Store) ReleaseLease(ctx context.Context, lease *ergon.Lease) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM queue_leases
		WHERE lease_key = $1 AND lease_value = $2
	`, lease.Key, lease.Value)

	if err != nil {
		return fmt.Errorf("failed to release lease: %w", err)
	}

	return nil
}
