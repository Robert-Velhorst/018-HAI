ALTER TABLE public.host_runtime_jobs
    ADD COLUMN IF NOT EXISTS stop_revision BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN public.host_runtime_jobs.stop_revision IS
    'Persisted emergency-stop revision under which this approval was created; a later revision invalidates unstarted work.';
