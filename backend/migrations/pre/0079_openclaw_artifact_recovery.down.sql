DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM openclaw_gateway_artifact_collections LIMIT 1) THEN
        RAISE EXCEPTION 'Retain OpenClaw artifact collection history; reconcile before rollback';
    END IF;
END $$;
DROP TABLE IF EXISTS openclaw_gateway_artifact_collections;
