CREATE INDEX IF NOT EXISTS idx_durable_jobs_queue_lease
    ON public.durable_jobs USING btree (queue, status, locked_at)
    WHERE locked_at IS NOT NULL;
