DO $$
BEGIN
    LOCK TABLE public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (
        SELECT 1
        FROM public.openclaw_gateway_session_receipts
        WHERE usage_snapshot_json IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'OpenClaw usage snapshots exist; preserve snapshot evidence before rolling back migration 0077';
    END IF;
END $$;

DROP INDEX IF EXISTS public.idx_openclaw_usage_pending;
ALTER TABLE public.openclaw_gateway_session_receipts DROP COLUMN IF EXISTS usage_snapshot_json;
CREATE INDEX idx_openclaw_usage_pending
    ON public.openclaw_gateway_session_receipts (usage_next_attempt_at, created_at)
    WHERE status = 'terminal' AND usage_summary = '' AND usage_attempts < 8 AND session_id <> '';
