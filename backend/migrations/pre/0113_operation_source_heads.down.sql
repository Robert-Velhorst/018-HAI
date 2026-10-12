-- GUARDED OPERATOR ROLLBACK: refuse removal of any origin, head, accepted revision
-- or positive observation epoch. No data rows are deleted.
-- Stop source writers; explicit operator approval and a verified backup/export
-- are required. Operations, operation events and observation history remain.
-- The migration runner keeps locks, preflight and empty-only removal transactional.
SET LOCAL lock_timeout = '5s';

LOCK TABLE public.operation_source_origins,
    public.operation_source_observations,
    public.operation_source_heads,
    public.operation_source_head_revisions IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operation_source_origins)
       OR EXISTS (SELECT 1 FROM public.operation_source_heads)
       OR EXISTS (SELECT 1 FROM public.operation_source_head_revisions)
       OR EXISTS (
           SELECT 1 FROM public.operation_source_observations WHERE config_epoch > 0
       ) THEN
        RAISE EXCEPTION 'rollback refused: source origin, head, revision or configuration epoch history exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_source_head_revisions_no_truncate
    ON public.operation_source_head_revisions;
DROP TRIGGER IF EXISTS trg_source_head_revisions_immutable
    ON public.operation_source_head_revisions;
DROP TABLE IF EXISTS public.operation_source_head_revisions;

DROP TRIGGER IF EXISTS trg_source_heads_no_truncate ON public.operation_source_heads;
DROP TRIGGER IF EXISTS trg_source_heads_guard ON public.operation_source_heads;
DROP FUNCTION IF EXISTS public.hai_guard_source_head_mutation();
DROP TABLE IF EXISTS public.operation_source_heads;

DROP TRIGGER IF EXISTS trg_source_origins_no_truncate ON public.operation_source_origins;
DROP TRIGGER IF EXISTS trg_source_origins_guard ON public.operation_source_origins;
DROP FUNCTION IF EXISTS public.hai_guard_source_origin_mutation();
DROP TABLE IF EXISTS public.operation_source_origins;

ALTER TABLE public.operation_source_observations
    DROP CONSTRAINT IF EXISTS uq_source_head_observations_scope_epoch,
    DROP CONSTRAINT IF EXISTS uq_source_head_observations_scope_id,
    DROP CONSTRAINT IF EXISTS chk_operation_source_observations_config_epoch,
    DROP COLUMN IF EXISTS config_epoch;
