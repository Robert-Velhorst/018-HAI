DROP INDEX IF EXISTS public.uq_automation_launch_events_event_key;

ALTER TABLE public.automation_launch_events
    DROP COLUMN IF EXISTS event_key;
