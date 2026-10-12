ALTER TABLE public.openclaw_gateway_session_receipts
    ADD COLUMN IF NOT EXISTS session_id character varying(256) DEFAULT '',
    ADD COLUMN IF NOT EXISTS requested_model character varying(255) DEFAULT '';
