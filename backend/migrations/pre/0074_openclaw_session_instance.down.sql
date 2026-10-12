DO $$
BEGIN
    LOCK TABLE public.openclaw_gateway_session_receipts IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (
        SELECT 1
        FROM public.openclaw_gateway_session_receipts
        WHERE (session_id IS NOT NULL AND BTRIM(session_id) <> '')
           OR (requested_model IS NOT NULL AND BTRIM(requested_model) <> '')
    ) THEN
        RAISE EXCEPTION
            'OpenClaw session-instance or requested-model data exists; preserve it before rolling back migration 0074';
    END IF;
END $$;

ALTER TABLE public.openclaw_gateway_session_receipts
    DROP COLUMN IF EXISTS requested_model,
    DROP COLUMN IF EXISTS session_id;
