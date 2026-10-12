CREATE TABLE IF NOT EXISTS openclaw_retained_artifacts (
    execution_reference VARCHAR(64) NOT NULL,
    artifact_digest VARCHAR(64) NOT NULL CHECK (artifact_digest ~ '^[a-f0-9]{64}$'),
    owner_identity VARCHAR(255) NOT NULL CHECK (length(owner_identity) > 0),
    source_event_id UUID NOT NULL REFERENCES automation_launch_events(id) ON DELETE RESTRICT,
    content_sha256 VARCHAR(64) NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 0 AND 8388608),
    encrypted_content BYTEA NOT NULL,
    retained_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (execution_reference, artifact_digest),
    FOREIGN KEY (execution_reference, artifact_digest)
        REFERENCES openclaw_gateway_artifact_receipts(execution_reference, artifact_digest) ON DELETE RESTRICT,
    CHECK (octet_length(encrypted_content) = size_bytes + 28)
);
CREATE INDEX IF NOT EXISTS idx_openclaw_retained_artifacts_owner
    ON openclaw_retained_artifacts(owner_identity);
