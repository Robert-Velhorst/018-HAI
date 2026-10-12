DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.workflow_items
        WHERE success_criteria IS DISTINCT FROM '[]'::jsonb
    ) THEN
        RAISE EXCEPTION 'cannot drop workflow success criteria while workflow records contain criteria';
    END IF;
END;
$$;

ALTER TABLE public.workflow_items
    DROP CONSTRAINT IF EXISTS chk_workflow_items_success_criteria;

ALTER TABLE public.workflow_items
    DROP COLUMN IF EXISTS success_criteria;
