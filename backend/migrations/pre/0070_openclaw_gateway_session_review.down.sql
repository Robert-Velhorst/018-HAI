LOCK TABLE public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
         FROM public.openclaw_gateway_session_receipts
         WHERE status = 'needs_review'
            OR review_reason IS DISTINCT FROM ''
            OR review_at IS NOT NULL
         LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'cannot roll back OpenClaw Gateway review state while unresolved review status or retained review metadata exists';
    END IF;
END
$$;

DROP INDEX IF EXISTS public.idx_openclaw_gateway_session_receipts_review;

ALTER TABLE public.openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_session_receipts_status;

ALTER TABLE public.openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_session_receipts_status
        CHECK (status IN ('admitted', 'terminal'));

ALTER TABLE openclaw_gateway_session_receipts
    DROP COLUMN IF EXISTS review_reason,
    DROP COLUMN IF EXISTS review_at;
