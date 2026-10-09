-- Structured source identity foundation only: no latest-source or claim fence.
-- Historical rows retain four empty fields; display URIs are not provenance.
-- Reconciliation requires a separately reviewed migration path, not an UPDATE
-- that upgrades an unidentified historical row through the ordinary runtime.
SET LOCAL lock_timeout = '5s';

ALTER TABLE public.operations
    ADD COLUMN source_provider text NOT NULL DEFAULT '',
    ADD COLUMN source_account text NOT NULL DEFAULT '',
    ADD COLUMN source_external_id text NOT NULL DEFAULT '',
    ADD COLUMN source_identity_hash text NOT NULL DEFAULT '';

ALTER TABLE public.operations
    ADD CONSTRAINT chk_operations_source_identity_group CHECK (
        (
            source_provider = ''
            AND source_account = ''
            AND source_external_id = ''
            AND source_identity_hash = ''
        )
        OR (
            btrim(source_provider) <> ''
            AND octet_length(source_provider) <= 64
            AND btrim(source_account) <> ''
            AND octet_length(source_account) <= 1024
            AND btrim(source_external_id) <> ''
            AND octet_length(source_external_id) <= 4096
            AND source_identity_hash <> ''
            AND octet_length(source_identity_hash) = 64
            AND source_identity_hash ~ '^[0-9a-f]{64}$'
            AND source_revision_hash IS NOT NULL
            AND btrim(source_revision_hash) <> ''
            AND octet_length(source_revision_hash) <= 4096
        )
    );

-- Revisions coexist under one identity; this lookup index is not unique.
CREATE INDEX idx_operations_owner_workspace_source_identity
    ON public.operations (owner_user_id, workspace_id, source_identity_hash)
    WHERE source_identity_hash <> '';

CREATE FUNCTION public.hai_reject_operation_source_identity_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.source_provider IS DISTINCT FROM OLD.source_provider
       OR NEW.source_account IS DISTINCT FROM OLD.source_account
       OR NEW.source_external_id IS DISTINCT FROM OLD.source_external_id
       OR NEW.source_identity_hash IS DISTINCT FROM OLD.source_identity_hash THEN
        RAISE EXCEPTION 'operation source identity is immutable; reconciliation requires a separate migration path'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF OLD.source_identity_hash <> ''
       AND NEW.source_revision_hash IS DISTINCT FROM OLD.source_revision_hash THEN
        RAISE EXCEPTION 'identified operation source revision is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_operations_source_identity_immutable
    BEFORE UPDATE ON public.operations
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_operation_source_identity_mutation();
