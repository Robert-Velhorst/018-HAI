ALTER TABLE public.llm_model_maintenances
    DROP CONSTRAINT IF EXISTS chk_llm_model_maintenance_status;

ALTER TABLE public.llm_model_maintenances
    ADD CONSTRAINT chk_llm_model_maintenance_status CHECK (
        status IN (
            'not_enforced', 'failed', 'current', 'provider_managed',
            'installed', 'updated', 'operator_managed', 'approval_required'
        )
    );
