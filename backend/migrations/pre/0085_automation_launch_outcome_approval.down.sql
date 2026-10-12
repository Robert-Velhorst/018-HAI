LOCK TABLE public.automation_launch_events IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.automation_launch_events
        WHERE requires_approval IS TRUE
    ) THEN
        RAISE EXCEPTION 'rollback refused: automation launch approval review state would be discarded'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE public.automation_launch_events
    DROP COLUMN requires_approval;
