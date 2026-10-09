ALTER TABLE public.automation_launch_events
    ADD COLUMN IF NOT EXISTS requires_approval BOOLEAN NOT NULL DEFAULT FALSE;
