DO $$
BEGIN
    LOCK TABLE public.openclaw_maintenance_targets IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (
        SELECT 1
        FROM public.openclaw_maintenance_targets
        WHERE check_failures IS DISTINCT FROM 0
    ) THEN
        RAISE EXCEPTION
            'OpenClaw maintenance check retry history exists; preserve it before rolling back migration 0083';
    END IF;
END $$;

ALTER TABLE public.openclaw_maintenance_targets
    DROP COLUMN check_failures;
