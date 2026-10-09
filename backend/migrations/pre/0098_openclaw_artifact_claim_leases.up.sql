ALTER TABLE openclaw_gateway_artifact_collections
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claim_token UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_openclaw_artifact_claim_pair'
          AND conrelid = 'openclaw_gateway_artifact_collections'::regclass
    ) THEN
        ALTER TABLE openclaw_gateway_artifact_collections
            ADD CONSTRAINT chk_openclaw_artifact_claim_pair
            CHECK ((claimed_at IS NULL) = (claim_token IS NULL));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_openclaw_artifact_collections_claimed
    ON openclaw_gateway_artifact_collections(claimed_at)
    WHERE claimed_at IS NOT NULL AND captured_at IS NULL AND attempts < 8;

CREATE UNIQUE INDEX IF NOT EXISTS idx_openclaw_artifact_collections_claim_token
    ON openclaw_gateway_artifact_collections(claim_token)
    WHERE claim_token IS NOT NULL;
