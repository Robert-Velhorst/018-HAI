DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.execution_authorization_workflow_decision_claims
    ) THEN
        RAISE EXCEPTION
            'cannot roll back workflow-decision approval claims after execution; preserve execution history';
    END IF;
END $$;

DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_claim_workflow_decision
    ON public.execution_authorization_consumptions;
DROP FUNCTION IF EXISTS public.hai_claim_execution_authorization_workflow_decision_consumption();

DROP TABLE public.execution_authorization_workflow_decision_claims;
