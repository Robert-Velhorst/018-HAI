DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM openclaw_gateway_session_receipts
        WHERE cancellation_intent_id <> ''
    ) THEN
        RAISE EXCEPTION 'cannot roll back OpenClaw cancellation intents while cancellation audit rows exist';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_openclaw_gateway_cancellations_pending;
DROP INDEX IF EXISTS idx_openclaw_gateway_cancellation_intent_id;

ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_intent,
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_attempts,
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_cancellation_status,
    DROP COLUMN cancellation_tried_at,
    DROP COLUMN cancellation_at,
    DROP COLUMN cancellation_attempts,
    DROP COLUMN cancellation_message,
    DROP COLUMN cancellation_status,
    DROP COLUMN cancellation_intent_id;
