-- Retain the legacy ORM table. Workers must be stopped before this migration.
ALTER TABLE public.openclaw_gateway_artifact_receipts
    ALTER COLUMN size_bytes DROP NOT NULL;

DO $$
BEGIN
    IF to_regclass('public.open_claw_gateway_artifact_receipts') IS NOT NULL THEN
        LOCK TABLE public.open_claw_gateway_artifact_receipts,
            public.openclaw_gateway_artifact_receipts IN ACCESS EXCLUSIVE MODE;

        CREATE TEMP TABLE hai_openclaw_artifact_import
            (LIKE public.openclaw_gateway_artifact_receipts) ON COMMIT DROP;
        ALTER TABLE hai_openclaw_artifact_import ALTER COLUMN mime_type DROP NOT NULL;
        INSERT INTO hai_openclaw_artifact_import
            SELECT (jsonb_populate_record(NULL::public.openclaw_gateway_artifact_receipts,
                to_jsonb(legacy))).*
            FROM public.open_claw_gateway_artifact_receipts AS legacy;
        UPDATE hai_openclaw_artifact_import SET mime_type = COALESCE(mime_type, '');

        IF EXISTS (
            SELECT 1 FROM hai_openclaw_artifact_import AS source
            JOIN public.openclaw_gateway_artifact_receipts AS target
                USING (execution_reference, artifact_digest)
            WHERE (to_jsonb(source) - 'id') IS DISTINCT FROM (to_jsonb(target) - 'id')
        ) THEN
            RAISE EXCEPTION 'Conflicting OpenClaw artifact histories; reconcile before migration';
        END IF;

        INSERT INTO public.openclaw_gateway_artifact_receipts
            SELECT * FROM hai_openclaw_artifact_import
            ON CONFLICT (execution_reference, artifact_digest) DO NOTHING;
    END IF;
END $$;
