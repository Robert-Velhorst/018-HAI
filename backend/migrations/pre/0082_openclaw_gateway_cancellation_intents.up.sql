ALTER TABLE openclaw_gateway_session_receipts
    ADD COLUMN cancellation_intent_id varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN cancellation_status varchar(30) NOT NULL DEFAULT '',
    ADD COLUMN cancellation_message varchar(512) NOT NULL DEFAULT '',
    ADD COLUMN cancellation_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN cancellation_at timestamptz,
    ADD COLUMN cancellation_tried_at timestamptz;

ALTER TABLE openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_status
        CHECK (cancellation_status IN ('', 'requested', 'awaiting_identity', 'acknowledged', 'delivery_failed', 'not_required')),
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_attempts
        CHECK (cancellation_attempts >= 0),
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_intent
        CHECK ((cancellation_intent_id = '' AND cancellation_status = '') OR
               (cancellation_intent_id <> '' AND cancellation_at IS NOT NULL AND cancellation_status <> ''));

CREATE UNIQUE INDEX idx_openclaw_gateway_cancellation_intent_id
    ON openclaw_gateway_session_receipts (cancellation_intent_id)
    WHERE cancellation_intent_id <> '';

CREATE INDEX idx_openclaw_gateway_cancellations_pending
    ON openclaw_gateway_session_receipts (created_at ASC, execution_reference ASC)
    WHERE cancellation_intent_id <> '' AND cancellation_status NOT IN ('acknowledged', 'not_required') AND reconciled_at IS NULL;
