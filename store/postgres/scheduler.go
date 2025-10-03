package postgres

import (
	"context"
	"fmt"
	"time"
)

// MoveScheduledToAvailable moves scheduled and retry tasks to pending state
func (s *Store) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
	query := `
		UPDATE queue_tasks
		SET state = 'pending', scheduled_at = NULL
		WHERE state IN ('scheduled', 'retrying')
		  AND scheduled_at <= $1
	`

	result, err := s.db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to move scheduled tasks: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rows), nil
}

// RecoverStuckTasks recovers tasks that have been running too long
func (s *Store) RecoverStuckTasks(ctx context.Context, timeout time.Duration) (int, error) {
	stuckBefore := time.Now().Add(-timeout)

	// Retry tasks that haven't exceeded max retries
	query := `
		UPDATE queue_tasks
		SET state = 'retrying',
		    scheduled_at = NOW() + INTERVAL '1 minute',
		    retried = retried + 1
		WHERE state = 'running'
		  AND started_at < $1
		  AND retried < max_retries
	`

	result, err := s.db.ExecContext(ctx, query, stuckBefore)
	if err != nil {
		return 0, fmt.Errorf("failed to retry stuck tasks: %w", err)
	}

	retriedCount, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Fail tasks that exceeded max retries
	query = `
		UPDATE queue_tasks
		SET state = 'failed',
		    completed_at = NOW(),
		    error = 'task timed out after ' || $2 || ' and exceeded max retries'
		WHERE state = 'running'
		  AND started_at < $1
		  AND retried >= max_retries
	`

	result, err = s.db.ExecContext(ctx, query, stuckBefore, timeout.String())
	if err != nil {
		return int(retriedCount), fmt.Errorf("failed to fail stuck tasks: %w", err)
	}

	failedCount, err := result.RowsAffected()
	if err != nil {
		return int(retriedCount), fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(retriedCount + failedCount), nil
}

// ExtendLease extends the lease on a running task
func (s *Store) ExtendLease(ctx context.Context, taskID string, duration time.Duration) error {
	query := `
		UPDATE queue_tasks
		SET started_at = NOW()
		WHERE id = $1 AND state = 'running'
	`

	result, err := s.db.ExecContext(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to extend lease: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("task not found or not running")
	}

	return nil
}

// ArchiveTasks archives completed/failed tasks older than the specified time
func (s *Store) ArchiveTasks(ctx context.Context, before time.Time) (int, error) {
	// In a full implementation, this would move to an archive table
	// For now, we'll just mark them (you could add an 'archived' column)
	// or implement actual archival logic

	// Simple implementation: just count them
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM queue_tasks
		WHERE state IN ('completed', 'failed', 'cancelled')
		  AND completed_at < $1
	`, before).Scan(&count)

	return count, err
}

// DeleteArchivedTasks deletes archived tasks
func (s *Store) DeleteArchivedTasks(ctx context.Context, before time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM queue_tasks
		WHERE state IN ('completed', 'failed', 'cancelled')
		  AND completed_at < $1
	`, before)

	if err != nil {
		return 0, fmt.Errorf("failed to delete archived tasks: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rows), nil
}
