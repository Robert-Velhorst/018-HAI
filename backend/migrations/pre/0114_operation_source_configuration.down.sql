-- GUARDED OPERATOR ROLLBACK: registry-managed authority cannot be discarded.
-- Stop source and registry writers; explicit operator approval and a verified
-- backup/export are required. No data rows are deleted or unenrolled.
-- Remove only the owned configuration artifacts and four owned columns; restore the
-- exact 0113 origin guard before removing its three configuration authority columns.
-- The migration runner keeps locks, preflight and restoration transactional.
SET LOCAL lock_timeout = '5s';

LOCK TABLE public.account_feeds,
    public.operation_source_origins IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.operation_source_origins WHERE registry_managed = true
    ) THEN
        RAISE EXCEPTION 'rollback refused: registry-managed source configuration authority exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_account_feeds_config_version ON public.account_feeds;
DROP TRIGGER IF EXISTS trg_account_feeds_managed_no_truncate ON public.account_feeds;
DROP TRIGGER IF EXISTS trg_account_feeds_managed_configuration ON public.account_feeds;
DROP TRIGGER IF EXISTS trg_source_origins_managed_configuration ON public.operation_source_origins;
DROP FUNCTION IF EXISTS public.hai_guard_account_feed_config_version();
DROP FUNCTION IF EXISTS public.hai_guard_account_feed_managed_truncate();
DROP FUNCTION IF EXISTS public.hai_check_account_feed_managed_configuration();
ALTER TABLE public.account_feeds
    DROP CONSTRAINT IF EXISTS chk_account_feeds_config_version,
    DROP COLUMN IF EXISTS config_version;

CREATE OR REPLACE FUNCTION public.hai_guard_source_origin_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'source origins cannot be deleted or truncated'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.origin_id IS DISTINCT FROM OLD.origin_id THEN
        RAISE EXCEPTION 'source origin scope is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.config_digest IS NOT DISTINCT FROM OLD.config_digest THEN
        IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch THEN
            RAISE EXCEPTION 'unchanged source configuration must retain its epoch'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    ELSE
        IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch + 1 THEN
            RAISE EXCEPTION 'changed source configuration must advance its epoch by one'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

ALTER TABLE public.operation_source_origins
    DROP CONSTRAINT IF EXISTS chk_source_origins_registry_config_version,
    DROP COLUMN IF EXISTS registry_config_version,
    DROP COLUMN IF EXISTS registry_managed,
    DROP COLUMN IF EXISTS enabled;
