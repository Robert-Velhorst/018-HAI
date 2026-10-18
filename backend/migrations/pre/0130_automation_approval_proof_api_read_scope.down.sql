DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.automation_approval_proof_consumptions
        WHERE scope = 'automation.api.read'
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'rollback refused: API-read approval proof records exist; preserve the immutable ledger before rollback'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE public.automation_approval_proof_consumptions
    DROP CONSTRAINT chk_automation_approval_proof_scope,
    ADD CONSTRAINT chk_automation_approval_proof_scope CHECK (
        scope IN (
            'automation.api.mutate',
            'automation.script.execute',
            'automation.docker.start',
            'automation.agent-runtime.execute'
        )
    );
