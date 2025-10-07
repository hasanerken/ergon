package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/hasanerken/ergon"
)

// MarkRunning marks a task as running
func (s *Store) MarkRunning(ctx context.Context, taskID string, workerID string) error {
	query := `
		UPDATE queue_tasks
		SET state = 'running', started_at = NOW()
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as running: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkCompleted marks a task as completed
func (s *Store) MarkCompleted(ctx context.Context, taskID string, result []byte) error {
	query := `
		UPDATE queue_tasks
		SET state = 'completed', completed_at = NOW(), result = $2
		WHERE id = $1
	`

	res, err := s.db.ExecContext(ctx, query, taskID, nullJSON(result))
	if err != nil {
		return fmt.Errorf("failed to mark task as completed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkFailed marks a task as failed
func (s *Store) MarkFailed(ctx context.Context, taskID string, taskErr error) error {
	errorMsg := ""
	if taskErr != nil {
		errorMsg = taskErr.Error()
	}

	query := `
		UPDATE queue_tasks
		SET state = 'failed', completed_at = NOW(), error = $2
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query, taskID, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkRetrying marks a task for retry
func (s *Store) MarkRetrying(ctx context.Context, taskID string, nextRetry time.Time) error {
	query := `
		UPDATE queue_tasks
		SET state = 'retrying', scheduled_at = $2, retried = retried + 1
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query, taskID, nextRetry)
	if err != nil {
		return fmt.Errorf("failed to mark task for retry: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkCancelled marks a task as cancelled
func (s *Store) MarkCancelled(ctx context.Context, taskID string) error {
	query := `
		UPDATE queue_tasks
		SET state = 'cancelled', completed_at = NOW()
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as cancelled: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// UpdateTask updates task fields
func (s *Store) UpdateTask(ctx context.Context, taskID string, updates *ergon.TaskUpdate) error {
	// Build dynamic UPDATE query based on what fields are set
	// This is a simplified version - you could make it more sophisticated

	if updates.State != nil {
		query := `UPDATE queue_tasks SET state = $2 WHERE id = $1`
		if _, err := s.db.ExecContext(ctx, query, taskID, *updates.State); err != nil {
			return fmt.Errorf("failed to update state: %w", err)
		}
	}

	if updates.Priority != nil {
		query := `UPDATE queue_tasks SET priority = $2 WHERE id = $1`
		if _, err := s.db.ExecContext(ctx, query, taskID, *updates.Priority); err != nil {
			return fmt.Errorf("failed to update priority: %w", err)
		}
	}

	if updates.MaxRetries != nil {
		query := `UPDATE queue_tasks SET max_retries = $2 WHERE id = $1`
		if _, err := s.db.ExecContext(ctx, query, taskID, *updates.MaxRetries); err != nil {
			return fmt.Errorf("failed to update max_retries: %w", err)
		}
	}

	if updates.Timeout != nil {
		query := `UPDATE queue_tasks SET timeout_seconds = $2 WHERE id = $1`
		seconds := int(updates.Timeout.Seconds())
		if _, err := s.db.ExecContext(ctx, query, taskID, seconds); err != nil {
			return fmt.Errorf("failed to update timeout: %w", err)
		}
	}

	if updates.ScheduledAt != nil {
		query := `UPDATE queue_tasks SET scheduled_at = $2 WHERE id = $1`
		if _, err := s.db.ExecContext(ctx, query, taskID, *updates.ScheduledAt); err != nil {
			return fmt.Errorf("failed to update scheduled_at: %w", err)
		}
	}

	return nil
}
