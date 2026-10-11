-- Explicit registry-managed configuration authority; no inferred enrollment.
-- Historical origins remain unmanaged and enabled, preserving prior semantics.
-- The migration runner applies this file inside one transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE public.account_feeds
    ADD COLUMN config_version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT chk_account_feeds_config_version CHECK (config_version > 0);

ALTER TABLE public.operation_source_origins
    ADD COLUMN registry_managed boolean NOT NULL DEFAULT false,
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN registry_config_version bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_source_origins_registry_config_version CHECK (
        registry_config_version >= 0
        AND (NOT registry_managed OR registry_config_version > 0)
    );

CREATE FUNCTION public.hai_guard_account_feed_config_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM 1 FROM public.operation_source_origins
        WHERE owner_user_id = OLD.owner_user_id
          AND workspace_id = OLD.workspace_id
          AND origin_id = OLD.id
          AND registry_managed = true
        FOR UPDATE;
        IF FOUND THEN
            RAISE EXCEPTION 'registry-managed account feeds cannot be deleted'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
        RETURN OLD;
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id THEN
        RAISE EXCEPTION 'account feed identity and scope are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.name IS DISTINCT FROM OLD.name
       OR NEW.provider IS DISTINCT FROM OLD.provider
       OR NEW.account_label IS DISTINCT FROM OLD.account_label
       OR NEW.source_type IS DISTINCT FROM OLD.source_type
       OR NEW.path IS DISTINCT FROM OLD.path
       OR NEW.url IS DISTINCT FROM OLD.url
       OR NEW.project_key IS DISTINCT FROM OLD.project_key
       OR NEW.operation_type IS DISTINCT FROM OLD.operation_type
       OR NEW.enabled IS DISTINCT FROM OLD.enabled THEN
        PERFORM 1 FROM public.operation_source_origins
        WHERE owner_user_id = OLD.owner_user_id
          AND workspace_id = OLD.workspace_id
          AND origin_id = OLD.id
          AND registry_managed = true
        FOR UPDATE;
        IF NEW.config_version IS DISTINCT FROM OLD.config_version + 1 THEN
            RAISE EXCEPTION 'changed account feed configuration must advance its version by one'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    ELSE
        IF NEW.config_version IS DISTINCT FROM OLD.config_version THEN
            RAISE EXCEPTION 'unchanged account feed configuration must retain its version'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_account_feeds_config_version
    BEFORE UPDATE OR DELETE ON public.account_feeds
    FOR EACH ROW EXECUTE FUNCTION public.hai_guard_account_feed_config_version();

CREATE FUNCTION public.hai_guard_account_feed_managed_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.account_feeds AS feed
        JOIN public.operation_source_origins AS origin
          ON origin.owner_user_id = feed.owner_user_id
         AND origin.workspace_id = feed.workspace_id
         AND origin.origin_id = feed.id
        WHERE origin.registry_managed = true
    ) THEN
        RAISE EXCEPTION 'registry-managed account feeds cannot be truncated'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_account_feeds_managed_no_truncate
    BEFORE TRUNCATE ON public.account_feeds
    FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_account_feed_managed_truncate();

CREATE FUNCTION public.hai_check_account_feed_managed_configuration()
RETURNS trigger
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
    checked_origin_id uuid;
    canonical_version bigint;
    canonical_enabled boolean;
    canonical_found boolean;
    enrolling boolean := false;
BEGIN
    IF TG_TABLE_NAME = 'operation_source_origins' THEN
        checked_origin_id := NEW.origin_id;
        IF NEW.registry_managed THEN
            IF TG_OP = 'INSERT' THEN
                enrolling := true;
            ELSIF TG_OP = 'UPDATE' THEN
                enrolling := NOT OLD.registry_managed;
            END IF;
        END IF;
    ELSIF TG_TABLE_NAME = 'account_feeds' THEN
        checked_origin_id := NEW.id;
    ELSE
        RAISE EXCEPTION 'unsupported registry-managed configuration trigger table'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    -- Enrollment must also change the feed's MVCC row, not just lock it:
    -- an older repeatable-read writer must fail rather than miss a new origin.
    -- Only origin enrollment writes; its queued feed check merely locks/reads.
    IF enrolling THEN
        UPDATE public.account_feeds AS feed
        SET config_version = feed.config_version
        WHERE feed.owner_user_id = NEW.owner_user_id
          AND feed.workspace_id = NEW.workspace_id
          AND feed.id = checked_origin_id
        RETURNING feed.config_version, feed.enabled
        INTO canonical_version, canonical_enabled;
    ELSE
        SELECT feed.config_version, feed.enabled
        INTO canonical_version, canonical_enabled
        FROM public.account_feeds AS feed
        WHERE feed.owner_user_id = NEW.owner_user_id
          AND feed.workspace_id = NEW.workspace_id
          AND feed.id = checked_origin_id
        FOR UPDATE;
    END IF;
    canonical_found := FOUND;

    -- Separate query after waiting: re-read current origin, not a queued image
    -- or a pre-lock join snapshot. Canonical writers already hold the feed lock.
    IF EXISTS (
        SELECT 1 FROM public.operation_source_origins AS origin
        WHERE origin.origin_id = checked_origin_id
          AND origin.owner_user_id = NEW.owner_user_id
          AND origin.workspace_id = NEW.workspace_id
          AND origin.registry_managed = true
          AND (NOT canonical_found
               OR origin.registry_config_version IS DISTINCT FROM canonical_version
               OR origin.enabled IS DISTINCT FROM canonical_enabled)
    ) THEN
        RAISE EXCEPTION 'registry-managed account feed configuration must match its canonical origin'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER trg_account_feeds_managed_configuration
    AFTER INSERT OR UPDATE ON public.account_feeds
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.hai_check_account_feed_managed_configuration();

CREATE CONSTRAINT TRIGGER trg_source_origins_managed_configuration
    AFTER INSERT OR UPDATE ON public.operation_source_origins
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.hai_check_account_feed_managed_configuration();

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

    IF OLD.registry_managed AND NOT NEW.registry_managed THEN
        RAISE EXCEPTION 'registry-managed source authority cannot be downgraded'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF OLD.registry_managed THEN
        IF NEW.registry_config_version < OLD.registry_config_version THEN
            RAISE EXCEPTION 'registry-managed source configuration version cannot decrease'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
        IF NEW.registry_config_version = OLD.registry_config_version
           AND (NEW.config_digest IS DISTINCT FROM OLD.config_digest
                OR NEW.enabled IS DISTINCT FROM OLD.enabled) THEN
            RAISE EXCEPTION 'changed registry-managed source configuration must advance its version'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    IF NEW.config_digest IS NOT DISTINCT FROM OLD.config_digest
       AND NEW.registry_managed IS NOT DISTINCT FROM OLD.registry_managed
       AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled
       AND NEW.registry_config_version IS NOT DISTINCT FROM OLD.registry_config_version THEN
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
