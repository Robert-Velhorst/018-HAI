LOCK TABLE public.openclaw_gateway_reconcile_cursors IN ACCESS EXCLUSIVE MODE;
LOCK TABLE public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM openclaw_gateway_session_receipts
        WHERE cancellation_status = 'review_required'
           OR cancellation_automatic_attempts > 0
           OR cancellation_next_attempt_at IS NOT NULL
           OR cancellation_delivery_lease_until IS NOT NULL
           OR cancellation_delivery_token <> ''
           OR cancellation_review_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back OpenClaw retry state while retry, lease, or operator review evidence exists';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM openclaw_gateway_reconcile_cursors
        WHERE after_created_at IS NOT NULL OR cycle_high_created_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back OpenClaw reconciliation cursor while scan progress exists';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_openclaw_gateway_cancellations_due;
DROP INDEX IF EXISTS idx_openclaw_gateway_reconcile_pending_page;
DROP TABLE IF EXISTS openclaw_gateway_reconcile_cursors;

ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_delivery_lease,
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_automatic_attempts,
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_status,
    DROP COLUMN cancellation_review_at,
    DROP COLUMN cancellation_delivery_token,
    DROP COLUMN cancellation_delivery_lease_until,
    DROP COLUMN cancellation_next_attempt_at,
    DROP COLUMN cancellation_automatic_attempts;

ALTER TABLE openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_status
        CHECK (cancellation_status IN ('', 'requested', 'awaiting_identity', 'acknowledged', 'delivery_failed', 'not_required'));
