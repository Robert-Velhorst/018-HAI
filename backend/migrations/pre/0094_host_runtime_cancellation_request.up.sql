ALTER TABLE public.host_runtime_jobs
    ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ;

COMMENT ON COLUMN public.host_runtime_jobs.cancel_requested_at IS
    'Durable owner Stop request observed by the active bridge; cancellation is terminal only after verified contained-process termination.';
