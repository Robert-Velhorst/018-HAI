-- GUARDED OPERATOR ROLLBACK: stop workers and preserve/export review evidence.
-- Dropping replay authority must never silently re-enable uncertain work.
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.durable_jobs IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.durable_jobs
        WHERE replay_policy IS DISTINCT FROM 'at_least_once' OR status IN ('needs_review', 'settling')
    ) THEN
        RAISE EXCEPTION 'rollback refused: durable review or replay authority exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP INDEX IF EXISTS public.idx_durable_jobs_review_hold;
ALTER TABLE public.durable_jobs
    DROP CONSTRAINT IF EXISTS chk_durable_jobs_replay_policy,
    DROP COLUMN IF EXISTS replay_policy;
