-- GUARDED OPERATOR ROLLBACK: refuse removal of any observation or clock history,
-- or any operation observation provenance. No data rows are deleted.
-- Stop observation and operation writers; explicit operator approval and a
-- verified backup/export are required. Operations and operation events remain.
-- The migration runner keeps locks, preflight and empty-only removal transactional.
SET LOCAL lock_timeout = '5s';

LOCK TABLE public.operation_source_observation_clocks,
    public.operation_source_observations,
    public.operations IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operation_source_observation_clocks)
       OR EXISTS (SELECT 1 FROM public.operation_source_observations)
       OR EXISTS (
           SELECT 1 FROM public.operations
           WHERE source_observation_id IS NOT NULL
              OR source_observation_generation <> 0
       ) THEN
        RAISE EXCEPTION 'rollback refused: source observation history or operation provenance exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_operations_source_observation_immutable
    ON public.operations;
DROP FUNCTION IF EXISTS public.hai_reject_operation_source_observation_mutation();

ALTER TABLE public.operations
    DROP CONSTRAINT IF EXISTS fk_operations_source_observation_scope,
    DROP CONSTRAINT IF EXISTS chk_operations_source_observation_group,
    DROP COLUMN IF EXISTS source_observation_id,
    DROP COLUMN IF EXISTS source_observation_generation;

DROP TRIGGER IF EXISTS trg_source_observations_no_truncate
    ON public.operation_source_observations;
DROP TRIGGER IF EXISTS trg_source_observations_immutable
    ON public.operation_source_observations;
DROP FUNCTION IF EXISTS public.hai_reject_source_observation_mutation();
DROP TABLE IF EXISTS public.operation_source_observations;

DROP TRIGGER IF EXISTS trg_source_observation_clocks_no_truncate
    ON public.operation_source_observation_clocks;
DROP TRIGGER IF EXISTS trg_source_observation_clocks_monotonic
    ON public.operation_source_observation_clocks;
DROP FUNCTION IF EXISTS public.hai_guard_source_observation_clock_mutation();
DROP TABLE IF EXISTS public.operation_source_observation_clocks;
