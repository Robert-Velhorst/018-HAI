ALTER TABLE public.openclaw_gateway_session_receipts
    ADD COLUMN IF NOT EXISTS usage_snapshot_json JSONB
        CHECK (usage_snapshot_json IS NULL OR jsonb_typeof(usage_snapshot_json) = 'object');

DROP INDEX IF EXISTS public.idx_openclaw_usage_pending;
CREATE INDEX idx_openclaw_usage_pending
    ON public.openclaw_gateway_session_receipts (usage_next_attempt_at, created_at)
    WHERE status = 'terminal' AND usage_snapshot_json IS NULL AND usage_attempts < 8 AND session_id <> '';
