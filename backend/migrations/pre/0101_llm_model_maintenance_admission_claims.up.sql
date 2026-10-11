CREATE TABLE public.llm_model_maintenance_admission_claims (
    provider_id VARCHAR(120) NOT NULL,
    model_id VARCHAR(255) NOT NULL,
    configuration_fingerprint VARCHAR(64) NOT NULL,
    claim_token UUID NOT NULL,
    state VARCHAR(24) NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    retry_after TIMESTAMPTZ NOT NULL,
    finalized_at TIMESTAMPTZ,
    last_outcome VARCHAR(24),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT pk_llm_model_maintenance_admission_claims
        PRIMARY KEY (provider_id, model_id, configuration_fingerprint),
    CONSTRAINT chk_llm_model_maintenance_admission_identity
        CHECK (length(btrim(provider_id)) > 0 AND length(btrim(model_id)) > 0),
    CONSTRAINT chk_llm_model_maintenance_admission_fingerprint
        CHECK (configuration_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_llm_model_maintenance_admission_state
        CHECK (
            (state = 'in_progress' AND finalized_at IS NULL AND last_outcome IS NULL)
            OR (state = 'retry_wait' AND finalized_at IS NOT NULL AND last_outcome = 'retry')
            OR (state = 'succeeded' AND finalized_at IS NOT NULL AND last_outcome = 'verified')
        )
);

CREATE INDEX idx_llm_model_maintenance_admission_expiry
    ON public.llm_model_maintenance_admission_claims (retry_after);

COMMENT ON TABLE public.llm_model_maintenance_admission_claims IS
    'Owner-free, fingerprint-scoped Ollama maintenance admission and bounded retry state; no endpoint, credential, or prompt data is stored.';
