CREATE TABLE IF NOT EXISTS public.source_o_auth_states (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    source_id uuid NOT NULL,
    owner_identity character varying(255) NOT NULL,
    state_digest character(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT source_o_auth_states_pkey PRIMARY KEY (id),
    CONSTRAINT fk_source_o_auth_states_source FOREIGN KEY (source_id)
        REFERENCES public.connected_sources(id) ON DELETE CASCADE,
    CONSTRAINT chk_source_o_auth_states_owner CHECK (btrim(owner_identity) <> ''),
    CONSTRAINT chk_source_o_auth_states_digest CHECK (state_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_source_o_auth_states_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_source_o_auth_states_consumed CHECK (
        consumed_at IS NULL OR consumed_at >= created_at
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_oauth_states_source
    ON public.source_o_auth_states (source_id);

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_oauth_states_digest
    ON public.source_o_auth_states (state_digest);

CREATE INDEX IF NOT EXISTS idx_source_oauth_states_source_created
    ON public.source_o_auth_states (source_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_oauth_states_owner_expiry
    ON public.source_o_auth_states (owner_identity, expires_at);
