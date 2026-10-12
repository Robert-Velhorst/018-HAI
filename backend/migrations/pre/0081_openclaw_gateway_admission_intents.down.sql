DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM openclaw_gateway_session_receipts
        WHERE status IN ('admitting', 'not_admitted')
    ) THEN
        RAISE EXCEPTION 'cannot roll back OpenClaw admission intents while rows use new statuses';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_openclaw_gateway_session_receipts_unresolved;

ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_session_receipts_status;

ALTER TABLE openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_session_receipts_status
        CHECK (status IN ('admitted', 'needs_review', 'terminal'));
