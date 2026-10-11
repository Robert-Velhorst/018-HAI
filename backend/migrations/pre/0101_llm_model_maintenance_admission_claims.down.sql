DO $migration$
BEGIN
    IF to_regclass('public.llm_model_maintenance_admission_claims') IS NULL THEN
        RETURN;
    END IF;

    EXECUTE 'LOCK TABLE public.llm_model_maintenance_admission_claims IN ACCESS EXCLUSIVE MODE';

    IF EXISTS (
        SELECT 1
          FROM public.llm_model_maintenance_admission_claims
         WHERE state = 'in_progress'
            OR retry_after > clock_timestamp()
    ) THEN
            RAISE EXCEPTION 'rollback refused: active or unexpired Ollama admission claims must remain until their bounded safety window expires';
    END IF;
END
$migration$;

DROP INDEX IF EXISTS public.idx_llm_model_maintenance_admission_expiry;
DROP TABLE IF EXISTS public.llm_model_maintenance_admission_claims;
