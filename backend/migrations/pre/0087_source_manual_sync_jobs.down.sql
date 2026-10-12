LOCK TABLE public.source_sync_jobs, public.durable_jobs IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.source_sync_jobs
        WHERE mode = 'manual_async_sync'
    ) THEN
        RAISE EXCEPTION 'manual source sync history exists; preserve its idempotency and ownership metadata before rolling back migration 0087';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.durable_jobs
        WHERE kind = 'source.manual_sync'
    ) THEN
        RAISE EXCEPTION 'manual source sync durable queue records exist; preserve them before rolling back migration 0087';
    END IF;
END $$;

DROP INDEX IF EXISTS public.ux_source_sync_jobs_one_active_manual;
DROP INDEX IF EXISTS public.ux_source_sync_jobs_durable_job;
DROP INDEX IF EXISTS public.ux_source_sync_jobs_owner_source_idempotency;

ALTER TABLE public.source_sync_jobs
    DROP COLUMN IF EXISTS durable_job_id,
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS idempotency_key_hash,
    DROP COLUMN IF EXISTS owner_identity;
