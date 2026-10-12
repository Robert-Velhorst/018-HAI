CREATE TABLE IF NOT EXISTS openclaw_gateway_session_receipts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    execution_reference character varying(64) NOT NULL UNIQUE,
    owner_identity character varying(255) NOT NULL,
    runtime_task_id character varying(120) NOT NULL,
    session_key TEXT NOT NULL,
    run_id character varying(256) NOT NULL,
    status character varying(30) NOT NULL,
    terminal_status character varying(30) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    terminal_at TIMESTAMPTZ,
    reconciled_at TIMESTAMPTZ,
    CONSTRAINT chk_openclaw_gateway_session_receipts_status
        CHECK (status IN ('admitted', 'terminal')),
    CONSTRAINT chk_openclaw_gateway_session_receipts_terminal_status
        CHECK (terminal_status IN ('', 'completed', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_openclaw_gateway_session_receipts_active
    ON openclaw_gateway_session_receipts (created_at)
    WHERE status = 'admitted' AND reconciled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_openclaw_gateway_session_receipts_owner_task
    ON openclaw_gateway_session_receipts (owner_identity, runtime_task_id, created_at DESC);
