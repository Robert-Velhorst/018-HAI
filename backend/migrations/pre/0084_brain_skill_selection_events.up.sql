CREATE TABLE public.brain_skill_selection_events (
    id BIGSERIAL PRIMARY KEY,
    owner_identity VARCHAR(255) NOT NULL
        CHECK (owner_identity = btrim(owner_identity) AND length(owner_identity) > 0),
    skill_id VARCHAR(128) NOT NULL
        CHECK (skill_id ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    source_commit VARCHAR(40) NOT NULL
        CHECK (length(source_commit) = 40 AND source_commit ~ '^[0-9a-f]{40}$'),
    source_sha256 VARCHAR(64) NOT NULL
        CHECK (length(source_sha256) = 64 AND source_sha256 ~ '^[0-9a-f]{64}$'),
    guidance_sha256 VARCHAR(64) NOT NULL
        CHECK (length(guidance_sha256) = 64 AND guidance_sha256 ~ '^[0-9a-f]{64}$'),
    actor_identity VARCHAR(255) NOT NULL
        CHECK (actor_identity = btrim(actor_identity) AND length(actor_identity) > 0),
    enabled BOOLEAN NOT NULL,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX idx_brain_skill_selection_owner_latest
    ON public.brain_skill_selection_events (owner_identity, skill_id, id DESC);

CREATE OR REPLACE FUNCTION public.hai_reject_brain_skill_selection_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'brain skill selection decisions are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER trg_brain_skill_selection_events_immutable
    BEFORE UPDATE OR DELETE ON public.brain_skill_selection_events
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_brain_skill_selection_event_mutation();

CREATE TRIGGER trg_brain_skill_selection_events_no_truncate
    BEFORE TRUNCATE ON public.brain_skill_selection_events
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_brain_skill_selection_event_mutation();
