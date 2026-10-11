-- Feed configuration and sync observations remain mutable; audits are append-only.
-- Scope enforcement follows the existing owner/workspace store boundary, not RLS.
CREATE TABLE public.account_feeds (
    id uuid NOT NULL,
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    name text NOT NULL,
    provider text NOT NULL,
    account_label text NOT NULL DEFAULT '',
    source_type text NOT NULL,
    path text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    project_key text NOT NULL DEFAULT '',
    operation_type text NOT NULL DEFAULT '',
    enabled boolean NOT NULL,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_items_read bigint NOT NULL DEFAULT 0,
    sync_token uuid,
    sync_started_at timestamptz,
    CONSTRAINT account_feeds_pkey PRIMARY KEY (id),
    CONSTRAINT uq_account_feeds_owner_workspace_id
        UNIQUE (owner_user_id, workspace_id, id),
    CONSTRAINT chk_account_feeds_scope CHECK (
        char_length(btrim(owner_user_id)) > 0
        AND char_length(btrim(workspace_id)) > 0
    ),
    CONSTRAINT chk_account_feeds_content CHECK (
        char_length(btrim(name)) > 0
        AND char_length(btrim(provider)) > 0
    ),
    CONSTRAINT chk_account_feeds_source_type CHECK (
        source_type IN ('local_json_file', 'http_json_feed')
    ),
    CONSTRAINT chk_account_feeds_last_items_read CHECK (last_items_read >= 0),
    CONSTRAINT chk_account_feeds_sync_lock CHECK (
        (sync_token IS NULL) = (sync_started_at IS NULL)
    )
);

CREATE TABLE public.account_feed_audits (
    id uuid NOT NULL,
    feed_id uuid NOT NULL,
    owner_user_id text NOT NULL,
    workspace_id text NOT NULL,
    event_type text NOT NULL,
    message text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT account_feed_audits_pkey PRIMARY KEY (id),
    CONSTRAINT fk_account_feed_audits_feed_owner_workspace
        FOREIGN KEY (owner_user_id, workspace_id, feed_id)
        REFERENCES public.account_feeds (owner_user_id, workspace_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_account_feed_audits_scope CHECK (
        char_length(btrim(owner_user_id)) > 0
        AND char_length(btrim(workspace_id)) > 0
    ),
    CONSTRAINT chk_account_feed_audits_content CHECK (
        char_length(btrim(event_type)) > 0
        AND char_length(btrim(message)) > 0
    )
);

CREATE INDEX idx_account_feed_audits_owner_workspace_feed_created
    ON public.account_feed_audits
    (owner_user_id, workspace_id, feed_id, created_at DESC, id DESC);

CREATE FUNCTION public.hai_reject_account_feed_audit_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'account feed audits are immutable'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER trg_account_feed_audits_immutable
    BEFORE UPDATE OR DELETE ON public.account_feed_audits
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_account_feed_audit_mutation();

CREATE TRIGGER trg_account_feed_audits_no_truncate
    BEFORE TRUNCATE ON public.account_feed_audits
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_account_feed_audit_mutation();
