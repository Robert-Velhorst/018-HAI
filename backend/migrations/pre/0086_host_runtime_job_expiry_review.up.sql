ALTER TABLE public.host_runtime_jobs
    ADD COLUMN IF NOT EXISTS approval_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS review_required_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS review_reason TEXT NOT NULL DEFAULT '';

-- Existing queued approvals get the same fixed 24-hour validity as new work.
-- Jobs whose deadline has already passed are made explicitly non-runnable.
UPDATE public.host_runtime_jobs
SET approval_expires_at = created_at + INTERVAL '24 hours'
WHERE approval_expires_at IS NULL;

-- Allow the intervention states before backfilling rows into those states.
ALTER TABLE public.host_runtime_jobs
    DROP CONSTRAINT IF EXISTS chk_host_runtime_jobs_status;

UPDATE public.host_runtime_jobs
SET status = 'expired',
    lease_digest = '',
    lease_expires = NULL,
    review_required_at = COALESCE(review_required_at, now()),
    review_reason = COALESCE(NULLIF(BTRIM(review_reason), ''), 'approved host task expired before execution'),
    updated_at = now()
WHERE status = 'pending'
  AND approval_expires_at <= now();

-- A lease past its deadline (or missing its token/deadline) has an unknown
-- execution outcome. Preserve the original worker and completion/evidence
-- fields, invalidate only the active lease, and require operator review.
UPDATE public.host_runtime_jobs
SET status = 'needs_review',
    lease_digest = '',
    lease_expires = NULL,
    review_required_at = COALESCE(review_required_at, now()),
    review_reason = COALESCE(NULLIF(BTRIM(review_reason), ''), 'host execution outcome is unknown; operator review required'),
    updated_at = now()
WHERE status = 'leased'
  AND (
      lease_expires IS NULL
      OR lease_expires <= now()
      OR BTRIM(lease_digest) = ''
      OR approval_expires_at <= now()
  );

ALTER TABLE public.host_runtime_jobs
    ALTER COLUMN approval_expires_at SET NOT NULL;

ALTER TABLE public.host_runtime_jobs
    ADD CONSTRAINT chk_host_runtime_jobs_status
    CHECK (status IN ('pending', 'leased', 'completed', 'cancelled', 'expired', 'needs_review'));

ALTER TABLE public.host_runtime_jobs
    ADD CONSTRAINT chk_host_runtime_jobs_intervention_state
    CHECK (
        status NOT IN ('expired', 'needs_review')
        OR (
            review_required_at IS NOT NULL
            AND length(BTRIM(review_reason)) > 0
            AND lease_digest = ''
            AND lease_expires IS NULL
        )
    );

CREATE INDEX IF NOT EXISTS idx_host_runtime_jobs_fresh_pending
    ON public.host_runtime_jobs (runtime_id, approval_expires_at, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_host_runtime_jobs_expired_lease
    ON public.host_runtime_jobs (runtime_id, lease_expires)
    WHERE status = 'leased';

COMMENT ON COLUMN public.host_runtime_jobs.approval_expires_at IS
    'Hard expiry for the approval authorizing queued host execution; expired work is never leased.';

COMMENT ON COLUMN public.host_runtime_jobs.review_required_at IS
    'Time the host job entered an intervention state because approval or execution outcome was no longer safe to assume.';

COMMENT ON COLUMN public.host_runtime_jobs.review_reason IS
    'Non-secret audit reason for an expired approval or uncertain host execution outcome.';
