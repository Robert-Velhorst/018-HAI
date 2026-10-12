DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.execution_authorization_task_review_claims
    ) THEN
        RAISE EXCEPTION
            'cannot roll back task-review approval claims after execution; preserve execution history';
    END IF;
END $$;

DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_claim_task_review
    ON public.execution_authorization_consumptions;
DROP FUNCTION IF EXISTS public.hai_claim_execution_authorization_task_review_consumption();

DROP TABLE public.execution_authorization_task_review_claims;
