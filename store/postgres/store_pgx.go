package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hasanerken/ergon"
	"github.com/hasanerken/ergon/internal/jsonutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StorePgx implements ergon.Store using pgx (high-performance PostgreSQL driver)
type StorePgx struct {
	pool *pgxpool.Pool
}

// PgxConfig holds configuration options for the pgx store
type PgxConfig struct {
	// DSN is the connection string
	DSN string

	// Pool settings
	MaxConns        int32         // Maximum connections in pool (default: 100)
	MinConns        int32         // Minimum connections in pool (default: 10)
	MaxConnLifetime time.Duration // Maximum connection lifetime (default: 1 hour)
	MaxConnIdleTime time.Duration // Maximum idle time (default: 30 minutes)
	HealthCheckPeriod time.Duration // Health check interval (default: 1 minute)

	// PgBouncer compatibility
	UsePgBouncer bool // Set to true when connecting through PgBouncer (disables prepared statements)
}

// NewStorePgx creates a new pgx-based PostgreSQL store with optimized pooling
func NewStorePgx(ctx context.Context, dsn string) (*StorePgx, error) {
	return NewStorePgxWithConfig(ctx, PgxConfig{
		DSN: dsn,
	})
}

// NewStorePgxWithConfig creates a new pgx-based PostgreSQL store with custom configuration
func NewStorePgxWithConfig(ctx context.Context, cfg PgxConfig) (*StorePgx, error) {
	config, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Apply pool settings with defaults
	if cfg.MaxConns > 0 {
		config.MaxConns = cfg.MaxConns
	} else {
		config.MaxConns = 100 // Default: high throughput
	}

	if cfg.MinConns > 0 {
		config.MinConns = cfg.MinConns
	} else {
		config.MinConns = 10 // Default: keep connections warm
	}

	if cfg.MaxConnLifetime > 0 {
		config.MaxConnLifetime = cfg.MaxConnLifetime
	} else {
		config.MaxConnLifetime = 1 * time.Hour // Default: recycle hourly
	}

	if cfg.MaxConnIdleTime > 0 {
		config.MaxConnIdleTime = cfg.MaxConnIdleTime
	} else {
		config.MaxConnIdleTime = 30 * time.Minute // Default: close idle after 30 min
	}

	if cfg.HealthCheckPeriod > 0 {
		config.HealthCheckPeriod = cfg.HealthCheckPeriod
	} else {
		config.HealthCheckPeriod = 1 * time.Minute // Default: check every minute
	}

	// Performance optimizations
	if cfg.UsePgBouncer {
		// PgBouncer compatibility: Use simple protocol (no prepared statements)
		// This is required when using PgBouncer in transaction or statement mode
		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	} else {
		// Direct PostgreSQL: Use prepared statements for better performance
		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &StorePgx{pool: pool}, nil
}

// Enqueue adds a task to the queue (optimized with prepared statement caching)
func (s *StorePgx) Enqueue(ctx context.Context, task *ergon.InternalTask) error {
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

	var metadataJSON *[]byte
	var err error
	if task.Metadata != nil {
		data, marshalErr := jsonutil.Marshal(task.Metadata)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal metadata: %w", marshalErr)
		}
		metadataJSON = &data
	}

	var intervalSeconds *int
	if task.Interval > 0 {
		seconds := int(task.Interval.Seconds())
		intervalSeconds = &seconds
	}

	_, err = s.pool.Exec(ctx, query,
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
		task.Payload, // pgx handles []byte directly - no extra marshaling!
		metadataJSON, // Pass as *[]byte so nil is handled correctly
		nullStringPgx(task.UniqueKey),
		nullStringPgx(task.GroupKey),
		nullStringPgx(task.RateLimitScope),
		task.Recurring,
		nullStringPgx(task.CronSchedule),
		intervalSeconds,
	)

	if err != nil {
		// Check for unique constraint violation
		if pgErr, ok := err.(*pgconn.PgError); ok {
			if pgErr.Code == "23505" { // unique_violation
				return ergon.ErrDuplicateTask
			}
		}
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

// EnqueueMany adds multiple tasks using batch protocol for maximum performance
func (s *StorePgx) EnqueueMany(ctx context.Context, tasks []*ergon.InternalTask) error {
	if len(tasks) == 0 {
		return nil
	}

	// Use batch insert for better performance
	batch := &pgx.Batch{}

	query := `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

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

		batch.Queue(query,
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
			task.Payload,
			metadataJSON,
			nullStringPgx(task.UniqueKey),
			nullStringPgx(task.GroupKey),
			nullStringPgx(task.RateLimitScope),
			task.Recurring,
			nullStringPgx(task.CronSchedule),
			intervalSeconds,
		)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	// Process all results
	for i := 0; i < len(tasks); i++ {
		_, err := br.Exec()
		if err != nil {
			if pgErr, ok := err.(*pgconn.PgError); ok {
				if pgErr.Code == "23505" {
					return ergon.ErrDuplicateTask
				}
			}
			return fmt.Errorf("failed to insert task %d: %w", i, err)
		}
	}

	return nil
}

// EnqueueTx adds a task within an existing transaction
func (s *StorePgx) EnqueueTx(ctx context.Context, tx ergon.Tx, task *ergon.InternalTask) error {
	pgxTx, ok := tx.(*TxPgx)
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

	_, err = pgxTx.tx.Exec(ctx, query,
		task.ID, task.Kind, task.Queue, task.State, task.Priority,
		task.MaxRetries, task.Retried, int(task.Timeout.Seconds()),
		task.ScheduledAt, task.EnqueuedAt, task.Payload, metadataJSON,
		nullStringPgx(task.UniqueKey), nullStringPgx(task.GroupKey),
		nullStringPgx(task.RateLimitScope), task.Recurring,
		nullStringPgx(task.CronSchedule), intervalSeconds,
	)

	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			if pgErr.Code == "23505" {
				return ergon.ErrDuplicateTask
			}
		}
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

// Dequeue retrieves and locks a task for processing
func (s *StorePgx) Dequeue(ctx context.Context, queues []string, workerID string) (*ergon.InternalTask, error) {
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
	var errorStr, uniqueKey, groupKey, rateLimitScope, cronSchedule *string
	var intervalSeconds *int64

	err := s.pool.QueryRow(ctx, query, queues).Scan(
		&task.ID, &task.Kind, &task.Queue, &task.State, &task.Priority,
		&task.MaxRetries, &task.Retried, &timeoutSeconds,
		&task.ScheduledAt, &task.EnqueuedAt, &task.StartedAt, &task.CompletedAt,
		&payloadJSON, &metadataJSON, &errorStr, &resultBytes,
		&uniqueKey, &groupKey, &rateLimitScope,
		&task.Recurring, &cronSchedule, &intervalSeconds,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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

	if errorStr != nil {
		task.Error = *errorStr
	}
	task.Result = resultBytes

	if uniqueKey != nil {
		task.UniqueKey = *uniqueKey
	}
	if groupKey != nil {
		task.GroupKey = *groupKey
	}
	if rateLimitScope != nil {
		task.RateLimitScope = *rateLimitScope
	}
	if cronSchedule != nil {
		task.CronSchedule = *cronSchedule
	}

	if intervalSeconds != nil {
		task.Interval = time.Duration(*intervalSeconds) * time.Second
	}

	return &task, nil
}

// GetTask retrieves a task by ID
func (s *StorePgx) GetTask(ctx context.Context, taskID string) (*ergon.InternalTask, error) {
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
	var errorStr, uniqueKey, groupKey, rateLimitScope, cronSchedule *string
	var intervalSeconds *int64

	err := s.pool.QueryRow(ctx, query, taskID).Scan(
		&task.ID, &task.Kind, &task.Queue, &task.State, &task.Priority,
		&task.MaxRetries, &task.Retried, &timeoutSeconds,
		&task.ScheduledAt, &task.EnqueuedAt, &task.StartedAt, &task.CompletedAt,
		&payloadJSON, &metadataJSON, &errorStr, &resultBytes,
		&uniqueKey, &groupKey, &rateLimitScope,
		&task.Recurring, &cronSchedule, &intervalSeconds,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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

	if errorStr != nil {
		task.Error = *errorStr
	}
	task.Result = resultBytes

	if uniqueKey != nil {
		task.UniqueKey = *uniqueKey
	}
	if groupKey != nil {
		task.GroupKey = *groupKey
	}
	if rateLimitScope != nil {
		task.RateLimitScope = *rateLimitScope
	}
	if cronSchedule != nil {
		task.CronSchedule = *cronSchedule
	}

	if intervalSeconds != nil {
		task.Interval = time.Duration(*intervalSeconds) * time.Second
	}

	return &task, nil
}

// ListTasks lists tasks matching the filter
func (s *StorePgx) ListTasks(ctx context.Context, filter *ergon.TaskFilter) ([]*ergon.InternalTask, error) {
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

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*ergon.InternalTask
	for rows.Next() {
		task, err := scanTaskPgx(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// CountTasks counts tasks matching the filter
func (s *StorePgx) CountTasks(ctx context.Context, filter *ergon.TaskFilter) (int, error) {
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
	err := s.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	return count, nil
}

// UpdateTask updates task fields
func (s *StorePgx) UpdateTask(ctx context.Context, taskID string, updates *ergon.TaskUpdate) error {
	// Build dynamic UPDATE query based on what fields are set
	if updates.State != nil {
		query := `UPDATE queue_tasks SET state = $2 WHERE id = $1`
		if _, err := s.pool.Exec(ctx, query, taskID, *updates.State); err != nil {
			return fmt.Errorf("failed to update state: %w", err)
		}
	}

	if updates.Priority != nil {
		query := `UPDATE queue_tasks SET priority = $2 WHERE id = $1`
		if _, err := s.pool.Exec(ctx, query, taskID, *updates.Priority); err != nil {
			return fmt.Errorf("failed to update priority: %w", err)
		}
	}

	if updates.MaxRetries != nil {
		query := `UPDATE queue_tasks SET max_retries = $2 WHERE id = $1`
		if _, err := s.pool.Exec(ctx, query, taskID, *updates.MaxRetries); err != nil {
			return fmt.Errorf("failed to update max_retries: %w", err)
		}
	}

	if updates.Timeout != nil {
		query := `UPDATE queue_tasks SET timeout_seconds = $2 WHERE id = $1`
		seconds := int(updates.Timeout.Seconds())
		if _, err := s.pool.Exec(ctx, query, taskID, seconds); err != nil {
			return fmt.Errorf("failed to update timeout: %w", err)
		}
	}

	if updates.ScheduledAt != nil {
		query := `UPDATE queue_tasks SET scheduled_at = $2 WHERE id = $1`
		if _, err := s.pool.Exec(ctx, query, taskID, *updates.ScheduledAt); err != nil {
			return fmt.Errorf("failed to update scheduled_at: %w", err)
		}
	}

	return nil
}

// DeleteTask deletes a task by ID
func (s *StorePgx) DeleteTask(ctx context.Context, taskID string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM queue_tasks WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkRunning marks a task as running
func (s *StorePgx) MarkRunning(ctx context.Context, taskID string, workerID string) error {
	query := `
		UPDATE queue_tasks
		SET state = 'running', started_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as running: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkCompleted marks a task as completed
func (s *StorePgx) MarkCompleted(ctx context.Context, taskID string, result []byte) error {
	query := `
		UPDATE queue_tasks
		SET state = 'completed', completed_at = NOW(), result = $2
		WHERE id = $1
	`

	res, err := s.pool.Exec(ctx, query, taskID, result)
	if err != nil {
		return fmt.Errorf("failed to mark task as completed: %w", err)
	}

	if res.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkFailed marks a task as failed
func (s *StorePgx) MarkFailed(ctx context.Context, taskID string, taskErr error) error {
	errorMsg := ""
	if taskErr != nil {
		errorMsg = taskErr.Error()
	}

	query := `
		UPDATE queue_tasks
		SET state = 'failed', completed_at = NOW(), error = $2
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, taskID, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkRetrying marks a task for retry
func (s *StorePgx) MarkRetrying(ctx context.Context, taskID string, nextRetry time.Time) error {
	query := `
		UPDATE queue_tasks
		SET state = 'retrying', scheduled_at = $2, retried = retried + 1
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, taskID, nextRetry)
	if err != nil {
		return fmt.Errorf("failed to mark task for retry: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MarkCancelled marks a task as cancelled
func (s *StorePgx) MarkCancelled(ctx context.Context, taskID string) error {
	query := `
		UPDATE queue_tasks
		SET state = 'cancelled', completed_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as cancelled: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ergon.ErrTaskNotFound
	}

	return nil
}

// MoveScheduledToAvailable moves scheduled and retry tasks to pending state
func (s *StorePgx) MoveScheduledToAvailable(ctx context.Context, before time.Time) (int, error) {
	query := `
		UPDATE queue_tasks
		SET state = 'pending', scheduled_at = NULL
		WHERE state IN ('scheduled', 'retrying')
		  AND scheduled_at <= $1
	`

	result, err := s.pool.Exec(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to move scheduled tasks: %w", err)
	}

	return int(result.RowsAffected()), nil
}

// ListQueues lists all queues
func (s *StorePgx) ListQueues(ctx context.Context) ([]*ergon.QueueInfo, error) {
	// Get unique queues from tasks
	rows, err := s.pool.Query(ctx, `
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
		var paused *bool
		err := s.pool.QueryRow(ctx, `
			SELECT paused FROM queue_info WHERE queue_name = $1
		`, q.Name).Scan(&paused)
		if err == nil && paused != nil {
			q.Paused = *paused
		}

		queues = append(queues, &q)
	}

	return queues, rows.Err()
}

// GetQueueInfo gets info for a specific queue
func (s *StorePgx) GetQueueInfo(ctx context.Context, queueName string) (*ergon.QueueInfo, error) {
	var q ergon.QueueInfo
	q.Name = queueName

	err := s.pool.QueryRow(ctx, `
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
	var paused *bool
	err = s.pool.QueryRow(ctx, `
		SELECT paused FROM queue_info WHERE queue_name = $1
	`, queueName).Scan(&paused)
	if err == nil && paused != nil {
		q.Paused = *paused
	}

	return &q, nil
}

// PauseQueue pauses a queue
func (s *StorePgx) PauseQueue(ctx context.Context, queueName string) error {
	_, err := s.pool.Exec(ctx, `
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
func (s *StorePgx) ResumeQueue(ctx context.Context, queueName string) error {
	_, err := s.pool.Exec(ctx, `
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

// AddToGroup adds a task to an aggregation group
func (s *StorePgx) AddToGroup(ctx context.Context, groupKey string, task *ergon.InternalTask) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Insert task
	var metadataJSON []byte
	if task.Metadata != nil {
		metadataJSON, err = jsonutil.Marshal(task.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO queue_tasks (
			id, kind, queue, state, priority, max_retries, retried,
			timeout_seconds, scheduled_at, enqueued_at, payload, metadata,
			unique_key, group_key, rate_limit_scope,
			recurring, cron_schedule, interval_seconds
		) VALUES ($1, $2, $3, 'aggregating', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`, task.ID, task.Kind, task.Queue, task.Priority, task.MaxRetries, task.Retried,
		int(task.Timeout.Seconds()), task.ScheduledAt, task.EnqueuedAt,
		task.Payload, metadataJSON, nullStringPgx(task.UniqueKey), groupKey,
		nullStringPgx(task.RateLimitScope), task.Recurring,
		nullStringPgx(task.CronSchedule), nil,
	)
	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	// Update or insert group metadata
	_, err = tx.Exec(ctx, `
		INSERT INTO queue_groups (group_key, task_count, first_task_at, last_update_at)
		VALUES ($1, 1, NOW(), NOW())
		ON CONFLICT (group_key) DO UPDATE
		SET task_count = queue_groups.task_count + 1,
		    last_update_at = NOW()
	`, groupKey)
	if err != nil {
		return fmt.Errorf("failed to update group metadata: %w", err)
	}

	return tx.Commit(ctx)
}

// GetGroup retrieves all tasks in a group
func (s *StorePgx) GetGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	rows, err := s.pool.Query(ctx, `
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
		task, err := scanTaskPgx(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// ConsumeGroup retrieves and removes all tasks from a group
func (s *StorePgx) ConsumeGroup(ctx context.Context, groupKey string) ([]*ergon.InternalTask, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get all tasks
	rows, err := tx.Query(ctx, `
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
	for rows.Next() {
		task, err := scanTaskPgx(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	rows.Close()

	if len(tasks) == 0 {
		return tasks, nil
	}

	// Delete tasks
	_, err = tx.Exec(ctx, `
		DELETE FROM queue_tasks
		WHERE group_key = $1 AND state = 'aggregating'
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group tasks: %w", err)
	}

	// Delete group metadata
	_, err = tx.Exec(ctx, `
		DELETE FROM queue_groups WHERE group_key = $1
	`, groupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group metadata: %w", err)
	}

	return tasks, tx.Commit(ctx)
}

// ArchiveTasks archives completed/failed tasks older than the specified time
func (s *StorePgx) ArchiveTasks(ctx context.Context, before time.Time) (int, error) {
	// Simple implementation: just count them
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM queue_tasks
		WHERE state IN ('completed', 'failed', 'cancelled')
		  AND completed_at < $1
	`, before).Scan(&count)

	return count, err
}

// DeleteArchivedTasks deletes archived tasks
func (s *StorePgx) DeleteArchivedTasks(ctx context.Context, before time.Time) (int, error) {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM queue_tasks
		WHERE state IN ('completed', 'failed', 'cancelled')
		  AND completed_at < $1
	`, before)

	if err != nil {
		return 0, fmt.Errorf("failed to delete archived tasks: %w", err)
	}

	return int(result.RowsAffected()), nil
}

// RecoverStuckTasks recovers tasks that have been running too long
func (s *StorePgx) RecoverStuckTasks(ctx context.Context, timeout time.Duration) (int, error) {
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

	result, err := s.pool.Exec(ctx, query, stuckBefore)
	if err != nil {
		return 0, fmt.Errorf("failed to retry stuck tasks: %w", err)
	}

	retriedCount := result.RowsAffected()

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

	result, err = s.pool.Exec(ctx, query, stuckBefore, timeout.String())
	if err != nil {
		return int(retriedCount), fmt.Errorf("failed to fail stuck tasks: %w", err)
	}

	failedCount := result.RowsAffected()

	return int(retriedCount + failedCount), nil
}

// ExtendLease extends the lease on a running task
func (s *StorePgx) ExtendLease(ctx context.Context, taskID string, duration time.Duration) error {
	query := `
		UPDATE queue_tasks
		SET started_at = NOW()
		WHERE id = $1 AND state = 'running'
	`

	result, err := s.pool.Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to extend lease: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("task not found or not running")
	}

	return nil
}

// Subscribe subscribes to task events (PostgreSQL LISTEN/NOTIFY)
func (s *StorePgx) Subscribe(ctx context.Context, events ...ergon.EventType) (<-chan *ergon.Event, error) {
	// PostgreSQL LISTEN/NOTIFY implementation
	// This requires a persistent connection
	// For simplicity, returning not implemented for now
	return nil, fmt.Errorf("subscribe not yet implemented for pgx store")
}

// TryAcquireLease attempts to acquire a distributed lease
func (s *StorePgx) TryAcquireLease(ctx context.Context, leaseKey string, ttl time.Duration) (*ergon.Lease, error) {
	expiresAt := time.Now().Add(ttl)
	leaseValue := fmt.Sprintf("%d", time.Now().UnixNano())

	// Try to insert lease
	_, err := s.pool.Exec(ctx, `
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
	err = s.pool.QueryRow(ctx, `
		SELECT lease_value, expires_at
		FROM queue_leases
		WHERE lease_key = $1
	`, leaseKey).Scan(&currentValue, &currentExpires)

	if err != nil {
		return nil, fmt.Errorf("failed to check lease: %w", err)
	}

	// If expired, try to claim it
	if currentExpires.Before(time.Now()) {
		_, err = s.pool.Exec(ctx, `
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
func (s *StorePgx) RenewLease(ctx context.Context, lease *ergon.Lease) error {
	expiresAt := time.Now().Add(lease.TTL)

	result, err := s.pool.Exec(ctx, `
		UPDATE queue_leases
		SET expires_at = $3
		WHERE lease_key = $1 AND lease_value = $2
	`, lease.Key, lease.Value, expiresAt)

	if err != nil {
		return fmt.Errorf("failed to renew lease: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("lease not found or not owned")
	}

	return nil
}

// ReleaseLease releases a lease
func (s *StorePgx) ReleaseLease(ctx context.Context, lease *ergon.Lease) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM queue_leases
		WHERE lease_key = $1 AND lease_value = $2
	`, lease.Key, lease.Value)

	if err != nil {
		return fmt.Errorf("failed to release lease: %w", err)
	}

	return nil
}

// BeginTx begins a new transaction
func (s *StorePgx) BeginTx(ctx context.Context) (ergon.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &TxPgx{tx: tx}, nil
}

// Close closes the connection pool
func (s *StorePgx) Close() error {
	s.pool.Close()
	return nil
}

// Ping checks if the database is accessible
func (s *StorePgx) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// GetPool returns the underlying connection pool (for direct queries if needed)
func (s *StorePgx) GetPool() *pgxpool.Pool {
	return s.pool
}

// TxPgx wraps a pgx transaction
type TxPgx struct {
	tx pgx.Tx
}

// Commit commits the transaction
func (t *TxPgx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

// Rollback rolls back the transaction
func (t *TxPgx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

// scanTaskPgx scans a task from pgx rows
func scanTaskPgx(scanner interface {
	Scan(dest ...interface{}) error
}) (*ergon.InternalTask, error) {
	var task ergon.InternalTask
	var timeoutSeconds int
	var payloadJSON, metadataJSON, resultBytes []byte
	var errorStr, uniqueKey, groupKey, rateLimitScope, cronSchedule *string
	var intervalSeconds *int64

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

	if errorStr != nil {
		task.Error = *errorStr
	}
	task.Result = resultBytes

	// Handle nullable strings from pgx (uses *string, not sql.NullString)
	if uniqueKey != nil {
		task.UniqueKey = *uniqueKey
	}
	if groupKey != nil {
		task.GroupKey = *groupKey
	}
	if rateLimitScope != nil {
		task.RateLimitScope = *rateLimitScope
	}
	if cronSchedule != nil {
		task.CronSchedule = *cronSchedule
	}

	if intervalSeconds != nil {
		task.Interval = time.Duration(*intervalSeconds) * time.Second
	}

	return &task, nil
}

// Helper functions for pgx (returns *string for NULL-able columns)
func nullStringPgx(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
