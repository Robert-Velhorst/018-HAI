ALTER TABLE openclaw_gateway_session_receipts
    ADD COLUMN cancellation_automatic_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN cancellation_next_attempt_at timestamptz,
    ADD COLUMN cancellation_delivery_lease_until timestamptz,
    ADD COLUMN cancellation_delivery_token varchar(36) NOT NULL DEFAULT '',
    ADD COLUMN cancellation_review_at timestamptz;

ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT chk_openclaw_gateway_cancellation_status,
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_status
        CHECK (cancellation_status IN ('', 'requested', 'awaiting_identity', 'acknowledged', 'delivery_failed', 'not_required', 'review_required')),
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_automatic_attempts
        CHECK (cancellation_automatic_attempts >= 0 AND cancellation_automatic_attempts <= cancellation_attempts),
    ADD CONSTRAINT chk_openclaw_gateway_cancellation_delivery_lease
        CHECK ((cancellation_delivery_token = '' AND cancellation_delivery_lease_until IS NULL) OR
               (cancellation_delivery_token <> '' AND cancellation_delivery_lease_until IS NOT NULL));

CREATE INDEX idx_openclaw_gateway_reconcile_pending_page
    ON openclaw_gateway_session_receipts (created_at ASC, execution_reference ASC)
    WHERE status IN ('admitting', 'admitted', 'needs_review') AND reconciled_at IS NULL;

CREATE INDEX idx_openclaw_gateway_cancellations_due
    ON openclaw_gateway_session_receipts (cancellation_next_attempt_at ASC, created_at ASC, execution_reference ASC)
    WHERE cancellation_intent_id <> ''
      AND status IN ('admitting', 'admitted', 'needs_review')
      AND reconciled_at IS NULL
      AND cancellation_status IN ('requested', 'delivery_failed', 'awaiting_identity');

CREATE TABLE openclaw_gateway_reconcile_cursors (
    name varchar(40) PRIMARY KEY,
    after_created_at timestamptz,
    after_execution_reference varchar(64) NOT NULL DEFAULT '',
    cycle_high_created_at timestamptz,
    cycle_high_reference varchar(64) NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_openclaw_gateway_reconcile_cursor_after
        CHECK ((after_created_at IS NULL AND after_execution_reference = '') OR
               (after_created_at IS NOT NULL AND after_execution_reference <> '')),
    CONSTRAINT chk_openclaw_gateway_reconcile_cursor_high
        CHECK ((cycle_high_created_at IS NULL AND cycle_high_reference = '') OR
               (cycle_high_created_at IS NOT NULL AND cycle_high_reference <> '')),
    CONSTRAINT chk_openclaw_gateway_reconcile_cursor_order
        CHECK (after_created_at IS NULL OR cycle_high_created_at IS NOT NULL),
    CONSTRAINT chk_openclaw_gateway_reconcile_cursor_within_high
        CHECK (after_created_at IS NULL OR (after_created_at, after_execution_reference) <= (cycle_high_created_at, cycle_high_reference))
);

INSERT INTO openclaw_gateway_reconcile_cursors (name)
VALUES ('gateway-sessions')
ON CONFLICT (name) DO NOTHING;
