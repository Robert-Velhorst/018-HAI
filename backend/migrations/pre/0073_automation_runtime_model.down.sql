DO $$
BEGIN
    LOCK TABLE public.automations IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (
        SELECT 1
        FROM public.automations
        WHERE runtime_model IS NOT NULL AND BTRIM(runtime_model) <> ''
    ) THEN
        RAISE EXCEPTION
            'automation runtime model selections exist; preserve them before rolling back migration 0073';
    END IF;
END $$;

ALTER TABLE public.automations
    DROP COLUMN IF EXISTS runtime_model;
