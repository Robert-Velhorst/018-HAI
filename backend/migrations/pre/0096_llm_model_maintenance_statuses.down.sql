DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM public.llm_model_maintenances
         WHERE status IN ('operator_managed', 'approval_required')
    ) THEN
        RAISE EXCEPTION 'model-maintenance history contains statuses introduced by 0096; preserve the records and keep the expanded status constraint';
    END IF;
END $$;

ALTER TABLE public.llm_model_maintenances
    DROP CONSTRAINT IF EXISTS chk_llm_model_maintenance_status;

ALTER TABLE public.llm_model_maintenances
    ADD CONSTRAINT chk_llm_model_maintenance_status CHECK (
        status IN (
            'not_enforced', 'failed', 'current', 'provider_managed',
            'installed', 'updated'
        )
    );
