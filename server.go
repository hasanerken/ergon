package ergon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Server processes tasks from the queue
type Server struct {
	store   Store
	config  ServerConfig
	workers *Workers

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// ServerConfig configures the server
type ServerConfig struct {
	// Worker settings
	Concurrency int
	Queues      map[string]QueueConfig
	Workers     *Workers // Required!

	// Lifecycle
	ShutdownTimeout time.Duration

	// Defaults
	DefaultTimeout time.Duration
	DefaultRetries int

	// Error handling
	ErrorHandler ErrorHandler

	// Global middleware (applied to all workers)
	Middleware []MiddlewareFunc

	// Retry
	RetryDelayFunc func(task *InternalTask, attempt int, err error) time.Duration
	IsFailure      func(error) bool

	// Aggregation
	EnableAggregation   bool          // Enable task aggregation
	AggregationInterval time.Duration // How often to check groups
	AggregationMaxSize  int           // Default max group size
	AggregationMaxDelay time.Duration // Default max group delay
}

// QueueConfig configures a queue
type QueueConfig struct {
	MaxWorkers   int
	Priority     int
	PollInterval time.Duration
}

// ErrorHandler handles errors from task execution
type ErrorHandler interface {
	HandleError(ctx context.Context, task *InternalTask, err error)
	HandlePanic(ctx context.Context, task *InternalTask, panicVal interface{}, stackTrace string)
}

// NewServer creates a new queue server
func NewServer(store Store, cfg ServerConfig) (*Server, error) {
	if cfg.Workers == nil {
		return nil, fmt.Errorf("workers required")
	}

	// Apply defaults
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 10
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.DefaultRetries == 0 {
		cfg.DefaultRetries = 25
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	// Default queues if none specified
	if cfg.Queues == nil {
		cfg.Queues = map[string]QueueConfig{
			"default": {
				MaxWorkers:   cfg.Concurrency,
				Priority:     1,
				PollInterval: 1 * time.Second,
			},
		}
	}

	// Add recovery middleware if not present
	hasRecovery := false
	for _, mw := range cfg.Middleware {
		// This is a simple check; in production you'd want a better way
		if fmt.Sprintf("%v", mw) == fmt.Sprintf("%v", RecoveryMiddleware()) {
			hasRecovery = true
			break
		}
	}
	if !hasRecovery {
		cfg.Middleware = append([]MiddlewareFunc{RecoveryMiddleware()}, cfg.Middleware...)
	}

	return &Server{
		store:   store,
		config:  cfg,
		workers: cfg.Workers,
	}, nil
}

// Start begins processing tasks
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true

	// Start workers for each queue
	for queueName, queueCfg := range s.config.Queues {
		for i := 0; i < queueCfg.MaxWorkers; i++ {
			s.wg.Add(1)
			go s.worker(ctx, queueName, queueCfg)
		}
	}

	// Start scheduler (moves scheduled tasks to available)
	s.wg.Add(1)
	go s.scheduler(ctx)

	// Start aggregation scheduler if enabled
	if s.config.EnableAggregation {
		s.wg.Add(1)
		go s.aggregationScheduler(ctx)
	}

	log.Printf("[Queue] Server started with %d total workers", s.totalWorkers())
	return nil
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return ErrServerStopped
	}
	s.mu.Unlock()

	log.Printf("[Queue] Shutting down server...")

	// Signal workers to stop
	s.cancel()

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[Queue] Server stopped gracefully")
	case <-ctx.Done():
		log.Printf("[Queue] Server stopped (timeout)")
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// Run blocks until context is cancelled
func (s *Server) Run(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	return s.Stop(shutdownCtx)
}

// worker processes tasks from a queue
func (s *Server) worker(ctx context.Context, queueName string, queueCfg QueueConfig) {
	defer s.wg.Done()

	pollInterval := queueCfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 1 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Dequeue task
			task, err := s.store.Dequeue(ctx, []string{queueName}, generateWorkerID())
			if err != nil {
				if !errors.Is(err, ErrTaskNotFound) {
					log.Printf("[Queue] Error dequeuing task: %v", err)
				}
				continue
			}

			if task == nil {
				continue
			}

			// Process task
			s.processTask(ctx, task)
		}
	}
}

// scheduler moves scheduled tasks to available state
func (s *Server) scheduler(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := s.store.MoveScheduledToAvailable(ctx, time.Now())
			if err != nil {
				log.Printf("[Queue] Scheduler error: %v", err)
			} else if count > 0 {
				log.Printf("[Queue] Scheduler moved %d tasks to available", count)
			}
		}
	}
}

// aggregationScheduler checks groups and triggers aggregated task processing
func (s *Server) aggregationScheduler(ctx context.Context) {
	defer s.wg.Done()

	interval := s.config.AggregationInterval
	if interval == 0 {
		interval = 10 * time.Second // Default: check every 10 seconds
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processAggregationGroups(ctx)
		}
	}
}

// processAggregationGroups checks all groups and processes ready ones
func (s *Server) processAggregationGroups(ctx context.Context) {
	// Get list of all groups (this requires a new method on Store)
	// For now, we'll use a type assertion to access Badger-specific method
	type groupLister interface {
		ListGroups(ctx context.Context) ([]*struct {
			GroupKey   string
			TaskCount  int
			FirstTask  time.Time
			LastUpdate time.Time
		}, error)
	}

	lister, ok := s.store.(groupLister)
	if !ok {
		// Store doesn't support listing groups
		return
	}

	groups, err := lister.ListGroups(ctx)
	if err != nil {
		log.Printf("[Queue] Aggregation scheduler error: %v", err)
		return
	}

	now := time.Now()
	maxSize := s.config.AggregationMaxSize
	if maxSize == 0 {
		maxSize = 100 // Default max group size
	}
	maxDelay := s.config.AggregationMaxDelay
	if maxDelay == 0 {
		maxDelay = 5 * time.Minute // Default max delay
	}

	for _, group := range groups {
		shouldProcess := false

		// Check if group reached max size
		if group.TaskCount >= maxSize {
			shouldProcess = true
			log.Printf("[Queue] Group %s ready: size %d >= %d", group.GroupKey, group.TaskCount, maxSize)
		}

		// Check if group exceeded max delay
		age := now.Sub(group.FirstTask)
		if age >= maxDelay {
			shouldProcess = true
			log.Printf("[Queue] Group %s ready: age %v >= %v", group.GroupKey, age, maxDelay)
		}

		if shouldProcess {
			// Consume group and create aggregated task
			tasks, err := s.store.ConsumeGroup(ctx, group.GroupKey)
			if err != nil {
				log.Printf("[Queue] Error consuming group %s: %v", group.GroupKey, err)
				continue
			}

			if len(tasks) == 0 {
				continue
			}

			// Create aggregated task from first task in group
			aggregatedTask := tasks[0]
			aggregatedTask.State = StatePending
			aggregatedTask.GroupKey = "" // Clear group key
			// Note: In a full implementation, you'd want to handle aggregated tasks
			// differently, perhaps with a special field containing all the grouped tasks

			// Enqueue the aggregated task
			if err := s.store.Enqueue(ctx, aggregatedTask); err != nil {
				log.Printf("[Queue] Error enqueueing aggregated task: %v", err)
				continue
			}

			log.Printf("[Queue] Aggregated %d tasks from group %s", len(tasks), group.GroupKey)
		}
	}
}

// processTask executes a task with middleware chain
func (s *Server) processTask(ctx context.Context, task *InternalTask) {
	// Get worker entry
	entry, ok := s.workers.Get(task.Kind)
	if !ok {
		log.Printf("[Queue] No worker found for kind %q", task.Kind)
		_ = s.store.MarkFailed(ctx, task.ID, ErrWorkerNotFound)
		return
	}

	// Mark task as running
	if err := s.store.MarkRunning(ctx, task.ID, generateWorkerID()); err != nil {
		log.Printf("[Queue] Failed to mark task as running: %v", err)
		return
	}

	// Build execution chain
	executor := entry.execute

	// Apply worker-specific middleware
	for i := len(entry.middleware) - 1; i >= 0; i-- {
		executor = entry.middleware[i](executor)
	}

	// Apply global middleware
	for i := len(s.config.Middleware) - 1; i >= 0; i-- {
		executor = s.config.Middleware[i](executor)
	}

	// Apply timeout
	timeout := s.config.DefaultTimeout
	if entry.timeout != nil {
		if t := entry.timeout(task); t > 0 {
			timeout = t
		}
	}
	if task.Timeout > 0 {
		timeout = task.Timeout
	}

	taskCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Execute task
	startTime := time.Now()
	task.StartedAt = &startTime
	err := executor(taskCtx, task)

	// Handle result
	if err != nil {
		s.handleTaskError(ctx, task, err)
	} else {
		s.handleTaskSuccess(ctx, task)
	}
}

// handleTaskSuccess handles successful task completion
func (s *Server) handleTaskSuccess(ctx context.Context, task *InternalTask) {
	// Check if this is a recurring task
	if task.Recurring {
		s.rescheduleRecurringTask(ctx, task)
	} else {
		// Normal task - mark as completed
		if err := s.store.MarkCompleted(ctx, task.ID, task.Result); err != nil {
			log.Printf("[Queue] Failed to mark task as completed: %v", err)
		}
	}
}

// rescheduleRecurringTask creates a new instance of a recurring task
func (s *Server) rescheduleRecurringTask(ctx context.Context, task *InternalTask) {
	var nextRun time.Time

	// Calculate next run time
	if task.Interval > 0 {
		// Interval-based recurring task
		nextRun = time.Now().Add(task.Interval)
	} else if task.CronSchedule != "" {
		// Cron-based recurring task
		// For now, use simple interval logic (full cron parsing would require a library)
		// TODO: Integrate a cron parser library for full cron support
		nextRun = time.Now().Add(1 * time.Hour) // Fallback
		log.Printf("[Queue] Cron parsing not fully implemented, using 1 hour interval for task %s", task.ID)
	} else {
		log.Printf("[Queue] Recurring task %s has no interval or cron schedule", task.ID)
		return
	}

	// Create new task instance with same properties
	newTask := &InternalTask{
		ID:           generateID(), // New ID for new instance
		Kind:         task.Kind,
		Queue:        task.Queue,
		State:        StateScheduled,
		Priority:     task.Priority,
		MaxRetries:   task.MaxRetries,
		Timeout:      task.Timeout,
		ScheduledAt:  &nextRun,
		EnqueuedAt:   time.Now(),
		Payload:      task.Payload,
		Metadata:     task.Metadata,
		Recurring:    true,
		CronSchedule: task.CronSchedule,
		Interval:     task.Interval,
	}

	// Enqueue the next instance
	if err := s.store.Enqueue(ctx, newTask); err != nil {
		log.Printf("[Queue] Failed to reschedule recurring task: %v", err)
		return
	}

	// Mark current instance as completed
	if err := s.store.MarkCompleted(ctx, task.ID, task.Result); err != nil {
		log.Printf("[Queue] Failed to mark recurring task as completed: %v", err)
	}

	log.Printf("[Queue] Recurring task %s rescheduled for %v (new ID: %s)",
		task.ID, nextRun, newTask.ID)
}

// handleTaskError handles task failure
func (s *Server) handleTaskError(ctx context.Context, task *InternalTask, err error) {
	// Check if it's a cancellation error
	if IsJobCancelled(err) {
		log.Printf("[Queue] Task %s cancelled: %v", task.ID, err)
		_ = s.store.MarkCancelled(ctx, task.ID)
		return
	}

	// Check if it's a snooze error
	if IsJobSnoozed(err) {
		if duration, ok := GetSnoozeDuration(err); ok {
			nextRun := time.Now().Add(duration)
			log.Printf("[Queue] Task %s snoozed until %v", task.ID, nextRun)
			_ = s.store.MarkRetrying(ctx, task.ID, nextRun)
		}
		return
	}

	// Check if we should retry
	shouldRetry := true
	if s.config.IsFailure != nil {
		shouldRetry = s.config.IsFailure(err)
	}

	if shouldRetry && task.Retried < task.MaxRetries {
		// Calculate retry delay
		var retryDelay time.Duration

		// Use worker-specific retry delay if available
		entry, ok := s.workers.Get(task.Kind)
		if ok && entry.retryDelay != nil {
			retryDelay = entry.retryDelay(task, task.Retried+1, err)
		} else if s.config.RetryDelayFunc != nil {
			retryDelay = s.config.RetryDelayFunc(task, task.Retried+1, err)
		} else {
			// Default exponential backoff
			retryDelay = defaultRetryDelay(task.Retried + 1)
		}

		nextRetry := time.Now().Add(retryDelay)
		log.Printf("[Queue] Task %s will retry in %v (attempt %d/%d): %v",
			task.ID, retryDelay, task.Retried+1, task.MaxRetries, err)

		_ = s.store.MarkRetrying(ctx, task.ID, nextRetry)
	} else {
		// Exhausted retries or non-retryable error
		log.Printf("[Queue] Task %s failed permanently: %v", task.ID, err)
		_ = s.store.MarkFailed(ctx, task.ID, err)
	}

	// Call error handler if configured
	if s.config.ErrorHandler != nil {
		s.config.ErrorHandler.HandleError(ctx, task, err)
	}
}

// totalWorkers returns total number of workers
func (s *Server) totalWorkers() int {
	total := 0
	for _, queueCfg := range s.config.Queues {
		total += queueCfg.MaxWorkers
	}
	return total
}

// defaultRetryDelay calculates default exponential backoff
func defaultRetryDelay(attempt int) time.Duration {
	// Exponential backoff: 2^attempt seconds, capped at 1 hour
	delay := time.Duration(1<<uint(attempt)) * time.Second
	if delay > time.Hour {
		delay = time.Hour
	}
	return delay
}

// generateWorkerID generates a worker identifier
func generateWorkerID() string {
	return fmt.Sprintf("worker-%s", generateID()[:8])
}
