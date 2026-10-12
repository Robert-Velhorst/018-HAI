-- Persist replay authority before any scheduler is started. Stop old workers
-- during upgrade: they do not understand review-held occurrences.
SET LOCAL lock_timeout = '5s';

ALTER TABLE public.durable_jobs
    ADD COLUMN IF NOT EXISTS replay_policy text NOT NULL DEFAULT 'at_least_once';

ALTER TABLE public.durable_jobs
    ALTER COLUMN replay_policy SET DEFAULT 'at_least_once',
    ALTER COLUMN replay_policy SET NOT NULL,
    ADD CONSTRAINT chk_durable_jobs_replay_policy
    CHECK (replay_policy IN ('at_least_once', 'review_unknown'));

-- Ambient scans can fan out across non-transactional participants. Preserve
-- active legacy occurrences, but never blindly replay a lost invocation.
UPDATE public.durable_jobs SET replay_policy = 'review_unknown'
WHERE kind = 'ambient.scan' AND status IN ('pending', 'running', 'needs_review');

CREATE INDEX idx_durable_jobs_review_hold
    ON public.durable_jobs (queue, kind) WHERE status IN ('needs_review', 'settling');
