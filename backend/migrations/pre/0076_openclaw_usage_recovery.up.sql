ALTER TABLE public.openclaw_gateway_session_receipts
    ADD COLUMN IF NOT EXISTS usage_summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS usage_attempts INTEGER NOT NULL DEFAULT 0 CHECK (usage_attempts BETWEEN 0 AND 8),
    ADD COLUMN IF NOT EXISTS usage_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS usage_captured_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_openclaw_usage_pending
    ON public.openclaw_gateway_session_receipts (usage_next_attempt_at, created_at)
    WHERE status = 'terminal' AND usage_summary = '' AND usage_attempts < 8 AND session_id <> '';
