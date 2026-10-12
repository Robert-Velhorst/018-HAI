-- Durable, generation-fenced ownership for Operation Ledger background work.
-- Keep released rows so the fencing generation remains monotonic.
SET LOCAL lock_timeout = '5s';

CREATE TABLE public.operation_execution_claims (
    operation_id uuid PRIMARY KEY REFERENCES public.operations(id) ON DELETE RESTRICT,
    claim_owner uuid,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    claimed_at timestamptz,
    lease_expires_at timestamptz,
    CONSTRAINT chk_operation_execution_claim_owner_expiry
        CHECK ((claim_owner IS NULL) = (lease_expires_at IS NULL)),
    CONSTRAINT chk_operation_execution_claim_generation
        CHECK (claim_owner IS NULL OR generation > 0)
);

CREATE INDEX idx_operation_execution_claims_active_expiry
    ON public.operation_execution_claims (lease_expires_at, operation_id)
    WHERE claim_owner IS NOT NULL;
