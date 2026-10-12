-- Ordinary source sync history does not carry request idempotency metadata.
-- Empty strings are not idempotency keys and must not collide in this index.
SET LOCAL lock_timeout = '5s';

DROP INDEX IF EXISTS public.ux_source_sync_jobs_owner_source_idempotency;

CREATE UNIQUE INDEX ux_source_sync_jobs_owner_source_idempotency
    ON public.source_sync_jobs (owner_identity, source_id, idempotency_key_hash)
    WHERE owner_identity IS NOT NULL
      AND btrim(owner_identity) <> ''
      AND idempotency_key_hash IS NOT NULL
      AND btrim(idempotency_key_hash) <> '';
