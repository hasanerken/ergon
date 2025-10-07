package ratelimit

import (
	"sync"
	"time"
)

// RateLimiter implements a token bucket algorithm for rate limiting
// Supports per-scope concurrent execution limits
type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
}

// bucket represents a token bucket for a specific scope
type bucket struct {
	limit     int           // Maximum concurrent executions
	current   int           // Current number of executing tasks
	lastCheck time.Time     // Last time bucket was checked
	mu        sync.Mutex    // Protects this bucket
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
	}
}

// Allow checks if a task can execute under the rate limit for the given scope
// Returns true if allowed, false if rate limited
// Also returns retry delay if rate limited
func (rl *RateLimiter) Allow(scope string, limit int) (bool, time.Duration) {
	if scope == "" || limit <= 0 {
		// No rate limiting configured
		return true, 0
	}

	b := rl.getOrCreateBucket(scope, limit)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Update limit if changed
	if b.limit != limit {
		b.limit = limit
	}

	// Check if we can allow this execution
	if b.current < b.limit {
		b.current++
		b.lastCheck = time.Now()
		return true, 0
	}

	// Rate limited - suggest retry after 1 second
	return false, time.Second
}

// Release decrements the counter for a scope when a task completes
func (rl *RateLimiter) Release(scope string) {
	if scope == "" {
		return
	}

	rl.mu.RLock()
	b, exists := rl.buckets[scope]
	rl.mu.RUnlock()

	if !exists {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.current > 0 {
		b.current--
	}
}

// GetCurrent returns the current number of executing tasks for a scope
func (rl *RateLimiter) GetCurrent(scope string) int {
	rl.mu.RLock()
	b, exists := rl.buckets[scope]
	rl.mu.RUnlock()

	if !exists {
		return 0
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.current
}

// Reset resets the bucket for a scope
func (rl *RateLimiter) Reset(scope string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.buckets, scope)
}

// ResetAll clears all buckets
func (rl *RateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.buckets = make(map[string]*bucket)
}

// getOrCreateBucket gets an existing bucket or creates a new one
func (rl *RateLimiter) getOrCreateBucket(scope string, limit int) *bucket {
	rl.mu.RLock()
	b, exists := rl.buckets[scope]
	rl.mu.RUnlock()

	if exists {
		return b
	}

	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if b, exists := rl.buckets[scope]; exists {
		return b
	}

	b = &bucket{
		limit:     limit,
		current:   0,
		lastCheck: time.Now(),
	}
	rl.buckets[scope] = b
	return b
}

// Stats returns statistics for a scope
type Stats struct {
	Scope       string
	Limit       int
	Current     int
	Available   int
	Utilization float64 // 0.0 to 1.0
}

// GetStats returns statistics for a scope
func (rl *RateLimiter) GetStats(scope string) *Stats {
	rl.mu.RLock()
	b, exists := rl.buckets[scope]
	rl.mu.RUnlock()

	if !exists {
		return &Stats{
			Scope:       scope,
			Limit:       0,
			Current:     0,
			Available:   0,
			Utilization: 0,
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return &Stats{
		Scope:       scope,
		Limit:       b.limit,
		Current:     b.current,
		Available:   b.limit - b.current,
		Utilization: float64(b.current) / float64(b.limit),
	}
}

// GetAllStats returns statistics for all scopes
func (rl *RateLimiter) GetAllStats() []*Stats {
	rl.mu.RLock()
	scopes := make([]string, 0, len(rl.buckets))
	for scope := range rl.buckets {
		scopes = append(scopes, scope)
	}
	rl.mu.RUnlock()

	stats := make([]*Stats, 0, len(scopes))
	for _, scope := range scopes {
		stats = append(stats, rl.GetStats(scope))
	}
	return stats
}
