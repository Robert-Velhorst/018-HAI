CREATE TABLE IF NOT EXISTS host_runtime_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_identity TEXT NOT NULL,
    runtime_id TEXT NOT NULL,
    task_id TEXT NOT NULL UNIQUE,
    prompt TEXT NOT NULL,
    workspace_key TEXT NOT NULL,
    status TEXT NOT NULL,
    worker_id TEXT NOT NULL DEFAULT '',
    lease_digest TEXT NOT NULL DEFAULT '',
    lease_expires TIMESTAMPTZ,
    output TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    reconciled_at TIMESTAMPTZ,
    CONSTRAINT chk_host_runtime_jobs_status CHECK (status IN ('pending', 'leased', 'completed', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_host_runtime_jobs_runtime_status_created
    ON host_runtime_jobs (runtime_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_host_runtime_jobs_owner_created
    ON host_runtime_jobs (owner_identity, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_host_runtime_jobs_completed_unreconciled
    ON host_runtime_jobs (completed_at)
    WHERE status = 'completed' AND reconciled_at IS NULL;
