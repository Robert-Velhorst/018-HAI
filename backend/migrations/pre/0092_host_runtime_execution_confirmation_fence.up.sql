ALTER TABLE public.host_runtime_jobs
    ADD COLUMN IF NOT EXISTS execution_confirmed_at TIMESTAMPTZ;

-- Older workers did not persist their final confirmation. Treat every job
-- already leased at migration time as potentially executing so a new server
-- never reports a cancellation it cannot prove.
UPDATE public.host_runtime_jobs
SET execution_confirmed_at = COALESCE(execution_confirmed_at, updated_at, created_at, now())
WHERE status = 'leased'
  AND execution_confirmed_at IS NULL;

COMMENT ON COLUMN public.host_runtime_jobs.execution_confirmed_at IS
    'Durable worker launch fence: cancellation may revoke a lease only before this timestamp is recorded.';
