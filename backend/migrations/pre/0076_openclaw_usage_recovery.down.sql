DO $$
BEGIN
    LOCK TABLE public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (
        SELECT 1
        FROM public.openclaw_gateway_session_receipts
        WHERE usage_summary IS DISTINCT FROM ''
           OR usage_attempts IS DISTINCT FROM 0
           OR usage_next_attempt_at IS NOT NULL
           OR usage_captured_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'OpenClaw usage recovery history exists; preserve summaries, retry counts and timestamps before rolling back migration 0076';
    END IF;
END $$;

DROP INDEX IF EXISTS public.idx_openclaw_usage_pending;
ALTER TABLE public.openclaw_gateway_session_receipts
    DROP COLUMN IF EXISTS usage_captured_at,
    DROP COLUMN IF EXISTS usage_next_attempt_at,
    DROP COLUMN IF EXISTS usage_attempts,
    DROP COLUMN IF EXISTS usage_summary;
