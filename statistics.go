package ergon

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"
)

// TaskStatistics provides queue metrics and analytics
type TaskStatistics interface {
	// GetQueueStats returns statistics for a specific queue
	GetQueueStats(ctx context.Context, queue string) (*QueueStatistics, error)

	// GetKindStats returns statistics for a specific task kind
	GetKindStats(ctx context.Context, kind string) (*KindStatistics, error)

	// GetOverallStats returns overall statistics across all queues
	GetOverallStats(ctx context.Context) (*OverallStatistics, error)

	// GetTimeSeries returns time-series data for a queue
	GetTimeSeries(ctx context.Context, opts TimeSeriesOptions) (*TimeSeries, error)

	// GetWorkerStats returns statistics per worker
	GetWorkerStats(ctx context.Context, workerID string) (*WorkerStatistics, error)

	// GetErrorStats returns most common errors
	GetErrorStats(ctx context.Context, queue string, limit int) ([]*ErrorStats, error)
}

// QueueStatistics contains queue-level metrics
type QueueStatistics struct {
	Queue string `json:"queue"`

	// Counts by state
	PendingCount     int `json:"pending_count"`
	RunningCount     int `json:"running_count"`
	ScheduledCount   int `json:"scheduled_count"`
	CompletedCount   int `json:"completed_count"`
	FailedCount      int `json:"failed_count"`
	RetryingCount    int `json:"retrying_count"`
	CancelledCount   int `json:"cancelled_count"`
	AggregatingCount int `json:"aggregating_count"`

	// Performance metrics
	TotalProcessed int64         `json:"total_processed"`
	SuccessRate    float64       `json:"success_rate"`
	FailureRate    float64       `json:"failure_rate"`
	AvgDuration    time.Duration `json:"avg_duration"`
	MedianDuration time.Duration `json:"median_duration"`
	P95Duration    time.Duration `json:"p95_duration"`
	P99Duration    time.Duration `json:"p99_duration"`

	// Throughput
	TasksPerHour   float64 `json:"tasks_per_hour"`
	TasksPerMinute float64 `json:"tasks_per_minute"`

	// Age metrics
	OldestPendingAge time.Duration `json:"oldest_pending_age"`
	AvgWaitTime      time.Duration `json:"avg_wait_time"`

	// Retry metrics
	AvgRetries   float64 `json:"avg_retries"`
	TotalRetries int64   `json:"total_retries"`

	// Status
	IsPaused   bool       `json:"is_paused"`
	LastTaskAt *time.Time `json:"last_task_at"`
}

// KindStatistics contains task kind metrics
type KindStatistics struct {
	Kind string `json:"kind"`

	// Counts
	TotalProcessed int64 `json:"total_processed"`
	TotalFailed    int64 `json:"total_failed"`
	TotalCancelled int64 `json:"total_cancelled"`

	// Performance
	SuccessRate    float64       `json:"success_rate"`
	AvgDuration    time.Duration `json:"avg_duration"`
	MedianDuration time.Duration `json:"median_duration"`
	P95Duration    time.Duration `json:"p95_duration"`

	// Retries
	AvgRetries float64 `json:"avg_retries"`
	MaxRetries int     `json:"max_retries"`

	// Errors
	CommonErrors []*ErrorStats `json:"common_errors"`

	// Timing
	FirstSeenAt *time.Time `json:"first_seen_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

// OverallStatistics contains system-wide metrics
type OverallStatistics struct {
	// Queue counts
	TotalQueues  int `json:"total_queues"`
	ActiveQueues int `json:"active_queues"`
	PausedQueues int `json:"paused_queues"`

	// Task counts
	TotalTasks     int64 `json:"total_tasks"`
	PendingTasks   int64 `json:"pending_tasks"`
	RunningTasks   int64 `json:"running_tasks"`
	ScheduledTasks int64 `json:"scheduled_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks    int64 `json:"failed_tasks"`

	// Performance
	OverallSuccessRate float64       `json:"overall_success_rate"`
	AvgDuration        time.Duration `json:"avg_duration"`
	TasksPerHour       float64       `json:"tasks_per_hour"`

	// Top stats
	BusiestQueue string `json:"busiest_queue"`
	SlowestKind  string `json:"slowest_kind"`
	FailingKind  string `json:"failing_kind"`

	// System health
	OldestTask *time.Time `json:"oldest_task"`
	StuckTasks int        `json:"stuck_tasks"`
}

// ErrorStats contains error frequency data
type ErrorStats struct {
	Error      string    `json:"error"`
	Count      int       `json:"count"`
	Percentage float64   `json:"percentage"`
	LastSeen   time.Time `json:"last_seen"`
}

// TimeSeriesOptions configures time-series queries
type TimeSeriesOptions struct {
	Queue     string        `json:"queue"`
	Kind      string        `json:"kind"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Interval  time.Duration `json:"interval"` // e.g., 1 hour, 1 day
	Limit     int           `json:"limit"`
}

// TimeSeries contains time-series data points
type TimeSeries struct {
	Queue      string        `json:"queue"`
	Kind       string        `json:"kind"`
	Interval   time.Duration `json:"interval"`
	DataPoints []*DataPoint  `json:"data_points"`
}

// DataPoint represents metrics at a specific time
type DataPoint struct {
	Timestamp  time.Time     `json:"timestamp"`
	Enqueued   int           `json:"enqueued"`
	Completed  int           `json:"completed"`
	Failed     int           `json:"failed"`
	AvgLatency time.Duration `json:"avg_latency"`
	P95Latency time.Duration `json:"p95_latency"`
}

// WorkerStatistics contains per-worker metrics
type WorkerStatistics struct {
	WorkerID       string        `json:"worker_id"`
	TasksProcessed int64         `json:"tasks_processed"`
	TasksFailed    int64         `json:"tasks_failed"`
	SuccessRate    float64       `json:"success_rate"`
	AvgDuration    time.Duration `json:"avg_duration"`
	LastActive     *time.Time    `json:"last_active"`
	CurrentTask    *string       `json:"current_task"`
}

// DefaultStatistics implements TaskStatistics interface
type DefaultStatistics struct {
	store Store
}

// NewTaskStatistics creates a new task statistics instance
func NewTaskStatistics(store Store) TaskStatistics {
	return &DefaultStatistics{
		store: store,
	}
}

// GetQueueStats returns statistics for a specific queue
func (s *DefaultStatistics) GetQueueStats(ctx context.Context, queue string) (*QueueStatistics, error) {
	stats := &QueueStatistics{
		Queue: queue,
	}

	// Get counts by state
	counts, err := s.countByState(ctx, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to count by state: %w", err)
	}

	stats.PendingCount = counts[StatePending]
	stats.RunningCount = counts[StateRunning]
	stats.ScheduledCount = counts[StateScheduled]
	stats.CompletedCount = counts[StateCompleted]
	stats.FailedCount = counts[StateFailed]
	stats.RetryingCount = counts[StateRetrying]
	stats.CancelledCount = counts[StateCancelled]
	stats.AggregatingCount = counts[StateAggregating]

	stats.TotalProcessed = int64(stats.CompletedCount + stats.FailedCount + stats.CancelledCount)

	// Calculate success rate
	if stats.TotalProcessed > 0 {
		stats.SuccessRate = float64(stats.CompletedCount) / float64(stats.TotalProcessed)
		stats.FailureRate = float64(stats.FailedCount) / float64(stats.TotalProcessed)
	}

	// Get performance metrics
	perf, err := s.getPerformanceMetrics(ctx, queue, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get performance metrics: %w", err)
	}

	stats.AvgDuration = perf.AvgDuration
	stats.MedianDuration = perf.MedianDuration
	stats.P95Duration = perf.P95Duration
	stats.P99Duration = perf.P99Duration
	stats.AvgRetries = perf.AvgRetries
	stats.TotalRetries = perf.TotalRetries

	// Get throughput metrics
	throughput, err := s.getThroughput(ctx, queue, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get throughput: %w", err)
	}

	stats.TasksPerHour = throughput.PerHour
	stats.TasksPerMinute = throughput.PerMinute
	stats.LastTaskAt = throughput.LastTaskAt

	// Get oldest pending task
	oldestAge, err := s.getOldestPendingAge(ctx, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to get oldest pending age: %w", err)
	}
	stats.OldestPendingAge = oldestAge

	// Get wait time
	waitTime, err := s.getAvgWaitTime(ctx, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to get avg wait time: %w", err)
	}
	stats.AvgWaitTime = waitTime

	// Check if paused
	info, err := s.store.GetQueueInfo(ctx, queue)
	if err == nil && info != nil {
		stats.IsPaused = info.Paused
	}

	return stats, nil
}

// GetKindStats returns statistics for a specific task kind
func (s *DefaultStatistics) GetKindStats(ctx context.Context, kind string) (*KindStatistics, error) {
	stats := &KindStatistics{
		Kind: kind,
	}

	// Get task counts by kind
	counts, err := s.countByKind(ctx, kind)
	if err != nil {
		return nil, fmt.Errorf("failed to count by kind: %w", err)
	}

	stats.TotalProcessed = counts.Completed + counts.Failed
	stats.TotalFailed = counts.Failed
	stats.TotalCancelled = counts.Cancelled

	if stats.TotalProcessed > 0 {
		stats.SuccessRate = float64(counts.Completed) / float64(stats.TotalProcessed)
	}

	// Get performance metrics
	perf, err := s.getPerformanceMetrics(ctx, "", kind)
	if err != nil {
		return nil, fmt.Errorf("failed to get performance metrics: %w", err)
	}

	stats.AvgDuration = perf.AvgDuration
	stats.MedianDuration = perf.MedianDuration
	stats.P95Duration = perf.P95Duration
	stats.AvgRetries = perf.AvgRetries
	stats.MaxRetries = perf.MaxRetries

	// Get error stats
	errorStats, err := s.GetErrorStats(ctx, "", 5)
	if err == nil {
		stats.CommonErrors = errorStats
	}

	// Get timing info
	timing, err := s.getKindTiming(ctx, kind)
	if err == nil {
		stats.FirstSeenAt = timing.FirstSeen
		stats.LastSeenAt = timing.LastSeen
	}

	return stats, nil
}

// GetOverallStats returns overall statistics across all queues
func (s *DefaultStatistics) GetOverallStats(ctx context.Context) (*OverallStatistics, error) {
	stats := &OverallStatistics{}

	// Get all queues
	queues, err := s.store.ListQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}

	stats.TotalQueues = len(queues)

	// Aggregate counts
	var maxTasks int64
	var maxFailures int64
	var slowestDuration time.Duration

	for _, qInfo := range queues {
		if qInfo.Paused {
			stats.PausedQueues++
		} else {
			stats.ActiveQueues++
		}

		// Get queue stats
		qStats, err := s.GetQueueStats(ctx, qInfo.Name)
		if err != nil {
			continue
		}

		stats.PendingTasks += int64(qStats.PendingCount)
		stats.RunningTasks += int64(qStats.RunningCount)
		stats.ScheduledTasks += int64(qStats.ScheduledCount)
		stats.CompletedTasks += int64(qStats.CompletedCount)
		stats.FailedTasks += int64(qStats.FailedCount)

		// Find busiest queue
		total := int64(qStats.PendingCount + qStats.RunningCount)
		if total > maxTasks {
			maxTasks = total
			stats.BusiestQueue = qInfo.Name
		}
	}

	stats.TotalTasks = stats.PendingTasks + stats.RunningTasks + stats.ScheduledTasks +
		stats.CompletedTasks + stats.FailedTasks

	// Calculate overall success rate
	processed := stats.CompletedTasks + stats.FailedTasks
	if processed > 0 {
		stats.OverallSuccessRate = float64(stats.CompletedTasks) / float64(processed)
	}

	// Get overall performance metrics
	perf, err := s.getPerformanceMetrics(ctx, "", "")
	if err == nil {
		stats.AvgDuration = perf.AvgDuration
	}

	// Get throughput
	throughput, err := s.getThroughput(ctx, "", "")
	if err == nil {
		stats.TasksPerHour = throughput.PerHour
	}

	// Find slowest kind and failing kind
	kinds, err := s.getAllKinds(ctx)
	if err == nil {
		for _, kind := range kinds {
			kindStats, err := s.GetKindStats(ctx, kind)
			if err != nil {
				continue
			}

			if kindStats.AvgDuration > slowestDuration {
				slowestDuration = kindStats.AvgDuration
				stats.SlowestKind = kind
			}

			if kindStats.TotalFailed > maxFailures {
				maxFailures = kindStats.TotalFailed
				stats.FailingKind = kind
			}
		}
	}

	// Get oldest task
	oldest, err := s.getOldestTask(ctx)
	if err == nil && oldest != nil {
		stats.OldestTask = oldest
	}

	// Get stuck tasks count
	stuckCount, err := s.getStuckTasksCount(ctx)
	if err == nil {
		stats.StuckTasks = stuckCount
	}

	return stats, nil
}

// GetTimeSeries returns time-series data
func (s *DefaultStatistics) GetTimeSeries(ctx context.Context, opts TimeSeriesOptions) (*TimeSeries, error) {
	// Set defaults
	if opts.Interval == 0 {
		opts.Interval = 1 * time.Hour
	}
	if opts.Limit == 0 {
		opts.Limit = 100
	}
	if opts.EndTime.IsZero() {
		opts.EndTime = time.Now()
	}
	if opts.StartTime.IsZero() {
		opts.StartTime = opts.EndTime.Add(-24 * time.Hour)
	}

	ts := &TimeSeries{
		Queue:    opts.Queue,
		Kind:     opts.Kind,
		Interval: opts.Interval,
	}

	// Generate time buckets
	current := opts.StartTime
	for current.Before(opts.EndTime) && len(ts.DataPoints) < opts.Limit {
		next := current.Add(opts.Interval)

		dp, err := s.getDataPoint(ctx, opts.Queue, opts.Kind, current, next)
		if err != nil {
			return nil, fmt.Errorf("failed to get data point: %w", err)
		}

		ts.DataPoints = append(ts.DataPoints, dp)
		current = next
	}

	return ts, nil
}

// GetWorkerStats returns statistics per worker
// Note: Worker tracking requires WorkerID to be stored in InternalTask
func (s *DefaultStatistics) GetWorkerStats(ctx context.Context, workerID string) (*WorkerStatistics, error) {
	// Worker statistics not yet implemented - requires schema changes
	return &WorkerStatistics{
		WorkerID: workerID,
	}, nil
}

// GetErrorStats returns most common errors
func (s *DefaultStatistics) GetErrorStats(ctx context.Context, queue string, limit int) ([]*ErrorStats, error) {
	// Get all failed tasks
	filter := &TaskFilter{
		State: StateFailed,
	}
	if queue != "" {
		filter.Queue = queue
	}

	tasks, err := s.store.ListTasks(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list failed tasks: %w", err)
	}

	// Count errors
	errorCounts := make(map[string]*ErrorStats)
	totalErrors := 0

	for _, task := range tasks {
		if task.Error == "" {
			continue
		}

		if _, exists := errorCounts[task.Error]; !exists {
			errorCounts[task.Error] = &ErrorStats{
				Error: task.Error,
			}
		}

		errorCounts[task.Error].Count++
		if task.CompletedAt != nil && (errorCounts[task.Error].LastSeen.IsZero() ||
			task.CompletedAt.After(errorCounts[task.Error].LastSeen)) {
			errorCounts[task.Error].LastSeen = *task.CompletedAt
		}
		totalErrors++
	}

	// Convert to slice and sort
	var result []*ErrorStats
	for _, es := range errorCounts {
		es.Percentage = float64(es.Count) / float64(totalErrors) * 100
		result = append(result, es)
	}

	slices.SortFunc(result, func(a, b *ErrorStats) int {
		return cmp.Compare(b.Count, a.Count) // Descending order
	})

	// Limit results
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// Helper types and functions

type performanceMetrics struct {
	AvgDuration    time.Duration
	MedianDuration time.Duration
	P95Duration    time.Duration
	P99Duration    time.Duration
	AvgRetries     float64
	MaxRetries     int
	TotalRetries   int64
}

type throughputMetrics struct {
	PerHour    float64
	PerMinute  float64
	LastTaskAt *time.Time
}

type kindCounts struct {
	Completed int64
	Failed    int64
	Cancelled int64
}

type kindTiming struct {
	FirstSeen *time.Time
	LastSeen  *time.Time
}

// countByState counts tasks by state for a queue
func (s *DefaultStatistics) countByState(ctx context.Context, queue string) (map[TaskState]int, error) {
	counts := make(map[TaskState]int)

	states := []TaskState{
		StatePending, StateRunning, StateScheduled, StateCompleted,
		StateFailed, StateRetrying, StateCancelled, StateAggregating,
	}

	for _, state := range states {
		filter := &TaskFilter{
			State: state,
		}
		if queue != "" {
			filter.Queue = queue
		}

		tasks, err := s.store.ListTasks(ctx, filter)
		if err != nil {
			return nil, err
		}
		counts[state] = len(tasks)
	}

	return counts, nil
}

// getPerformanceMetrics calculates performance metrics
func (s *DefaultStatistics) getPerformanceMetrics(ctx context.Context, queue, kind string) (*performanceMetrics, error) {
	filter := &TaskFilter{
		State: StateCompleted,
	}
	if queue != "" {
		filter.Queue = queue
	}
	if kind != "" {
		filter.Kind = kind
	}

	tasks, err := s.store.ListTasks(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return &performanceMetrics{}, nil
	}

	var durations []time.Duration
	var totalRetries int64
	maxRetries := 0

	for _, task := range tasks {
		if task.StartedAt != nil && task.CompletedAt != nil {
			duration := task.CompletedAt.Sub(*task.StartedAt)
			durations = append(durations, duration)
		}

		totalRetries += int64(task.Retried)
		if task.Retried > maxRetries {
			maxRetries = task.Retried
		}
	}

	if len(durations) == 0 {
		return &performanceMetrics{
			TotalRetries: totalRetries,
			MaxRetries:   maxRetries,
		}, nil
	}

	// Sort for percentiles
	slices.SortFunc(durations, func(a, b time.Duration) int {
		return cmp.Compare(a, b)
	})

	// Calculate average
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	avg := total / time.Duration(len(durations))

	// Calculate median
	median := durations[len(durations)/2]

	// Calculate percentiles
	p95Idx := int(math.Ceil(float64(len(durations))*0.95)) - 1
	p99Idx := int(math.Ceil(float64(len(durations))*0.99)) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p99Idx < 0 {
		p99Idx = 0
	}

	return &performanceMetrics{
		AvgDuration:    avg,
		MedianDuration: median,
		P95Duration:    durations[p95Idx],
		P99Duration:    durations[p99Idx],
		AvgRetries:     float64(totalRetries) / float64(len(tasks)),
		MaxRetries:     maxRetries,
		TotalRetries:   totalRetries,
	}, nil
}

// getThroughput calculates throughput metrics
func (s *DefaultStatistics) getThroughput(ctx context.Context, queue, kind string) (*throughputMetrics, error) {
	filter := &TaskFilter{}
	if queue != "" {
		filter.Queue = queue
	}
	if kind != "" {
		filter.Kind = kind
	}

	tasks, err := s.store.ListTasks(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return &throughputMetrics{}, nil
	}

	// Find time range
	var earliest, latest time.Time
	for _, task := range tasks {
		if earliest.IsZero() || task.EnqueuedAt.Before(earliest) {
			earliest = task.EnqueuedAt
		}
		if latest.IsZero() || task.EnqueuedAt.After(latest) {
			latest = task.EnqueuedAt
		}
	}

	duration := latest.Sub(earliest)
	if duration == 0 {
		return &throughputMetrics{
			LastTaskAt: &latest,
		}, nil
	}

	hours := duration.Hours()
	minutes := duration.Minutes()

	return &throughputMetrics{
		PerHour:    float64(len(tasks)) / hours,
		PerMinute:  float64(len(tasks)) / minutes,
		LastTaskAt: &latest,
	}, nil
}

// getOldestPendingAge returns age of oldest pending task
func (s *DefaultStatistics) getOldestPendingAge(ctx context.Context, queue string) (time.Duration, error) {
	filter := &TaskFilter{
		Queue: queue,
		State: StatePending,
	}

	tasks, err := s.store.ListTasks(ctx, filter)
	if err != nil {
		return 0, err
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	oldest := tasks[0].EnqueuedAt
	for _, task := range tasks {
		if task.EnqueuedAt.Before(oldest) {
			oldest = task.EnqueuedAt
		}
	}

	return time.Since(oldest), nil
}

// getAvgWaitTime calculates average wait time
func (s *DefaultStatistics) getAvgWaitTime(ctx context.Context, queue string) (time.Duration, error) {
	filter := &TaskFilter{
		Queue: queue,
		State: StateCompleted,
	}

	tasks, err := s.store.ListTasks(ctx, filter)
	if err != nil {
		return 0, err
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	var total time.Duration
	count := 0

	for _, task := range tasks {
		if task.StartedAt != nil {
			wait := task.StartedAt.Sub(task.EnqueuedAt)
			total += wait
			count++
		}
	}

	if count == 0 {
		return 0, nil
	}

	return total / time.Duration(count), nil
}

// countByKind counts tasks by kind
func (s *DefaultStatistics) countByKind(ctx context.Context, kind string) (*kindCounts, error) {
	counts := &kindCounts{}

	// Count completed
	completed, err := s.store.ListTasks(ctx, &TaskFilter{
		Kind:  kind,
		State: StateCompleted,
	})
	if err != nil {
		return nil, err
	}
	counts.Completed = int64(len(completed))

	// Count failed
	failed, err := s.store.ListTasks(ctx, &TaskFilter{
		Kind:  kind,
		State: StateFailed,
	})
	if err != nil {
		return nil, err
	}
	counts.Failed = int64(len(failed))

	// Count cancelled
	cancelled, err := s.store.ListTasks(ctx, &TaskFilter{
		Kind:  kind,
		State: StateCancelled,
	})
	if err != nil {
		return nil, err
	}
	counts.Cancelled = int64(len(cancelled))

	return counts, nil
}

// getKindTiming gets first and last seen times for a kind
func (s *DefaultStatistics) getKindTiming(ctx context.Context, kind string) (*kindTiming, error) {
	tasks, err := s.store.ListTasks(ctx, &TaskFilter{Kind: kind})
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return &kindTiming{}, nil
	}

	var first, last time.Time
	for _, task := range tasks {
		if first.IsZero() || task.EnqueuedAt.Before(first) {
			first = task.EnqueuedAt
		}
		if last.IsZero() || task.EnqueuedAt.After(last) {
			last = task.EnqueuedAt
		}
	}

	return &kindTiming{
		FirstSeen: &first,
		LastSeen:  &last,
	}, nil
}

// getAllKinds returns all task kinds
func (s *DefaultStatistics) getAllKinds(ctx context.Context) ([]string, error) {
	tasks, err := s.store.ListTasks(ctx, &TaskFilter{})
	if err != nil {
		return nil, err
	}

	kinds := make(map[string]bool)
	for _, task := range tasks {
		kinds[task.Kind] = true
	}

	var result []string
	for kind := range kinds {
		result = append(result, kind)
	}

	return result, nil
}

// getOldestTask returns the oldest task timestamp
func (s *DefaultStatistics) getOldestTask(ctx context.Context) (*time.Time, error) {
	tasks, err := s.store.ListTasks(ctx, &TaskFilter{})
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return nil, nil
	}

	oldest := tasks[0].EnqueuedAt
	for _, task := range tasks {
		if task.EnqueuedAt.Before(oldest) {
			oldest = task.EnqueuedAt
		}
	}

	return &oldest, nil
}

// getStuckTasksCount counts stuck (running too long) tasks
func (s *DefaultStatistics) getStuckTasksCount(ctx context.Context) (int, error) {
	tasks, err := s.store.ListTasks(ctx, &TaskFilter{
		State: StateRunning,
	})
	if err != nil {
		return 0, err
	}

	count := 0
	stuckThreshold := 1 * time.Hour // Consider stuck after 1 hour

	for _, task := range tasks {
		if task.StartedAt != nil && time.Since(*task.StartedAt) > stuckThreshold {
			count++
		}
	}

	return count, nil
}

// getDataPoint generates a data point for a time range
func (s *DefaultStatistics) getDataPoint(ctx context.Context, queue, kind string, start, end time.Time) (*DataPoint, error) {
	dp := &DataPoint{
		Timestamp: start,
	}

	// Get all tasks in this time range
	tasks, err := s.store.ListTasks(ctx, &TaskFilter{
		Queue: queue,
		Kind:  kind,
	})
	if err != nil {
		return nil, err
	}

	var latencies []time.Duration

	for _, task := range tasks {
		// Check if task was enqueued in this range
		if task.EnqueuedAt.After(start) && task.EnqueuedAt.Before(end) {
			dp.Enqueued++
		}

		// Check if task completed in this range
		if task.CompletedAt != nil && task.CompletedAt.After(start) && task.CompletedAt.Before(end) {
			switch task.State {
			case StateCompleted:
				dp.Completed++
			case StateFailed:
				dp.Failed++
			}

			// Calculate latency
			if task.StartedAt != nil {
				latency := task.CompletedAt.Sub(*task.StartedAt)
				latencies = append(latencies, latency)
			}
		}
	}

	// Calculate average latency
	if len(latencies) > 0 {
		var total time.Duration
		for _, l := range latencies {
			total += l
		}
		dp.AvgLatency = total / time.Duration(len(latencies))

		// Calculate P95
		slices.SortFunc(latencies, func(a, b time.Duration) int {
			return cmp.Compare(a, b)
		})
		p95Idx := int(math.Ceil(float64(len(latencies))*0.95)) - 1
		if p95Idx >= 0 && p95Idx < len(latencies) {
			dp.P95Latency = latencies[p95Idx]
		}
	}

	return dp, nil
}
