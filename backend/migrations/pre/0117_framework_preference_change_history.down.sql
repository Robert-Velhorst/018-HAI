DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.framework_preference_changes) THEN
        RAISE EXCEPTION 'cannot drop framework preference change history while audit records exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS framework_preference_changes_no_update_delete
    ON public.framework_preference_changes;
DROP TRIGGER IF EXISTS framework_preference_changes_no_truncate
    ON public.framework_preference_changes;
DROP TABLE IF EXISTS public.framework_preference_changes;
DROP FUNCTION IF EXISTS public.reject_framework_preference_change_mutation();
