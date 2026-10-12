CREATE TABLE public.trello_webhook_receipts (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    source_id uuid NOT NULL REFERENCES public.connected_sources(id) ON DELETE CASCADE,
    action_id character varying(24) NOT NULL,
    board_id character varying(24) NOT NULL,
    card_id character varying(24) NOT NULL DEFAULT '',
    card_name character varying(512) NOT NULL DEFAULT '',
    card_url character varying(1024) NOT NULL DEFAULT '',
    action_type character varying(80) NOT NULL,
    action_text text NOT NULL DEFAULT '',
    actor_id character varying(24) NOT NULL DEFAULT '',
    occurred_at timestamp with time zone NOT NULL,
    fingerprint character(64) NOT NULL,
    durable_job_id uuid NOT NULL REFERENCES public.durable_jobs(id) ON DELETE RESTRICT,
    sync_job_id uuid REFERENCES public.source_sync_jobs(id) ON DELETE SET NULL,
    status character varying(20) NOT NULL DEFAULT 'queued',
    received_at timestamp with time zone NOT NULL DEFAULT now(),
    completed_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ux_trello_webhook_source_action UNIQUE (source_id, action_id),
    CONSTRAINT ux_trello_webhook_durable_job UNIQUE (durable_job_id),
    CONSTRAINT ck_trello_webhook_action_id CHECK (action_id ~ '^[0-9a-f]{24}$'),
    CONSTRAINT ck_trello_webhook_board_id CHECK (board_id ~ '^([0-9a-f]{24}|[A-Za-z0-9]{8})$'),
    CONSTRAINT ck_trello_webhook_card_id CHECK (card_id = '' OR card_id ~ '^[0-9a-f]{24}$'),
    CONSTRAINT ck_trello_webhook_fingerprint CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_trello_webhook_status CHECK (status IN ('queued', 'dispatched', 'completed', 'ignored', 'failed'))
);

CREATE INDEX ix_trello_webhook_status_received
    ON public.trello_webhook_receipts (status, received_at);
