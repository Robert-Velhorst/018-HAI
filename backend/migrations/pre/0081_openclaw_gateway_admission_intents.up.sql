ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_session_receipts_status;

ALTER TABLE openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_session_receipts_status
        CHECK (status IN ('admitting', 'not_admitted', 'admitted', 'needs_review', 'terminal'));

CREATE INDEX IF NOT EXISTS idx_openclaw_gateway_session_receipts_unresolved
    ON openclaw_gateway_session_receipts (created_at ASC)
    WHERE status IN ('admitting', 'admitted', 'needs_review') AND reconciled_at IS NULL;
