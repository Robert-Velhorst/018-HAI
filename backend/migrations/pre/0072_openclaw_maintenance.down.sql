DO $$
BEGIN
    LOCK TABLE public.openclaw_maintenance_targets, public.openclaw_maintenance_jobs
        IN ACCESS EXCLUSIVE MODE;

    IF EXISTS (SELECT 1 FROM public.openclaw_maintenance_jobs) THEN
        RAISE EXCEPTION
            'OpenClaw maintenance job history exists; preserve operational and audit records before rolling back migration 0072';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.openclaw_maintenance_targets
        WHERE (policy IS DISTINCT FROM 'observe'
               AND policy IS DISTINCT FROM 'auto_install_verified')
           OR installed IS DISTINCT FROM ''
           OR available IS DISTINCT FROM ''
           OR state IS DISTINCT FROM 'unknown'
           OR reason IS DISTINCT FROM ''
           OR checked_at IS NOT NULL
           OR verified_at IS NOT NULL
           OR receipt_id IS DISTINCT FROM ''
           OR updated_by IS DISTINCT FROM ''
    ) THEN
        RAISE EXCEPTION
            'OpenClaw maintenance target state or policy has changed; preserve it before rolling back migration 0072';
    END IF;
END $$;

DROP TABLE IF EXISTS public.openclaw_maintenance_jobs;
DROP TABLE IF EXISTS public.openclaw_maintenance_targets;
