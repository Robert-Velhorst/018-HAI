ALTER TABLE public.automation_launch_events
    ADD COLUMN IF NOT EXISTS event_key character varying(120);

CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_launch_events_event_key
    ON public.automation_launch_events USING btree (event_key)
    WHERE event_key IS NOT NULL AND event_key <> '';
