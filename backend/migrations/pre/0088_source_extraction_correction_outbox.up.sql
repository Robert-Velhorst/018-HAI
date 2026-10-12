CREATE TABLE public.source_extraction_corrections (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_identity VARCHAR(255) NOT NULL,
    source_id UUID NOT NULL,
    extraction_id UUID NOT NULL,
    idempotency_key_hash VARCHAR(64) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    expected_revision TIMESTAMPTZ NOT NULL,
    patch_json JSONB NOT NULL,
    before_state_json JSONB NOT NULL,
    requires_retraction BOOLEAN NOT NULL DEFAULT FALSE,
    phase VARCHAR(40) NOT NULL,
    status VARCHAR(24) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    durable_job_id UUID NOT NULL REFERENCES public.durable_jobs(id) ON DELETE RESTRICT,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    applied_revision TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT ck_source_extraction_correction_status
        CHECK (status IN ('pending', 'completed', 'conflict', 'failed')),
    CONSTRAINT ck_source_extraction_correction_phase
        CHECK (phase IN (
            'intent_persisted', 'workflow_retracted', 'patch_applied',
            'workflow_reconciled', 'index_updated', 'graph_projected',
            'memory_recorded', 'completed', 'conflict_reconcile', 'conflict', 'failed'
        )),
    CONSTRAINT ck_source_extraction_correction_attempts
        CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 10)
);

CREATE UNIQUE INDEX ux_source_extraction_corrections_owner_idempotency
    ON public.source_extraction_corrections (owner_identity, extraction_id, idempotency_key_hash);

CREATE UNIQUE INDEX ux_source_extraction_corrections_active_extraction
    ON public.source_extraction_corrections (owner_identity, extraction_id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX ux_source_extraction_corrections_durable_job
    ON public.source_extraction_corrections (durable_job_id);

CREATE INDEX ix_source_extraction_corrections_owner_history
    ON public.source_extraction_corrections (owner_identity, extraction_id, created_at DESC);

CREATE INDEX ix_source_extraction_corrections_recovery
    ON public.source_extraction_corrections (status, updated_at)
    WHERE status = 'pending';
