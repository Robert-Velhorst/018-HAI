ALTER TABLE openclaw_gateway_session_receipts
    ADD COLUMN IF NOT EXISTS review_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_at TIMESTAMPTZ;

ALTER TABLE openclaw_gateway_session_receipts
    DROP CONSTRAINT IF EXISTS chk_openclaw_gateway_session_receipts_status;

ALTER TABLE openclaw_gateway_session_receipts
    ADD CONSTRAINT chk_openclaw_gateway_session_receipts_status
        CHECK (status IN ('admitted', 'needs_review', 'terminal'));

CREATE INDEX IF NOT EXISTS idx_openclaw_gateway_session_receipts_review
    ON openclaw_gateway_session_receipts (review_at ASC)
    WHERE status = 'needs_review';
