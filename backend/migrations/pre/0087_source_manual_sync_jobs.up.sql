ALTER TABLE public.source_sync_jobs
    ADD COLUMN IF NOT EXISTS owner_identity character varying(255),
    ADD COLUMN IF NOT EXISTS idempotency_key_hash character varying(64),
    ADD COLUMN IF NOT EXISTS request_hash character varying(64),
    ADD COLUMN IF NOT EXISTS durable_job_id uuid;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.source_sync_jobs
        WHERE owner_identity IS NOT NULL
          AND idempotency_key_hash IS NOT NULL
        GROUP BY owner_identity, source_id, idempotency_key_hash
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate source sync idempotency keys exist; resolve them before creating the manual-sync index';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.source_sync_jobs
        WHERE durable_job_id IS NOT NULL
        GROUP BY durable_job_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate source sync durable job ids exist; resolve them before creating the manual-sync index';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.source_sync_jobs
        WHERE mode = 'manual_async_sync'
          AND status IN ('queued', 'running')
        GROUP BY source_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'multiple active manual source sync jobs exist; resolve them before creating the active-job index';
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_sync_jobs_owner_source_idempotency
    ON public.source_sync_jobs (owner_identity, source_id, idempotency_key_hash)
    WHERE owner_identity IS NOT NULL AND idempotency_key_hash IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_sync_jobs_durable_job
    ON public.source_sync_jobs (durable_job_id)
    WHERE durable_job_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_sync_jobs_one_active_manual
    ON public.source_sync_jobs (source_id)
    WHERE mode = 'manual_async_sync' AND status IN ('queued', 'running');
