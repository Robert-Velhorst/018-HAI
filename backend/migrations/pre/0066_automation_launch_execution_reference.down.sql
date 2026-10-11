DROP INDEX IF EXISTS public.idx_automation_launch_events_execution_reference;

ALTER TABLE public.automation_launch_events
    DROP COLUMN IF EXISTS execution_reference;
