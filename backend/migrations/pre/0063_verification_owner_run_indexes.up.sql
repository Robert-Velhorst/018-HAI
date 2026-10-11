-- Owner-scoped verification history is ordered by creation time and individual
-- inspector lookups are constrained by run id and owner identity.
CREATE INDEX IF NOT EXISTS idx_verification_runs_owner_created
    ON public.verification_runs (owner_identity, created_at DESC);
