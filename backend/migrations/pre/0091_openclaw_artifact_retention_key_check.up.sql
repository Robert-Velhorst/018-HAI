CREATE TABLE public.openclaw_artifact_retention_key_check (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    encrypted_check BYTEA NOT NULL CHECK (octet_length(encrypted_check) >= 28),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
