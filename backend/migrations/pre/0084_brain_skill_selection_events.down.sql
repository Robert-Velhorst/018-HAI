-- The HAI migration runner executes rollback statements in one transaction.
-- The table lock prevents a new decision racing the audit-loss check.
LOCK TABLE public.brain_skill_selection_events IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.brain_skill_selection_events LIMIT 1) THEN
        RAISE EXCEPTION 'rollback refused: brain skill selection audit records would be discarded'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER trg_brain_skill_selection_events_immutable
    ON public.brain_skill_selection_events;
DROP TRIGGER trg_brain_skill_selection_events_no_truncate
    ON public.brain_skill_selection_events;
DROP TABLE public.brain_skill_selection_events;
DROP FUNCTION public.hai_reject_brain_skill_selection_event_mutation();
