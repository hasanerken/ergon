package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// NewStoreOptimized creates an optimized PostgreSQL store using pgx with connection pooling
func NewStoreOptimized(ctx context.Context, dsn string) (*Store, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Optimize connection pool for high throughput
	config.MaxConns = 100                      // Much higher for concurrent writes
	config.MinConns = 10                       // Keep connections warm
	config.MaxConnLifetime = 1 * time.Hour     // Recycle connections hourly
	config.MaxConnIdleTime = 30 * time.Minute  // Close idle connections
	config.HealthCheckPeriod = 1 * time.Minute // Regular health checks

	// Performance optimizations
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement // Use prepared statements

	// Convert pgxpool to database/sql compatible
	connStr := stdlib.RegisterConnConfig(config.ConnConfig)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool parameters
	db.SetMaxOpenConns(int(config.MaxConns))
	db.SetMaxIdleConns(int(config.MinConns))
	db.SetConnMaxLifetime(config.MaxConnLifetime)
	db.SetConnMaxIdleTime(config.MaxConnIdleTime)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Store{db: db}, nil
}
