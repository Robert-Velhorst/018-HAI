-- Additive only: existing reference rows are preserved. Sources without a
-- checkpoint replay once using their per-record immutable change sequences.
CREATE TABLE public.account_feed_reference_checkpoints (
    owner_user_id varchar(255) NOT NULL CHECK (owner_user_id <> ''),
    workspace_id varchar(255) NOT NULL CHECK (workspace_id <> ''),
    feed_id uuid NOT NULL CHECK (feed_id <> '00000000-0000-0000-0000-000000000000'),
    cursor varchar(128) NOT NULL CHECK (cursor ~ '^[A-Za-z0-9_-]{1,128}$'),
    sequence bigint NOT NULL CHECK (sequence > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, workspace_id, feed_id)
);
