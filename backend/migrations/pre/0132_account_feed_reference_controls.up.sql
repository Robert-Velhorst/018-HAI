-- Durable source stop state, deliberately independent of pagination progress.
CREATE TABLE public.account_feed_reference_controls (
    owner_user_id varchar(255) NOT NULL CHECK (owner_user_id <> ''),
    workspace_id varchar(255) NOT NULL CHECK (workspace_id <> ''),
    feed_id uuid NOT NULL CHECK (feed_id <> '00000000-0000-0000-0000-000000000000'),
    enabled boolean NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, workspace_id, feed_id)
);
