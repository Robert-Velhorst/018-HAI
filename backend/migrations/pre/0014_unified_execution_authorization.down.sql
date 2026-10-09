LOCK TABLE public.execution_authorization_consumptions,
    public.execution_authorization_final_effect_exercises,
    public.execution_authorization_receipts,
    public.workflow_decisions
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.execution_authorization_consumptions)
       OR EXISTS (SELECT 1 FROM public.execution_authorization_final_effect_exercises)
       OR EXISTS (SELECT 1 FROM public.execution_authorization_receipts)
       OR EXISTS (SELECT 1 FROM public.workflow_decisions)
    THEN
        RAISE EXCEPTION 'rollback 0014_unified_execution_authorization refused: authorization or workflow decision data exists; preserve it before rollback';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS trg_execution_authorization_final_effects_no_truncate
    ON public.execution_authorization_final_effect_exercises;
DROP TRIGGER IF EXISTS trg_execution_authorization_final_effects_immutable
    ON public.execution_authorization_final_effect_exercises;
DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_no_truncate
    ON public.execution_authorization_consumptions;
DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_immutable
    ON public.execution_authorization_consumptions;
DROP TRIGGER IF EXISTS trg_execution_authorization_receipts_no_truncate
    ON public.execution_authorization_receipts;
DROP TRIGGER IF EXISTS trg_execution_authorization_receipts_immutable
    ON public.execution_authorization_receipts;

DROP TABLE IF EXISTS public.execution_authorization_final_effect_exercises;
DROP TABLE IF EXISTS public.execution_authorization_consumptions;
DROP TABLE IF EXISTS public.execution_authorization_receipts;

DROP FUNCTION IF EXISTS public.hai_reject_execution_authorization_mutation();

DROP TRIGGER IF EXISTS trg_workflow_decisions_bind_owner
    ON public.workflow_decisions;
DROP FUNCTION IF EXISTS public.hai_bind_workflow_decision_owner();
ALTER TABLE public.workflow_decisions
    DROP CONSTRAINT IF EXISTS fk_workflow_decision_owner_workflow,
    DROP CONSTRAINT IF EXISTS uq_workflow_decision_owner_id,
    DROP CONSTRAINT IF EXISTS chk_workflow_decision_owner_identity,
    DROP COLUMN IF EXISTS owner_identity;
ALTER TABLE public.workflow_items
    DROP CONSTRAINT IF EXISTS uq_workflow_item_owner_id;
ALTER TABLE public.task_review_decisions
    DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id_source,
    DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id;
ALTER TABLE public.standing_mandate_authorization_decisions
    DROP CONSTRAINT IF EXISTS uq_standing_mandate_decision_owner_id_mandate;
ALTER TABLE public.robert_constitution_versions
    DROP CONSTRAINT IF EXISTS uq_robert_constitution_owner_id_version;
