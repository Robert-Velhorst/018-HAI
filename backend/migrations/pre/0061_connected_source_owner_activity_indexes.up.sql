-- Owner-scoped source views and bounded activity history are on the interactive
-- control-plane path. These composite indexes avoid full scans as accounts,
-- sync jobs, and audit records grow.
CREATE INDEX IF NOT EXISTS idx_connected_sources_owner_updated
    ON public.connected_sources (owner_identity, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_sync_jobs_source_created
    ON public.source_sync_jobs (source_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_audit_logs_source_created
    ON public.source_audit_logs (source_id, created_at DESC);
