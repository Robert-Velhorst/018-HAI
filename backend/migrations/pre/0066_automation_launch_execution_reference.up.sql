ALTER TABLE public.automation_launch_events
    ADD COLUMN IF NOT EXISTS execution_reference character varying(120);

CREATE INDEX IF NOT EXISTS idx_automation_launch_events_execution_reference
    ON public.automation_launch_events USING btree (execution_reference)
    WHERE execution_reference IS NOT NULL AND execution_reference <> '';
