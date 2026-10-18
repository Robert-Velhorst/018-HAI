ALTER TABLE public.automation_approval_proof_consumptions
    DROP CONSTRAINT chk_automation_approval_proof_scope,
    ADD CONSTRAINT chk_automation_approval_proof_scope CHECK (
        scope IN (
            'automation.api.read',
            'automation.api.mutate',
            'automation.script.execute',
            'automation.docker.start',
            'automation.agent-runtime.execute'
        )
    );
