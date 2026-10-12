ALTER TABLE public.workflow_items
    ADD COLUMN IF NOT EXISTS success_criteria jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.workflow_items
    DROP CONSTRAINT IF EXISTS chk_workflow_items_success_criteria;

ALTER TABLE public.workflow_items
    ADD CONSTRAINT chk_workflow_items_success_criteria
    CHECK (
        CASE
            WHEN jsonb_typeof(success_criteria) = 'array'
                THEN jsonb_array_length(success_criteria) <= 50
            ELSE false
        END
    );
