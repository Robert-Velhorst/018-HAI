CREATE TABLE IF NOT EXISTS openclaw_gateway_artifact_receipts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    execution_reference character varying(64) NOT NULL,
    artifact_digest character varying(64) NOT NULL,
    artifact_type character varying(120) NOT NULL,
    mime_type character varying(160) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_openclaw_gateway_artifact_receipts_size_bytes CHECK (size_bytes >= 0),
    CONSTRAINT uq_openclaw_gateway_artifact_receipts_reference_digest UNIQUE (execution_reference, artifact_digest),
    CONSTRAINT fk_openclaw_gateway_artifact_receipts_session_receipt
        FOREIGN KEY (execution_reference)
        REFERENCES openclaw_gateway_session_receipts (execution_reference)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_openclaw_gateway_artifact_receipts_reference
    ON openclaw_gateway_artifact_receipts (execution_reference, created_at ASC);
