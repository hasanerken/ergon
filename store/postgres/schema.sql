-- Ergon Task Queue Schema for PostgreSQL

-- Tasks table
CREATE TABLE IF NOT EXISTS queue_tasks (
    id VARCHAR(36) PRIMARY KEY,
    kind VARCHAR(255) NOT NULL,
    queue VARCHAR(255) NOT NULL DEFAULT 'default',
    state VARCHAR(50) NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    retried INTEGER NOT NULL DEFAULT 0,
    timeout_seconds INTEGER NOT NULL DEFAULT 1800,
    scheduled_at TIMESTAMP,
    enqueued_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    payload JSONB NOT NULL,
    result JSONB,
    error TEXT,
    metadata JSONB,
    unique_key VARCHAR(255),
    group_key VARCHAR(255),
    rate_limit_scope VARCHAR(255),
    recurring BOOLEAN NOT NULL DEFAULT FALSE,
    cron_schedule VARCHAR(255),
    interval_seconds INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Task groups table
CREATE TABLE IF NOT EXISTS task_groups (
    group_key VARCHAR(255) PRIMARY KEY,
    max_size INTEGER NOT NULL,
    max_delay_seconds INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    task_count INTEGER NOT NULL DEFAULT 0
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_tasks_state ON queue_tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_queue_state ON queue_tasks(queue, state);
CREATE INDEX IF NOT EXISTS idx_tasks_queue_priority ON queue_tasks(queue, priority DESC, enqueued_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_scheduled ON queue_tasks(scheduled_at) WHERE state = 'scheduled';
CREATE INDEX IF NOT EXISTS idx_tasks_kind ON queue_tasks(kind);
CREATE INDEX IF NOT EXISTS idx_tasks_unique_key ON queue_tasks(unique_key) WHERE unique_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_group_key ON queue_tasks(group_key) WHERE group_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_state_queue ON queue_tasks(state, queue);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to auto-update updated_at
CREATE TRIGGER update_tasks_updated_at BEFORE UPDATE ON queue_tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Leader election table for distributed scheduling
CREATE TABLE IF NOT EXISTS leader_leases (
    lease_key VARCHAR(255) PRIMARY KEY,
    holder_id VARCHAR(255) NOT NULL,
    acquired_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    renewed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_leases_expires ON leader_leases(expires_at);
