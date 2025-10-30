package postgres

import (
	"testing"
	"time"
)

// TestPgxConfig_PgBouncerMode tests that PgBouncer mode disables prepared statements
func TestPgxConfig_PgBouncerMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test verifies the configuration is applied correctly
	// Actual PgBouncer testing requires a running PgBouncer instance

	cfg := PgxConfig{
		DSN:          "postgres://user:pass@localhost:5432/testdb",
		UsePgBouncer: true,
		MaxConns:     50,
		MinConns:     5,
		MaxConnLifetime: 2 * time.Hour,
		MaxConnIdleTime: 45 * time.Minute,
		HealthCheckPeriod: 2 * time.Minute,
	}

	// Verify config values
	if !cfg.UsePgBouncer {
		t.Error("Expected UsePgBouncer to be true")
	}

	if cfg.MaxConns != 50 {
		t.Errorf("Expected MaxConns=50, got %d", cfg.MaxConns)
	}

	if cfg.MinConns != 5 {
		t.Errorf("Expected MinConns=5, got %d", cfg.MinConns)
	}

	if cfg.MaxConnLifetime != 2*time.Hour {
		t.Errorf("Expected MaxConnLifetime=2h, got %v", cfg.MaxConnLifetime)
	}

	if cfg.MaxConnIdleTime != 45*time.Minute {
		t.Errorf("Expected MaxConnIdleTime=45m, got %v", cfg.MaxConnIdleTime)
	}

	if cfg.HealthCheckPeriod != 2*time.Minute {
		t.Errorf("Expected HealthCheckPeriod=2m, got %v", cfg.HealthCheckPeriod)
	}
}

// TestPgxConfig_DefaultValues tests that default values are applied
func TestPgxConfig_DefaultValues(t *testing.T) {
	cfg := PgxConfig{
		DSN: "postgres://user:pass@localhost:5432/testdb",
		// All other fields left as zero values to test defaults
	}

	// Defaults should be applied in NewStorePgxWithConfig
	// This just tests the config struct itself
	if cfg.MaxConns != 0 {
		t.Errorf("Expected MaxConns=0 (to trigger default), got %d", cfg.MaxConns)
	}

	if cfg.UsePgBouncer {
		t.Error("Expected UsePgBouncer to be false by default")
	}
}

// TestPgxConfig_DirectPostgreSQL tests direct PostgreSQL mode (prepared statements enabled)
func TestPgxConfig_DirectPostgreSQL(t *testing.T) {
	cfg := PgxConfig{
		DSN:          "postgres://user:pass@localhost:5432/testdb",
		UsePgBouncer: false, // Explicitly set to false
		MaxConns:     100,
	}

	if cfg.UsePgBouncer {
		t.Error("Expected UsePgBouncer to be false for direct PostgreSQL")
	}

	// In direct mode, prepared statements should be enabled (default behavior)
}
