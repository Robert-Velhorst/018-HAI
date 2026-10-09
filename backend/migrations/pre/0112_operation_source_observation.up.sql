-- Durable pre-read observation ordering only, not a final source-head fence.
-- Historical operations retain NULL/zero provenance; no inferred backfill.
-- The migration runner applies this file inside one transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE public.operation_source_observation_clocks (
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    generation bigint NOT NULL,
    CONSTRAINT operation_source_observation_clocks_pkey
        PRIMARY KEY (owner_user_id, workspace_id),
    CONSTRAINT chk_operation_source_observation_clocks_generation
        CHECK (generation > 0)
);

CREATE TABLE public.operation_source_observations (
    id uuid PRIMARY KEY,
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    origin_id uuid NOT NULL,
    config_digest text NOT NULL,
    generation bigint NOT NULL,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_operation_source_observations_scope_generation
        UNIQUE (owner_user_id, workspace_id, generation),
    CONSTRAINT uq_operation_source_observations_scope_id_generation_origin
        UNIQUE (owner_user_id, workspace_id, id, generation, origin_id),
    CONSTRAINT chk_operation_source_observations_config_digest CHECK (
        octet_length(config_digest) = 64
        AND config_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_operation_source_observations_generation
        CHECK (generation > 0)
);

ALTER TABLE public.operation_source_observations
    ADD CONSTRAINT chk_source_observations_nonzero_identity CHECK (
        id <> '00000000-0000-0000-0000-000000000000'::uuid
        AND origin_id <> '00000000-0000-0000-0000-000000000000'::uuid
    );

CREATE FUNCTION public.hai_guard_source_observation_clock_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'source observation clocks cannot be deleted or truncated'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.generation IS DISTINCT FROM OLD.generation + 1 THEN
        RAISE EXCEPTION 'source observation clock scope is immutable and generation must advance by one'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_source_observation_clocks_monotonic
    BEFORE UPDATE OR DELETE ON public.operation_source_observation_clocks
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_guard_source_observation_clock_mutation();

CREATE TRIGGER trg_source_observation_clocks_no_truncate
    BEFORE TRUNCATE ON public.operation_source_observation_clocks
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_guard_source_observation_clock_mutation();

CREATE FUNCTION public.hai_reject_source_observation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'source observations are immutable'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER trg_source_observations_immutable
    BEFORE UPDATE OR DELETE ON public.operation_source_observations
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_source_observation_mutation();

CREATE TRIGGER trg_source_observations_no_truncate
    BEFORE TRUNCATE ON public.operation_source_observations
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_source_observation_mutation();

-- Legacy NULL/zero groups bypass the FK; observed groups require the exact feed origin.
ALTER TABLE public.operations
    ADD COLUMN source_observation_id uuid,
    ADD COLUMN source_observation_generation bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_operations_source_observation_group CHECK (
        (source_observation_id IS NULL AND source_observation_generation = 0)
        OR (
            source_observation_id IS NOT NULL
            AND source_observation_generation > 0
            AND source_identity_hash <> ''
            AND account_feed_id IS NOT NULL
            AND account_feed_id <> '00000000-0000-0000-0000-000000000000'::uuid
        )
    ),
    ADD CONSTRAINT fk_operations_source_observation_scope
        FOREIGN KEY (owner_user_id, workspace_id, source_observation_id, source_observation_generation, account_feed_id)
        REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id)
        MATCH SIMPLE
        ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE FUNCTION public.hai_reject_operation_source_observation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.source_observation_id IS DISTINCT FROM OLD.source_observation_id
       OR NEW.source_observation_generation IS DISTINCT FROM OLD.source_observation_generation
       OR (OLD.source_observation_id IS NOT NULL AND NEW.account_feed_id IS DISTINCT FROM OLD.account_feed_id) THEN
        RAISE EXCEPTION 'operation source observation provenance is immutable; reconciliation requires a separate migration path'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_operations_source_observation_immutable
    BEFORE UPDATE ON public.operations
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_operation_source_observation_mutation();
