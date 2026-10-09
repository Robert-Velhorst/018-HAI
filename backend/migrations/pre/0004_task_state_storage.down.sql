LOCK TABLE
    public.task_completion_plan_logs,
    public.task_review_items,
    public.task_review_decisions
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.task_completion_plan_logs)
       OR EXISTS (SELECT 1 FROM public.task_review_items)
       OR EXISTS (SELECT 1 FROM public.task_review_decisions)
    THEN
        RAISE EXCEPTION 'rollback 0004_task_state_storage refused: task state data exists; preserve it before dropping these tables';
    END IF;
END
$$;

DROP TABLE IF EXISTS public.task_review_decisions;
DROP TABLE IF EXISTS public.task_review_items;
DROP TABLE IF EXISTS public.task_completion_plan_logs;

DROP FUNCTION IF EXISTS public.hai_enforce_task_review_decision_binding();
DROP FUNCTION IF EXISTS public.hai_require_task_review_resolution_state();
DROP FUNCTION IF EXISTS public.hai_enforce_task_review_item_transition();
DROP FUNCTION IF EXISTS public.hai_enforce_task_review_item_insert();
DROP FUNCTION IF EXISTS public.hai_enforce_task_review_item_provenance();
DROP FUNCTION IF EXISTS public.hai_reject_task_audit_truncate();
DROP FUNCTION IF EXISTS public.hai_reject_task_audit_mutation();
