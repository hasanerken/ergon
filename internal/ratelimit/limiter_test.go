package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("allows up to limit", func(t *testing.T) {
		scope := "test-scope-1"
		limit := 3

		// Should allow first 3
		for i := 0; i < limit; i++ {
			allowed, _ := rl.Allow(scope, limit)
			if !allowed {
				t.Errorf("Expected to allow request %d, but was rate limited", i+1)
			}
		}

		// Should deny 4th
		allowed, retryAfter := rl.Allow(scope, limit)
		if allowed {
			t.Error("Expected to rate limit 4th request")
		}
		if retryAfter != time.Second {
			t.Errorf("Expected retry after %v, got %v", time.Second, retryAfter)
		}
	})

	t.Run("different scopes are independent", func(t *testing.T) {
		scope1 := "scope-a"
		scope2 := "scope-b"
		limit := 2

		// Fill up scope1
		rl.Allow(scope1, limit)
		rl.Allow(scope1, limit)

		// scope1 should be rate limited
		allowed, _ := rl.Allow(scope1, limit)
		if allowed {
			t.Error("Expected scope1 to be rate limited")
		}

		// scope2 should still be available
		allowed, _ = rl.Allow(scope2, limit)
		if !allowed {
			t.Error("Expected scope2 to be available")
		}
	})

	t.Run("empty scope always allows", func(t *testing.T) {
		allowed, retryAfter := rl.Allow("", 5)
		if !allowed {
			t.Error("Expected empty scope to always allow")
		}
		if retryAfter != 0 {
			t.Errorf("Expected zero retry delay, got %v", retryAfter)
		}
	})

	t.Run("zero or negative limit always allows", func(t *testing.T) {
		allowed, retryAfter := rl.Allow("test-scope", 0)
		if !allowed {
			t.Error("Expected zero limit to always allow")
		}
		if retryAfter != 0 {
			t.Errorf("Expected zero retry delay, got %v", retryAfter)
		}

		allowed, retryAfter = rl.Allow("test-scope", -1)
		if !allowed {
			t.Error("Expected negative limit to always allow")
		}
		if retryAfter != 0 {
			t.Errorf("Expected zero retry delay, got %v", retryAfter)
		}
	})
}

func TestRateLimiter_Release(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("release allows new requests", func(t *testing.T) {
		scope := "test-release"
		limit := 2

		// Fill up
		rl.Allow(scope, limit)
		rl.Allow(scope, limit)

		// Should be rate limited
		allowed, _ := rl.Allow(scope, limit)
		if allowed {
			t.Error("Expected to be rate limited before release")
		}

		// Release one slot
		rl.Release(scope)

		// Should now allow one more
		allowed, _ = rl.Allow(scope, limit)
		if !allowed {
			t.Error("Expected to allow after release")
		}
	})

	t.Run("release on empty scope does nothing", func(t *testing.T) {
		// Should not panic
		rl.Release("")
	})

	t.Run("release on non-existent scope does nothing", func(t *testing.T) {
		// Should not panic
		rl.Release("non-existent-scope")
	})
}

func TestRateLimiter_GetCurrent(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("tracks current usage", func(t *testing.T) {
		scope := "test-current"
		limit := 3

		if current := rl.GetCurrent(scope); current != 0 {
			t.Errorf("Expected 0 current, got %d", current)
		}

		rl.Allow(scope, limit)
		if current := rl.GetCurrent(scope); current != 1 {
			t.Errorf("Expected 1 current, got %d", current)
		}

		rl.Allow(scope, limit)
		if current := rl.GetCurrent(scope); current != 2 {
			t.Errorf("Expected 2 current, got %d", current)
		}

		rl.Release(scope)
		if current := rl.GetCurrent(scope); current != 1 {
			t.Errorf("Expected 1 current after release, got %d", current)
		}
	})
}

func TestRateLimiter_Reset(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("reset clears scope", func(t *testing.T) {
		scope := "test-reset"
		limit := 2

		// Fill up
		rl.Allow(scope, limit)
		rl.Allow(scope, limit)

		// Should be rate limited
		allowed, _ := rl.Allow(scope, limit)
		if allowed {
			t.Error("Expected to be rate limited before reset")
		}

		// Reset
		rl.Reset(scope)

		// Should allow again
		allowed, _ = rl.Allow(scope, limit)
		if !allowed {
			t.Error("Expected to allow after reset")
		}
	})
}

func TestRateLimiter_ResetAll(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("reset all clears all scopes", func(t *testing.T) {
		scope1 := "scope-1"
		scope2 := "scope-2"
		limit := 1

		// Fill up both
		rl.Allow(scope1, limit)
		rl.Allow(scope2, limit)

		// Both should be rate limited
		if allowed, _ := rl.Allow(scope1, limit); allowed {
			t.Error("Expected scope1 to be rate limited")
		}
		if allowed, _ := rl.Allow(scope2, limit); allowed {
			t.Error("Expected scope2 to be rate limited")
		}

		// Reset all
		rl.ResetAll()

		// Both should allow again
		if allowed, _ := rl.Allow(scope1, limit); !allowed {
			t.Error("Expected scope1 to allow after reset all")
		}
		if allowed, _ := rl.Allow(scope2, limit); !allowed {
			t.Error("Expected scope2 to allow after reset all")
		}
	})
}

func TestRateLimiter_GetStats(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("returns correct stats", func(t *testing.T) {
		scope := "test-stats"
		limit := 5

		// Allow 3 out of 5
		rl.Allow(scope, limit)
		rl.Allow(scope, limit)
		rl.Allow(scope, limit)

		stats := rl.GetStats(scope)

		if stats.Scope != scope {
			t.Errorf("Expected scope %s, got %s", scope, stats.Scope)
		}
		if stats.Limit != limit {
			t.Errorf("Expected limit %d, got %d", limit, stats.Limit)
		}
		if stats.Current != 3 {
			t.Errorf("Expected current 3, got %d", stats.Current)
		}
		if stats.Available != 2 {
			t.Errorf("Expected available 2, got %d", stats.Available)
		}
		if stats.Utilization != 0.6 {
			t.Errorf("Expected utilization 0.6, got %f", stats.Utilization)
		}
	})

	t.Run("returns empty stats for non-existent scope", func(t *testing.T) {
		stats := rl.GetStats("non-existent")

		if stats.Scope != "non-existent" {
			t.Errorf("Expected scope 'non-existent', got %s", stats.Scope)
		}
		if stats.Limit != 0 {
			t.Errorf("Expected limit 0, got %d", stats.Limit)
		}
		if stats.Current != 0 {
			t.Errorf("Expected current 0, got %d", stats.Current)
		}
	})
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("handles concurrent requests safely", func(t *testing.T) {
		scope := "concurrent-test"
		limit := 10
		goroutines := 100

		var wg sync.WaitGroup
		allowed := make([]bool, goroutines)

		// Launch many goroutines trying to acquire
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				ok, _ := rl.Allow(scope, limit)
				allowed[index] = ok
			}(i)
		}

		wg.Wait()

		// Count how many were allowed
		count := 0
		for _, ok := range allowed {
			if ok {
				count++
			}
		}

		// Exactly 'limit' should be allowed
		if count != limit {
			t.Errorf("Expected exactly %d allowed, got %d", limit, count)
		}
	})

	t.Run("handles concurrent release safely", func(t *testing.T) {
		scope := "concurrent-release-test"
		limit := 5

		// Fill up
		for i := 0; i < limit; i++ {
			rl.Allow(scope, limit)
		}

		var wg sync.WaitGroup
		// Release concurrently
		for i := 0; i < limit; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rl.Release(scope)
			}()
		}

		wg.Wait()

		// Should be back to 0
		if current := rl.GetCurrent(scope); current != 0 {
			t.Errorf("Expected 0 current after concurrent releases, got %d", current)
		}
	})
}

func TestRateLimiter_DynamicLimitChange(t *testing.T) {
	rl := NewRateLimiter()

	t.Run("handles limit changes", func(t *testing.T) {
		scope := "dynamic-limit"

		// Start with limit of 3
		rl.Allow(scope, 3)
		rl.Allow(scope, 3)
		rl.Allow(scope, 3)

		// Should be rate limited with limit=3
		allowed, _ := rl.Allow(scope, 3)
		if allowed {
			t.Error("Expected to be rate limited with limit=3")
		}

		// Increase limit to 5
		allowed, _ = rl.Allow(scope, 5)
		if !allowed {
			t.Error("Expected to allow with increased limit=5")
		}
	})
}
