LOCK TABLE public.host_runtime_jobs IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.host_runtime_jobs) THEN
        RAISE EXCEPTION 'rollback 0065_host_runtime_jobs refused: host runtime job data exists; preserve it before dropping the table';
    END IF;
END
$$;

DROP TABLE IF EXISTS public.host_runtime_jobs;
