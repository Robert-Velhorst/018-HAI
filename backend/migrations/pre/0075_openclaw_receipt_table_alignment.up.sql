-- Earlier GORM defaults wrote to open_claw_* while migrations used openclaw_*.
-- Preserve the legacy table and refuse ambiguous histories rather than overwrite.
DO $$
BEGIN
    IF to_regclass('public.open_claw_gateway_session_receipts') IS NOT NULL THEN
        LOCK TABLE public.open_claw_gateway_session_receipts,
            public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

        CREATE TEMP TABLE hai_openclaw_receipt_import
            (LIKE public.openclaw_gateway_session_receipts) ON COMMIT DROP;
        INSERT INTO hai_openclaw_receipt_import
            SELECT (jsonb_populate_record(NULL::public.openclaw_gateway_session_receipts,
                to_jsonb(legacy))).*
            FROM public.open_claw_gateway_session_receipts AS legacy;
        UPDATE hai_openclaw_receipt_import SET
            terminal_status = COALESCE(terminal_status, ''),
            review_reason = COALESCE(review_reason, ''),
            session_id = COALESCE(session_id, ''),
            requested_model = COALESCE(requested_model, '');

        IF EXISTS (
            SELECT 1 FROM hai_openclaw_receipt_import AS source
            JOIN public.openclaw_gateway_session_receipts AS target
                USING (execution_reference)
            WHERE (to_jsonb(source) - 'id') IS DISTINCT FROM (to_jsonb(target) - 'id')
        ) THEN
            RAISE EXCEPTION 'Conflicting OpenClaw receipt histories; reconcile before migration';
        END IF;

        INSERT INTO public.openclaw_gateway_session_receipts
            SELECT * FROM hai_openclaw_receipt_import
            ON CONFLICT (execution_reference) DO NOTHING;
    END IF;
END $$;
