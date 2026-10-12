-- Durable source-head and accepted-revision storage, not a final-effect fence.
-- Historical observations retain epoch zero; runtime must mint positive epochs.
-- No inferred origins, heads, revisions or historical provenance backfill.
-- The migration runner applies this file inside one transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE public.operation_source_origins (
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    origin_id uuid NOT NULL,
    config_digest text NOT NULL,
    config_epoch bigint NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT operation_source_origins_pkey
        PRIMARY KEY (owner_user_id, workspace_id, origin_id),
    CONSTRAINT chk_operation_source_origins_nonzero_identity
        CHECK (origin_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT chk_operation_source_origins_config_digest CHECK (
        octet_length(config_digest) = 64
        AND config_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_operation_source_origins_config_epoch CHECK (config_epoch > 0)
);

ALTER TABLE public.operation_source_observations
    ADD COLUMN config_epoch bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_operation_source_observations_config_epoch
        CHECK (config_epoch >= 0),
    ADD CONSTRAINT uq_source_head_observations_scope_id
        UNIQUE (owner_user_id, workspace_id, id),
    ADD CONSTRAINT uq_source_head_observations_scope_epoch
        UNIQUE (owner_user_id, workspace_id, id, generation, origin_id, config_epoch);

-- Reuse the 0016 operation key; bind heads to observation origin and epoch.
-- Preserve the 0112 five-field observation key used by operation provenance.
CREATE TABLE public.operation_source_heads (
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    source_identity_hash text NOT NULL,
    origin_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    revision_hash text NOT NULL,
    observation_id uuid NOT NULL,
    observation_generation bigint NOT NULL,
    config_epoch bigint NOT NULL,
    state text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT operation_source_heads_pkey
        PRIMARY KEY (owner_user_id, workspace_id, source_identity_hash),
    CONSTRAINT chk_operation_source_heads_identity_hash CHECK (
        octet_length(source_identity_hash) = 64
        AND source_identity_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_operation_source_heads_nonzero_identity CHECK (
        origin_id <> '00000000-0000-0000-0000-000000000000'::uuid
        AND operation_id <> '00000000-0000-0000-0000-000000000000'::uuid
        AND observation_id <> '00000000-0000-0000-0000-000000000000'::uuid
    ),
    CONSTRAINT chk_operation_source_heads_revision CHECK (
        btrim(revision_hash) <> '' AND octet_length(revision_hash) <= 4096
    ),
    CONSTRAINT chk_operation_source_heads_generation CHECK (observation_generation > 0),
    CONSTRAINT chk_operation_source_heads_config_epoch CHECK (config_epoch > 0),
    CONSTRAINT chk_operation_source_heads_state
        CHECK (state IN ('accepted', 'reconciliation_required', 'tombstoned')),
    CONSTRAINT fk_operation_source_heads_origin_scope
        FOREIGN KEY (owner_user_id, workspace_id, origin_id)
        REFERENCES public.operation_source_origins (owner_user_id, workspace_id, origin_id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_operation_source_heads_operation_scope
        FOREIGN KEY (owner_user_id, workspace_id, operation_id)
        REFERENCES public.operations (owner_user_id, workspace_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_operation_source_heads_observation_scope
        FOREIGN KEY (owner_user_id, workspace_id, observation_id, observation_generation, origin_id, config_epoch)
        REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id, config_epoch)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

-- Accepted content digests survive head changes and operation archival/recreation.
-- No FK to the mutable head is needed; runtime records acceptance transactionally.
CREATE TABLE public.operation_source_head_revisions (
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    source_identity_hash text NOT NULL,
    revision_digest text NOT NULL,
    operation_id uuid NOT NULL,
    observation_id uuid NOT NULL,
    CONSTRAINT operation_source_head_revisions_pkey
        PRIMARY KEY (owner_user_id, workspace_id, source_identity_hash, revision_digest),
    CONSTRAINT chk_operation_source_head_revisions_identity_hash CHECK (
        octet_length(source_identity_hash) = 64
        AND source_identity_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_operation_source_head_revisions_revision_digest CHECK (
        octet_length(revision_digest) = 64
        AND revision_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_operation_source_head_revisions_nonzero_identity CHECK (
        operation_id <> '00000000-0000-0000-0000-000000000000'::uuid
        AND observation_id <> '00000000-0000-0000-0000-000000000000'::uuid
    ),
    CONSTRAINT fk_operation_source_head_revisions_operation_scope
        FOREIGN KEY (owner_user_id, workspace_id, operation_id)
        REFERENCES public.operations (owner_user_id, workspace_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_operation_source_head_revisions_observation_scope
        FOREIGN KEY (owner_user_id, workspace_id, observation_id)
        REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE FUNCTION public.hai_guard_source_origin_mutation()
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

CREATE TRIGGER trg_source_origins_guard
    BEFORE UPDATE OR DELETE ON public.operation_source_origins
    FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_origin_mutation();
CREATE TRIGGER trg_source_origins_no_truncate
    BEFORE TRUNCATE ON public.operation_source_origins
    FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_origin_mutation();

CREATE FUNCTION public.hai_guard_source_head_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'source heads cannot be deleted or truncated'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.source_identity_hash IS DISTINCT FROM OLD.source_identity_hash
       OR NEW.origin_id IS DISTINCT FROM OLD.origin_id THEN
        RAISE EXCEPTION 'source head tenant, identity and origin are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    -- Every update, including tombstoning, requires a newer observation.
    IF NEW.observation_generation <= OLD.observation_generation
       OR NEW.config_epoch < OLD.config_epoch THEN
        RAISE EXCEPTION 'source head observation generation must increase and configuration epoch cannot decrease'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_source_heads_guard
    BEFORE UPDATE OR DELETE ON public.operation_source_heads
    FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_head_mutation();
CREATE TRIGGER trg_source_heads_no_truncate
    BEFORE TRUNCATE ON public.operation_source_heads
    FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_head_mutation();

CREATE TRIGGER trg_source_head_revisions_immutable
    BEFORE UPDATE OR DELETE ON public.operation_source_head_revisions
    FOR EACH ROW EXECUTE FUNCTION public.hai_reject_source_observation_mutation();
CREATE TRIGGER trg_source_head_revisions_no_truncate
    BEFORE TRUNCATE ON public.operation_source_head_revisions
    FOR EACH STATEMENT EXECUTE FUNCTION public.hai_reject_source_observation_mutation();
