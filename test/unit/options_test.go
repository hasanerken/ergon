package unit_test

import (
	"testing"
	"time"
)

func TestOptions_Basic(t *testing.T) {
	cfg := newTaskConfig()

	t.Run("ergon.WithQueue", func(t *testing.T) {
		opt := ergon.WithQueue("custom")
		opt(cfg)

		if cfg.queue != "custom" {
			t.Errorf("expected queue 'custom', got %s", cfg.queue)
		}
	})

	t.Run("ergon.WithPriority", func(t *testing.T) {
		opt := ergon.WithPriority(10)
		opt(cfg)

		if cfg.priority != 10 {
			t.Errorf("expected priority 10, got %d", cfg.priority)
		}
	})

	t.Run("ergon.WithMaxRetries", func(t *testing.T) {
		opt := ergon.WithMaxRetries(5)
		opt(cfg)

		if cfg.maxRetries != 5 {
			t.Errorf("expected max retries 5, got %d", cfg.maxRetries)
		}
	})

	t.Run("ergon.WithTimeout", func(t *testing.T) {
		opt := ergon.WithTimeout(1 * time.Minute)
		opt(cfg)

		if cfg.timeout != 1*time.Minute {
			t.Errorf("expected timeout 1m, got %v", cfg.timeout)
		}
	})

	t.Run("WithRetention", func(t *testing.T) {
		opt := WithRetention(24 * time.Hour)
		opt(cfg)

		if cfg.retention != 24*time.Hour {
			t.Errorf("expected retention 24h, got %v", cfg.retention)
		}
	})
}

func TestOptions_Metadata(t *testing.T) {
	cfg := newTaskConfig()

	t.Run("WithMetadata single", func(t *testing.T) {
		opt := WithMetadata("key", "value")
		opt(cfg)

		if cfg.metadata["key"] != "value" {
			t.Error("metadata not set correctly")
		}
	})

	t.Run("WithMetadataMap", func(t *testing.T) {
		metadata := map[string]interface{}{
			"user_id":    "123",
			"request_id": "abc",
		}

		opt := WithMetadataMap(metadata)
		opt(cfg)

		if cfg.metadata["user_id"] != "123" {
			t.Error("user_id not set")
		}
		if cfg.metadata["request_id"] != "abc" {
			t.Error("request_id not set")
		}
	})

	t.Run("multiple metadata calls", func(t *testing.T) {
		cfg := newTaskConfig()

		WithMetadata("key1", "value1")(cfg)
		WithMetadata("key2", "value2")(cfg)

		if len(cfg.metadata) != 2 {
			t.Errorf("expected 2 metadata entries, got %d", len(cfg.metadata))
		}
	})
}

func TestOptions_Scheduling(t *testing.T) {
	cfg := newTaskConfig()

	t.Run("ergon.WithScheduledAt", func(t *testing.T) {
		scheduledTime := time.Now().Add(1 * time.Hour)
		opt := ergon.WithScheduledAt(scheduledTime)
		opt(cfg)

		if cfg.scheduledAt == nil {
			t.Fatal("scheduled time not set")
		}
		if !cfg.scheduledAt.Equal(scheduledTime) {
			t.Error("scheduled time mismatch")
		}
	})

	t.Run("WithProcessIn", func(t *testing.T) {
		before := time.Now()
		opt := WithProcessIn(5 * time.Minute)
		opt(cfg)
		after := time.Now()

		if cfg.scheduledAt == nil {
			t.Fatal("scheduled time not set")
		}

		expected := before.Add(5 * time.Minute)
		if cfg.scheduledAt.Before(expected) || cfg.scheduledAt.After(after.Add(5*time.Minute)) {
			t.Error("scheduled time not in expected range")
		}
	})

	t.Run("ergon.WithDelay", func(t *testing.T) {
		before := time.Now()
		opt := ergon.WithDelay(10 * time.Second)
		opt(cfg)
		after := time.Now()

		if cfg.scheduledAt == nil {
			t.Fatal("scheduled time not set")
		}

		expected := before.Add(10 * time.Second)
		if cfg.scheduledAt.Before(expected) || cfg.scheduledAt.After(after.Add(10*time.Second)) {
			t.Error("scheduled time not in expected range")
		}
	})
}

func TestOptions_Uniqueness(t *testing.T) {
	t.Run("ergon.WithUnique", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := ergon.WithUnique(1 * time.Hour)
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if cfg.uniquePeriod != 1*time.Hour {
			t.Errorf("expected unique period 1h, got %v", cfg.uniquePeriod)
		}
		if !cfg.uniqueByArgs {
			t.Error("uniqueByArgs should be true")
		}
		if !cfg.uniqueByQueue {
			t.Error("uniqueByQueue should be true")
		}
	})

	t.Run("WithUniqueByArgs", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithUniqueByArgs()
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if !cfg.uniqueByArgs {
			t.Error("uniqueByArgs not set")
		}
	})

	t.Run("WithUniqueByQueue", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithUniqueByQueue()
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if !cfg.uniqueByQueue {
			t.Error("uniqueByQueue not set")
		}
	})

	t.Run("WithUniquePeriod", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithUniquePeriod(30 * time.Minute)
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if cfg.uniquePeriod != 30*time.Minute {
			t.Errorf("expected period 30m, got %v", cfg.uniquePeriod)
		}
	})

	t.Run("WithUniqueStates", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithUniqueStates(ergon.StatePending, ergon.StateRunning)
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if len(cfg.uniqueByStates) != 2 {
			t.Errorf("expected 2 states, got %d", len(cfg.uniqueByStates))
		}
	})

	t.Run("WithUniqueKey", func(t *testing.T) {
		cfg := newTaskConfig()
		keyFunc := func(kind string, payload []byte, queue string) string {
			return "custom-key"
		}

		opt := WithUniqueKey(keyFunc)
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if cfg.uniqueKeyFunc == nil {
			t.Error("uniqueKeyFunc not set")
		}
	})
}

func TestOptions_Aggregation(t *testing.T) {
	t.Run("ergon.WithGroup", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := ergon.WithGroup("user:123")
		opt(cfg)

		if cfg.groupKey != "user:123" {
			t.Errorf("expected group key 'user:123', got %s", cfg.groupKey)
		}
	})

	t.Run("WithGroupMaxSize", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithGroupMaxSize(100)
		opt(cfg)

		if cfg.groupMaxSize != 100 {
			t.Errorf("expected max size 100, got %d", cfg.groupMaxSize)
		}
	})

	t.Run("WithGroupMaxDelay", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithGroupMaxDelay(5 * time.Minute)
		opt(cfg)

		if cfg.groupMaxDelay != 5*time.Minute {
			t.Errorf("expected max delay 5m, got %v", cfg.groupMaxDelay)
		}
	})

	t.Run("WithGroupGracePeriod", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithGroupGracePeriod(30 * time.Second)
		opt(cfg)

		if cfg.groupGracePeriod != 30*time.Second {
			t.Errorf("expected grace period 30s, got %v", cfg.groupGracePeriod)
		}
	})
}

func TestOptions_Recurring(t *testing.T) {
	t.Run("WithCronSchedule", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithCronSchedule("0 */6 * * *")
		opt(cfg)

		if !cfg.recurring {
			t.Error("recurring not enabled")
		}
		if cfg.cronSchedule != "0 */6 * * *" {
			t.Errorf("expected cron '0 */6 * * *', got %s", cfg.cronSchedule)
		}
	})

	t.Run("WithInterval", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithInterval(15 * time.Minute)
		opt(cfg)

		if !cfg.recurring {
			t.Error("recurring not enabled")
		}
		if cfg.interval != 15*time.Minute {
			t.Errorf("expected interval 15m, got %v", cfg.interval)
		}
	})

	t.Run("ergon.EveryMinute", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := ergon.EveryMinute()
		opt(cfg)

		if !cfg.recurring {
			t.Error("recurring not enabled")
		}
		if cfg.interval != 1*time.Minute {
			t.Errorf("expected interval 1m, got %v", cfg.interval)
		}
	})

	t.Run("ergon.EveryHour", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := ergon.EveryHour()
		opt(cfg)

		if cfg.interval != 1*time.Hour {
			t.Errorf("expected interval 1h, got %v", cfg.interval)
		}
	})

	t.Run("ergon.EveryDay", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := ergon.EveryDay()
		opt(cfg)

		if cfg.interval != 24*time.Hour {
			t.Errorf("expected interval 24h, got %v", cfg.interval)
		}
	})

	t.Run("Every custom", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := Every(45 * time.Minute)
		opt(cfg)

		if cfg.interval != 45*time.Minute {
			t.Errorf("expected interval 45m, got %v", cfg.interval)
		}
	})
}

func TestOptions_Composite(t *testing.T) {
	t.Run("WithHighPriority", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithHighPriority()
		opt(cfg)

		if cfg.queue != "critical" {
			t.Errorf("expected queue 'critical', got %s", cfg.queue)
		}
		if cfg.priority != 10 {
			t.Errorf("expected priority 10, got %d", cfg.priority)
		}
		if cfg.timeout != 5*time.Minute {
			t.Errorf("expected timeout 5m, got %v", cfg.timeout)
		}
	})

	t.Run("WithLowPriority", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithLowPriority()
		opt(cfg)

		if cfg.queue != "low" {
			t.Errorf("expected queue 'low', got %s", cfg.queue)
		}
		if cfg.priority != -10 {
			t.Errorf("expected priority -10, got %d", cfg.priority)
		}
		if cfg.timeout != 1*time.Hour {
			t.Errorf("expected timeout 1h, got %v", cfg.timeout)
		}
	})

	t.Run("WithRetryOnce", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithRetryOnce()
		opt(cfg)

		if cfg.maxRetries != 1 {
			t.Errorf("expected max retries 1, got %d", cfg.maxRetries)
		}
	})

	t.Run("WithNoRetry", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := WithNoRetry()
		opt(cfg)

		if cfg.maxRetries != 0 {
			t.Errorf("expected max retries 0, got %d", cfg.maxRetries)
		}
	})

	t.Run("AtMostOncePerHour", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := AtMostOncePerHour()
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if cfg.uniquePeriod != 1*time.Hour {
			t.Errorf("expected period 1h, got %v", cfg.uniquePeriod)
		}
	})

	t.Run("AtMostOncePerDay", func(t *testing.T) {
		cfg := newTaskConfig()
		opt := AtMostOncePerDay()
		opt(cfg)

		if !cfg.unique {
			t.Error("unique not enabled")
		}
		if cfg.uniquePeriod != 24*time.Hour {
			t.Errorf("expected period 24h, got %v", cfg.uniquePeriod)
		}
	})
}

func TestOptions_Chaining(t *testing.T) {
	cfg := newTaskConfig()

	// Apply multiple options
	opts := []Option{
		ergon.WithQueue("custom"),
		ergon.WithPriority(5),
		ergon.WithMaxRetries(3),
		ergon.WithTimeout(2 * time.Minute),
		WithMetadata("key", "value"),
		ergon.WithUnique(1 * time.Hour),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Verify all options applied
	if cfg.queue != "custom" {
		t.Error("queue not set")
	}
	if cfg.priority != 5 {
		t.Error("priority not set")
	}
	if cfg.maxRetries != 3 {
		t.Error("max retries not set")
	}
	if cfg.timeout != 2*time.Minute {
		t.Error("timeout not set")
	}
	if cfg.metadata["key"] != "value" {
		t.Error("metadata not set")
	}
	if !cfg.unique {
		t.Error("unique not set")
	}
}

func TestOptions_DefaultValues(t *testing.T) {
	cfg := newTaskConfig()

	if cfg.queue != "default" {
		t.Errorf("expected default queue 'default', got %s", cfg.queue)
	}
	if cfg.priority != 0 {
		t.Errorf("expected default priority 0, got %d", cfg.priority)
	}
	if cfg.maxRetries != 25 {
		t.Errorf("expected default max retries 25, got %d", cfg.maxRetries)
	}
	if cfg.timeout != 30*time.Minute {
		t.Errorf("expected default timeout 30m, got %v", cfg.timeout)
	}
}

func TestComputeUniqueKey(t *testing.T) {
	payload := []byte(`{"message":"test"}`)
	kind := "test_task"
	queue := "default"

	t.Run("default unique key", func(t *testing.T) {
		cfg := &taskConfig{
			uniqueByArgs:  true,
			uniqueByQueue: true,
			uniquePeriod:  1 * time.Hour,
		}

		key := computeUniqueKey(kind, payload, queue, cfg)
		if key == "" {
			t.Error("unique key is empty")
		}

		// Same inputs should produce same key
		key2 := computeUniqueKey(kind, payload, queue, cfg)
		if key != key2 {
			t.Error("same inputs should produce same key")
		}
	})

	t.Run("custom unique key function", func(t *testing.T) {
		cfg := &taskConfig{
			uniqueKeyFunc: func(k string, p []byte, q string) string {
				return "custom-key"
			},
		}

		key := computeUniqueKey(kind, payload, queue, cfg)
		if key != "custom-key" {
			t.Errorf("expected 'custom-key', got %s", key)
		}
	})

	t.Run("different payloads produce different keys", func(t *testing.T) {
		cfg := &taskConfig{
			uniqueByArgs: true,
		}

		payload1 := []byte(`{"message":"test1"}`)
		payload2 := []byte(`{"message":"test2"}`)

		key1 := computeUniqueKey(kind, payload1, queue, cfg)
		key2 := computeUniqueKey(kind, payload2, queue, cfg)

		if key1 == key2 {
			t.Error("different payloads should produce different keys")
		}
	})
}
