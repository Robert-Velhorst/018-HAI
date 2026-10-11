-- Refuse to restore the legacy predicate if ordinary sync history now contains
-- repeated empty metadata; preserve the current index instead of deleting rows.
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
        RAISE EXCEPTION 'cannot restore the legacy source sync idempotency index while duplicate metadata rows exist';
    END IF;
END $$;

SET LOCAL lock_timeout = '5s';

DROP INDEX IF EXISTS public.ux_source_sync_jobs_owner_source_idempotency;

CREATE UNIQUE INDEX ux_source_sync_jobs_owner_source_idempotency
    ON public.source_sync_jobs (owner_identity, source_id, idempotency_key_hash)
    WHERE owner_identity IS NOT NULL
      AND idempotency_key_hash IS NOT NULL;
