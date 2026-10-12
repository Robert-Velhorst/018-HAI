DO $migration$
BEGIN
    IF to_regclass('public.model_run_telemetries') IS NULL THEN
        RETURN;
    END IF;

    EXECUTE 'LOCK TABLE public.model_run_telemetries IN ACCESS EXCLUSIVE MODE';

    IF EXISTS (
        SELECT 1
          FROM public.model_run_telemetries
         WHERE usage_source <> 'estimated'
    ) THEN
        RAISE EXCEPTION 'rollback refused: token usage provenance must remain attached to telemetry rows';
    END IF;
END
$migration$;

ALTER TABLE public.model_run_telemetries
    DROP CONSTRAINT IF EXISTS chk_model_run_telemetry_usage_source,
    DROP COLUMN IF EXISTS usage_source;
