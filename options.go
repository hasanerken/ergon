package ergon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// Option configures task enqueue behavior
type Option func(*taskConfig)

// taskConfig holds all configuration options for a task
type taskConfig struct {
	// Basic options
	queue      string
	priority   int
	maxRetries int
	timeout    time.Duration
	retention  time.Duration
	metadata   map[string]interface{}

	// Scheduling
	scheduledAt *time.Time
	processIn   time.Duration

	// Uniqueness
	unique         bool
	uniquePeriod   time.Duration
	uniqueByArgs   bool
	uniqueByQueue  bool
	uniqueByStates []TaskState
	uniqueKeyFunc  func(kind string, payload []byte, queue string) string

	// Rate limiting
	rateLimitScope string
	maxConcurrent  int
	tokenRate      float64
	tokenBurst     int
	minInterval    time.Duration

	// Concurrency partitioning
	globalConcurrency int
	localConcurrency  int
	partitionByArgs   []string
	partitionKeyFunc  func(payload []byte) string

	// Aggregation/Grouping
	groupKey         string
	groupMaxSize     int
	groupMaxDelay    time.Duration
	groupGracePeriod time.Duration

	// Recurring
	recurring    bool
	cronSchedule string
	interval     time.Duration
}

// newTaskConfig returns a config with default values
func newTaskConfig() *taskConfig {
	return &taskConfig{
		queue:      "default",
		priority:   0,
		maxRetries: 25,
		timeout:    30 * time.Minute,
	}
}

// ============================================================================
// BASIC OPTIONS
// ============================================================================

// WithQueue sets the queue name
func WithQueue(queue string) Option {
	return func(c *taskConfig) {
		c.queue = queue
	}
}

// WithPriority sets task priority (higher = more important)
func WithPriority(priority int) Option {
	return func(c *taskConfig) {
		c.priority = priority
	}
}

// WithMaxRetries sets maximum retry attempts
func WithMaxRetries(maxRetries int) Option {
	return func(c *taskConfig) {
		c.maxRetries = maxRetries
	}
}

// WithTimeout sets task execution timeout
func WithTimeout(timeout time.Duration) Option {
	return func(c *taskConfig) {
		c.timeout = timeout
	}
}

// WithRetention keeps completed task for duration
func WithRetention(retention time.Duration) Option {
	return func(c *taskConfig) {
		c.retention = retention
	}
}

// WithMetadata adds custom metadata
func WithMetadata(key string, value interface{}) Option {
	return func(c *taskConfig) {
		if c.metadata == nil {
			c.metadata = make(map[string]interface{})
		}
		c.metadata[key] = value
	}
}

// WithMetadataMap sets multiple metadata values
func WithMetadataMap(metadata map[string]interface{}) Option {
	return func(c *taskConfig) {
		if c.metadata == nil {
			c.metadata = make(map[string]interface{})
		}
		for k, v := range metadata {
			c.metadata[k] = v
		}
	}
}

// ============================================================================
// SCHEDULING OPTIONS
// ============================================================================

// WithScheduledAt schedules task for specific time
func WithScheduledAt(t time.Time) Option {
	return func(c *taskConfig) {
		c.scheduledAt = &t
	}
}

// WithProcessIn schedules task after duration
func WithProcessIn(d time.Duration) Option {
	return func(c *taskConfig) {
		t := time.Now().Add(d)
		c.scheduledAt = &t
	}
}

// WithDelay is an alias for WithProcessIn
func WithDelay(d time.Duration) Option {
	return WithProcessIn(d)
}

// ============================================================================
// UNIQUENESS OPTIONS
// ============================================================================

// WithUnique enables uniqueness with TTL
// Prevents duplicate tasks based on kind + payload + queue
func WithUnique(period time.Duration) Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniquePeriod = period
		c.uniqueByArgs = true
		c.uniqueByQueue = true
	}
}

// WithUniqueByArgs makes uniqueness consider task arguments
func WithUniqueByArgs() Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniqueByArgs = true
	}
}

// WithUniqueByQueue makes uniqueness consider queue name
func WithUniqueByQueue() Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniqueByQueue = true
	}
}

// WithUniquePeriod sets time window for uniqueness (like River's ByPeriod)
// Example: WithUniquePeriod(1*time.Hour) = "at most once per hour"
func WithUniquePeriod(period time.Duration) Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniquePeriod = period
	}
}

// WithUniqueStates sets which states to check for uniqueness
func WithUniqueStates(states ...TaskState) Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniqueByStates = states
	}
}

// WithUniqueKey provides custom uniqueness key function
func WithUniqueKey(keyFunc func(kind string, payload []byte, queue string) string) Option {
	return func(c *taskConfig) {
		c.unique = true
		c.uniqueKeyFunc = keyFunc
	}
}

// ============================================================================
// RATE LIMITING OPTIONS
// ============================================================================

// WithRateLimit sets max concurrent executions (distributed)
func WithRateLimit(maxConcurrent int) Option {
	return func(c *taskConfig) {
		c.maxConcurrent = maxConcurrent
		c.rateLimitScope = "global"
	}
}

// WithRateLimitScope sets the scope for rate limiting
func WithRateLimitScope(scope string) Option {
	return func(c *taskConfig) {
		c.rateLimitScope = scope
	}
}

// WithTokenBucket enables token bucket rate limiting (X requests per second)
func WithTokenBucket(rate float64, burst int) Option {
	return func(c *taskConfig) {
		c.tokenRate = rate
		c.tokenBurst = burst
	}
}

// WithMinInterval sets minimum delay between task executions
func WithMinInterval(interval time.Duration) Option {
	return func(c *taskConfig) {
		c.minInterval = interval
	}
}

// ============================================================================
// CONCURRENCY PARTITIONING OPTIONS
// ============================================================================

// WithGlobalConcurrency limits concurrent executions globally
func WithGlobalConcurrency(limit int) Option {
	return func(c *taskConfig) {
		c.globalConcurrency = limit
	}
}

// WithLocalConcurrency limits concurrent executions per client
func WithLocalConcurrency(limit int) Option {
	return func(c *taskConfig) {
		c.localConcurrency = limit
	}
}

// WithPartitionByArgs partitions concurrency by argument fields
func WithPartitionByArgs(fields ...string) Option {
	return func(c *taskConfig) {
		c.partitionByArgs = fields
	}
}

// WithPartitionKey provides custom partition key function
func WithPartitionKey(keyFunc func(payload []byte) string) Option {
	return func(c *taskConfig) {
		c.partitionKeyFunc = keyFunc
	}
}

// ============================================================================
// AGGREGATION/GROUPING OPTIONS
// ============================================================================

// WithGroup enables task aggregation/batching
func WithGroup(groupKey string) Option {
	return func(c *taskConfig) {
		c.groupKey = groupKey
	}
}

// WithGroupMaxSize sets max tasks before triggering aggregation
func WithGroupMaxSize(maxSize int) Option {
	return func(c *taskConfig) {
		c.groupMaxSize = maxSize
	}
}

// WithGroupMaxDelay sets max wait time before aggregation
func WithGroupMaxDelay(maxDelay time.Duration) Option {
	return func(c *taskConfig) {
		c.groupMaxDelay = maxDelay
	}
}

// WithGroupGracePeriod sets additional wait time for more tasks
func WithGroupGracePeriod(gracePeriod time.Duration) Option {
	return func(c *taskConfig) {
		c.groupGracePeriod = gracePeriod
	}
}

// ============================================================================
// COMPOSITE OPTIONS (Convenience Helpers)
// ============================================================================

// WithHighPriority is a preset for high priority tasks
func WithHighPriority() Option {
	return func(c *taskConfig) {
		c.queue = "critical"
		c.priority = 10
		c.timeout = 5 * time.Minute
	}
}

// WithLowPriority is a preset for low priority tasks
func WithLowPriority() Option {
	return func(c *taskConfig) {
		c.queue = "low"
		c.priority = -10
		c.timeout = 1 * time.Hour
	}
}

// WithRetryOnce disables retries (single attempt only)
func WithRetryOnce() Option {
	return WithMaxRetries(1)
}

// WithNoRetry completely disables retries
func WithNoRetry() Option {
	return WithMaxRetries(0)
}

// AtMostOncePerHour ensures task runs max once per hour
func AtMostOncePerHour() Option {
	return WithUnique(time.Hour)
}

// AtMostOncePerDay ensures task runs max once per day
func AtMostOncePerDay() Option {
	return WithUnique(24 * time.Hour)
}

// MaxConcurrentPerCustomer limits concurrent tasks per customer
func MaxConcurrentPerCustomer(limit int) Option {
	return func(c *taskConfig) {
		c.globalConcurrency = limit
		c.partitionByArgs = []string{"customer_id"}
	}
}

// BatchNotifications groups notification tasks
func BatchNotifications(userID string) Option {
	return func(c *taskConfig) {
		c.groupKey = userID
		c.groupMaxSize = 50
		c.groupMaxDelay = 5 * time.Minute
		c.groupGracePeriod = 1 * time.Minute
	}
}

// ============================================================================
// RECURRING TASK OPTIONS
// ============================================================================

// WithCronSchedule sets a cron schedule for recurring tasks
// Example: "0 */6 * * *" runs every 6 hours
func WithCronSchedule(cronExpr string) Option {
	return func(c *taskConfig) {
		c.recurring = true
		c.cronSchedule = cronExpr
	}
}

// WithInterval sets a recurring interval
// Example: Every 5 minutes
func WithInterval(interval time.Duration) Option {
	return func(c *taskConfig) {
		c.recurring = true
		c.interval = interval
	}
}

// EveryMinute runs task every minute
func EveryMinute() Option {
	return WithInterval(1 * time.Minute)
}

// EveryHour runs task every hour
func EveryHour() Option {
	return WithInterval(1 * time.Hour)
}

// EveryDay runs task every 24 hours
func EveryDay() Option {
	return WithInterval(24 * time.Hour)
}

// EveryDayAt runs task daily at specific hour:minute (UTC)
func EveryDayAt(hour, minute int) Option {
	return WithCronSchedule(fmt.Sprintf("%d %d * * *", minute, hour))
}

// EveryWeek runs task weekly
func EveryWeek() Option {
	return WithInterval(7 * 24 * time.Hour)
}

// Every runs task at custom interval
func Every(interval time.Duration) Option {
	return WithInterval(interval)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// computeUniqueKey generates a unique key for task deduplication
func computeUniqueKey(kind string, payload []byte, queue string, cfg *taskConfig) string {
	if cfg.uniqueKeyFunc != nil {
		return cfg.uniqueKeyFunc(kind, payload, queue)
	}

	// Default: kind + payload hash + optional queue
	key := kind
	if cfg.uniqueByArgs {
		key += ":" + hashPayload(payload)
	}
	if cfg.uniqueByQueue {
		key += ":" + queue
	}
	if cfg.uniquePeriod > 0 {
		// Round to period bucket (River-style)
		bucket := time.Now().Truncate(cfg.uniquePeriod).Unix()
		key += ":" + strconv.FormatInt(bucket, 10)
	}
	return key
}

// hashPayload creates a SHA256 hash of the payload
func hashPayload(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

// computePartitionKey generates a partition key for concurrency limiting
func computePartitionKey(payload []byte, cfg *taskConfig) string {
	if cfg.partitionKeyFunc != nil {
		return cfg.partitionKeyFunc(payload)
	}

	// Default: extract fields from payload (simplified)
	// In a real implementation, this would parse JSON and extract specific fields
	return fmt.Sprintf("partition:%s", hashPayload(payload)[:8])
}
