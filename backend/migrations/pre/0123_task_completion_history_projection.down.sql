DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.task_completion_plan_history LIMIT 1) THEN
        RAISE EXCEPTION 'refusing to discard immutable task completion history projections'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_task_completion_plan_logs_project_history
    ON public.task_completion_plan_logs;
DROP TABLE IF EXISTS public.task_completion_plan_history;
DROP FUNCTION IF EXISTS public.hai_project_task_completion_history();
DROP FUNCTION IF EXISTS public.hai_enforce_task_completion_history_binding();
