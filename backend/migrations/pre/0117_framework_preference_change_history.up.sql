CREATE TABLE IF NOT EXISTS public.framework_preference_changes (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    owner_identity character varying(255) NOT NULL,
    framework_id character varying(160) NOT NULL,
    sequence bigint NOT NULL,
    actor character varying(255) NOT NULL,
    reason character varying(1024) NOT NULL,
    before_json jsonb,
    after_json jsonb NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    previous_event_digest character(64),
    event_digest character(64) NOT NULL,
    CONSTRAINT framework_preference_changes_pkey PRIMARY KEY (id),
    CONSTRAINT uq_framework_preference_changes_owner_framework_sequence
        UNIQUE (owner_identity, framework_id, sequence),
    CONSTRAINT chk_framework_preference_changes_owner CHECK (btrim(owner_identity) <> ''),
    CONSTRAINT chk_framework_preference_changes_framework CHECK (btrim(framework_id) <> ''),
    CONSTRAINT chk_framework_preference_changes_sequence CHECK (sequence > 0),
    CONSTRAINT chk_framework_preference_changes_actor CHECK (btrim(actor) <> ''),
    CONSTRAINT chk_framework_preference_changes_reason CHECK (btrim(reason) <> ''),
    CONSTRAINT chk_framework_preference_changes_snapshots CHECK (
        (before_json IS NULL OR jsonb_typeof(before_json) = 'object')
        AND jsonb_typeof(after_json) = 'object'
    ),
    CONSTRAINT chk_framework_preference_changes_digests CHECK (
        event_digest ~ '^[0-9a-f]{64}$'
        AND (previous_event_digest IS NULL OR previous_event_digest ~ '^[0-9a-f]{64}$')
    )
);

CREATE INDEX IF NOT EXISTS idx_framework_preference_changes_owner_created
    ON public.framework_preference_changes USING btree (owner_identity, occurred_at DESC);

CREATE OR REPLACE FUNCTION public.reject_framework_preference_change_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'framework preference change history is append-only';
END;
$$;

CREATE TRIGGER framework_preference_changes_no_update_delete
    BEFORE UPDATE OR DELETE ON public.framework_preference_changes
    FOR EACH ROW EXECUTE FUNCTION public.reject_framework_preference_change_mutation();

CREATE TRIGGER framework_preference_changes_no_truncate
    BEFORE TRUNCATE ON public.framework_preference_changes
    FOR EACH STATEMENT EXECUTE FUNCTION public.reject_framework_preference_change_mutation();
