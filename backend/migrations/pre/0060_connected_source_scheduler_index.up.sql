-- Source scheduler sweeps repeatedly inspect only sources that are eligible
-- for autonomous background work. Keep paused and revoked history out of the
-- index so stopping a source lowers both scan and index-maintenance cost.
CREATE INDEX IF NOT EXISTS idx_connected_sources_scheduler_active
    ON public.connected_sources (updated_at DESC)
    WHERE enabled = true
      AND status NOT IN ('paused', 'revoked');
