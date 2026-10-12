LOCK TABLE public.openclaw_retained_artifacts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.openclaw_retained_artifacts
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'refusing to remove OpenClaw artifact owner-binding constraints and indexes while retained artifacts exist; forget retained artifacts through HAI before rolling back this migration';
    END IF;
END
$$;

ALTER TABLE public.openclaw_retained_artifacts
    DROP CONSTRAINT IF EXISTS fk_openclaw_retained_artifacts_owner_receipt;

ALTER TABLE public.openclaw_retained_artifacts
    DROP CONSTRAINT IF EXISTS fk_openclaw_retained_artifacts_owner_event;

DROP INDEX IF EXISTS public.uq_openclaw_session_receipts_artifact_owner_binding;
DROP INDEX IF EXISTS public.uq_automation_launch_events_artifact_owner_binding;
