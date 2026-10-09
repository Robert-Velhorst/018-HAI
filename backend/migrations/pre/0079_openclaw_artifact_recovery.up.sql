CREATE TABLE IF NOT EXISTS openclaw_gateway_artifact_collections (
    execution_reference VARCHAR(64) PRIMARY KEY REFERENCES openclaw_gateway_session_receipts(execution_reference) ON DELETE RESTRICT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 8),
    next_attempt_at TIMESTAMPTZ,
    captured_at TIMESTAMPTZ,
    descriptor_count INTEGER CHECK (descriptor_count BETWEEN 0 AND 20),
    CHECK ((captured_at IS NULL AND descriptor_count IS NULL) OR (captured_at IS NOT NULL AND descriptor_count IS NOT NULL AND next_attempt_at IS NULL))
);
CREATE INDEX IF NOT EXISTS idx_openclaw_artifact_collections_pending
    ON openclaw_gateway_artifact_collections(next_attempt_at)
    WHERE captured_at IS NULL AND attempts < 8;
