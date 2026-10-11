DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM openclaw_gateway_artifact_receipts LIMIT 1) THEN
        RAISE EXCEPTION 'refusing to drop populated OpenClaw Gateway artifact receipt ledger';
    END IF;
END $$;

DROP TABLE IF EXISTS openclaw_gateway_artifact_receipts;
