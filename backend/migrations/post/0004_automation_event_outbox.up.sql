CREATE TABLE public.automation_event_outbox (
    id uuid PRIMARY KEY,
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 10,
    next_attempt_at timestamp with time zone NOT NULL,
    lease_token uuid,
    lease_until timestamp with time zone,
    last_error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    published_at timestamp with time zone,
    CONSTRAINT automation_event_outbox_event_type_check
        CHECK (event_type IN ('create', 'update', 'delete')),
    CONSTRAINT automation_event_outbox_status_check
        CHECK (status IN ('pending', 'published', 'dead_lettered')),
    CONSTRAINT automation_event_outbox_attempts_check
        CHECK (attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts),
    CONSTRAINT automation_event_outbox_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX idx_automation_event_outbox_claim
    ON public.automation_event_outbox (status, next_attempt_at, created_at)
    WHERE status = 'pending';

CREATE INDEX idx_automation_event_outbox_aggregate
    ON public.automation_event_outbox (aggregate_id, created_at);
