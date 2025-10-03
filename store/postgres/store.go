package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/internal/jsonutil"
	"github.com/lib/pq"
)

// Store implements ergon.Store interface using PostgreSQL
type Store struct {
	db *sql.DB
}

// NewStore creates a new PostgreSQL store
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection cannot be nil")
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Store{db: db}, nil
}

// NewStoreFromDSN creates a store from a connection string
func NewStoreFromDSN(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return NewStore(db)
}

// Enqueue adds a task to the queue
func (s *Store) Enqueue(ctx context.Context, task *ergon.InternalTask) error {
	query := `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
	`

	// Payload is already JSON bytes - no need to marshal again
	var metadataJSON []byte
	var err error
	if task.Metadata != nil {
		metadataJSON, err = jsonutil.Marshal(task.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	var intervalSeconds *int
	if task.Interval > 0 {
		seconds := int(task.Interval.Seconds())
		intervalSeconds = &seconds
	}

	_, err = s.db.ExecContext(ctx, query,
		task.ID,
		task.Kind,
		task.Queue,
		task.State,
		task.Priority,
		task.MaxRetries,
		task.Retried,
		int(task.Timeout.Seconds()),
		task.ScheduledAt,
		task.EnqueuedAt,
		task.Payload, // Already JSON bytes
		metadataJSON,
		nullString(task.UniqueKey),
		nullString(task.GroupKey),
		nullString(task.RateLimitScope),
		task.Recurring,
		nullString(task.CronSchedule),
		intervalSeconds,
	)

	if err != nil {
		// Check for unique constraint violation
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" { // unique_violation
				return ergon.ErrDuplicateTask
			}
		}
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

// EnqueueMany adds multiple tasks in a single transaction
func (s *Store) EnqueueMany(ctx context.Context, tasks []*ergon.InternalTask) error {
	if len(tasks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, task := range tasks {
		var metadataJSON []byte
		var err error
		if task.Metadata != nil {
			metadataJSON, err = jsonutil.Marshal(task.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}
		}

		var intervalSeconds *int
		if task.Interval > 0 {
			seconds := int(task.Interval.Seconds())
			intervalSeconds = &seconds
		}

		_, err = stmt.ExecContext(ctx,
			task.ID, task.Kind, task.Queue, task.State, task.Priority,
			task.MaxRetries, task.Retried, int(task.Timeout.Seconds()),
			task.ScheduledAt, task.EnqueuedAt, task.Payload, metadataJSON, // Payload already JSON
			nullString(task.UniqueKey), nullString(task.GroupKey),
			nullString(task.RateLimitScope), task.Recurring,
			nullString(task.CronSchedule), intervalSeconds,
		)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok {
				if pqErr.Code == "23505" {
					return ergon.ErrDuplicateTask
				}
			}
			return fmt.Errorf("failed to insert task %s: %w", task.ID, err)
		}
	}

	return tx.Commit()
}

// EnqueueTx adds a task within an existing transaction
func (s *Store) EnqueueTx(ctx context.Context, tx ergon.Tx, task *ergon.InternalTask) error {
	pgTx, ok := tx.(*Tx)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}

	query := `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	// Payload is already JSON - no double marshaling needed
	payloadJSON := task.Payload

	var metadataJSON []byte
	var err error
	if task.Metadata != nil {
		metadataJSON, err = jsonutil.Marshal(task.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	var intervalSeconds *int
	if task.Interval > 0 {
		seconds := int(task.Interval.Seconds())
		intervalSeconds = &seconds
	}

	_, err = pgTx.tx.ExecContext(ctx, query,
		task.ID, task.Kind, task.Queue, task.State, task.Priority,
		task.MaxRetries, task.Retried, int(task.Timeout.Seconds()),
		task.ScheduledAt, task.EnqueuedAt, payloadJSON, metadataJSON,
		nullString(task.UniqueKey), nullString(task.GroupKey),
		nullString(task.RateLimitScope), task.Recurring,
		nullString(task.CronSchedule), intervalSeconds,
	)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" {
				return ergon.ErrDuplicateTask
			}
		}
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

// Dequeue retrieves and locks a task for processing
func (s *Store) Dequeue(ctx context.Context, queues []string, workerID string) (*ergon.InternalTask, error) {
	// Use FOR UPDATE SKIP LOCKED for efficient locking
	query := `
		UPDATE queue_tasks
		SET state = 'running', started_at = NOW()
		WHERE id = (
			SELECT id FROM queue_tasks
			WHERE queue = ANY($1)
			  AND state = 'pending'
			ORDER BY priority DESC, enqueued_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, kind, queue, state, priority, max_retries, retried,
		          timeout_seconds, scheduled_at, enqueued_at, started_at,
		          completed_at, payload, metadata, error, result,
		          unique_key, group_key, rate_limit_scope,
		          recurring, cron_schedule, interval_seconds
	`

	var task ergon.InternalTask
	var timeoutSeconds int
	var payloadJSON, metadataJSON, resultBytes []byte
	var errorStr sql.NullString
	var uniqueKey, groupKey, rateLimitScope, cronSchedule sql.NullString
	var intervalSeconds sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, pq.Array(queues)).Scan(
		&task.ID, &task.Kind, &task.Queue, &task.State, &task.Priority,
		&task.MaxRetries, &task.Retried, &timeoutSeconds,
		&task.ScheduledAt, &task.EnqueuedAt, &task.StartedAt, &task.CompletedAt,
		&payloadJSON, &metadataJSON, &errorStr, &resultBytes,
		&uniqueKey, &groupKey, &rateLimitScope,
		&task.Recurring, &cronSchedule, &intervalSeconds,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ergon.ErrTaskNotFound
		}
		return nil, fmt.Errorf("failed to dequeue task: %w", err)
	}

	// Parse fields
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

// GetTask retrieves a task by ID
func (s *Store) GetTask(ctx context.Context, taskID string) (*ergon.InternalTask, error) {
	query := `
		SELECT id, kind, queue, state, priority, max_retries, retried,
		       timeout_seconds, scheduled_at, enqueued_at, started_at,
		       completed_at, payload, metadata, error, result,
		       unique_key, group_key, rate_limit_scope,
		       recurring, cron_schedule, interval_seconds
		FROM queue_tasks
		WHERE id = $1
	`

	var task ergon.InternalTask
	var timeoutSeconds int
	var payloadJSON, metadataJSON, resultBytes []byte
	var errorStr sql.NullString
	var uniqueKey, groupKey, rateLimitScope, cronSchedule sql.NullString
	var intervalSeconds sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, taskID).Scan(
		&task.ID, &task.Kind, &task.Queue, &task.State, &task.Priority,
		&task.MaxRetries, &task.Retried, &timeoutSeconds,
		&task.ScheduledAt, &task.EnqueuedAt, &task.StartedAt, &task.CompletedAt,
		&payloadJSON, &metadataJSON, &errorStr, &resultBytes,
		&uniqueKey, &groupKey, &rateLimitScope,
		&task.Recurring, &cronSchedule, &intervalSeconds,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ergon.ErrTaskNotFound
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	// Parse fields
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

// DeleteTask deletes a task by ID
func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM queue_tasks WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
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

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks database connectivity
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// GetDB returns the underlying database connection (for advanced usage)
func (s *Store) GetDB() *sql.DB {
	return s.db
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
