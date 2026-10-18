LOCK TABLE public.automation_approval_proof_issuances IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.automation_approval_proof_issuances LIMIT 1
    ) THEN
        RAISE EXCEPTION 'rollback refused: immutable approval proof issuance records exist; preserve the ledger before rollback'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_automation_approval_proof_issuances_no_truncate
    ON public.automation_approval_proof_issuances;
DROP TRIGGER IF EXISTS trg_automation_approval_proof_issuances_immutable
    ON public.automation_approval_proof_issuances;
DROP TABLE IF EXISTS public.automation_approval_proof_issuances;
