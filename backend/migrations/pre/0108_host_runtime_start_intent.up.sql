ALTER TABLE public.host_runtime_jobs
    ADD COLUMN IF NOT EXISTS approval_digest TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS start_intent_id UUID,
    ADD COLUMN IF NOT EXISTS start_intent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS start_intent_worker_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS start_intent_lease_digest TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS start_intent_approval_digest TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS start_intent_stop_revision BIGINT,
    ADD COLUMN IF NOT EXISTS actual_start_ack_at TIMESTAMPTZ;

-- A lease from the pre-protocol worker does not prove whether Resume occurred.
-- Preserve the record and its existing evidence, but prohibit automatic reuse.
UPDATE public.host_runtime_jobs
SET status = 'needs_review',
    lease_digest = '',
    lease_expires = NULL,
    review_required_at = COALESCE(review_required_at, updated_at, created_at, now()),
    review_reason = 'pre-protocol host lease has no actual-start acknowledgment; operator review required',
    updated_at = now()
WHERE status = 'leased';

ALTER TABLE public.host_runtime_jobs
    ADD CONSTRAINT chk_host_runtime_jobs_approval_digest CHECK (
        approval_digest = '' OR approval_digest ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT chk_host_runtime_jobs_start_intent_binding CHECK (
        (start_intent_id IS NULL
            AND start_intent_at IS NULL
            AND start_intent_worker_id = ''
            AND start_intent_lease_digest = ''
            AND start_intent_approval_digest = ''
            AND start_intent_stop_revision IS NULL
            AND actual_start_ack_at IS NULL)
        OR
        (start_intent_id IS NOT NULL
            AND start_intent_at IS NOT NULL
            AND length(btrim(start_intent_worker_id)) > 0
            AND start_intent_lease_digest ~ '^[0-9a-f]{64}$'
            AND start_intent_approval_digest ~ '^[0-9a-f]{64}$'
            AND start_intent_stop_revision >= 0)
    ),
    ADD CONSTRAINT chk_host_runtime_jobs_start_ack_requires_intent CHECK (
        actual_start_ack_at IS NULL OR start_intent_id IS NOT NULL
    );

COMMENT ON COLUMN public.host_runtime_jobs.approval_digest IS
    'SHA-256 binding of the immutable approved host-job identity and content.';
COMMENT ON COLUMN public.host_runtime_jobs.start_intent_id IS
    'One-shot bridge start identity; never reset or replaced after durable consumption.';
COMMENT ON COLUMN public.host_runtime_jobs.actual_start_ack_at IS
    'Timestamp of the authenticated bridge actual-start report; not independent OS proof.';

CREATE OR REPLACE FUNCTION public.prevent_host_runtime_start_binding_rewrite()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF ROW(NEW.id, NEW.owner_identity, NEW.runtime_id, NEW.task_id, NEW.prompt,
           NEW.workspace_key, NEW.approval_expires_at, NEW.created_at, NEW.stop_revision)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.owner_identity, OLD.runtime_id, OLD.task_id, OLD.prompt,
           OLD.workspace_key, OLD.approval_expires_at, OLD.created_at, OLD.stop_revision) THEN
        RAISE EXCEPTION 'host runtime approved job binding is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.approval_digest <> '' AND NEW.approval_digest IS DISTINCT FROM OLD.approval_digest THEN
        RAISE EXCEPTION 'host runtime approval digest is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.start_intent_id IS NOT NULL AND
       ROW(NEW.start_intent_id, NEW.start_intent_at, NEW.start_intent_worker_id,
           NEW.start_intent_lease_digest, NEW.start_intent_approval_digest,
           NEW.start_intent_stop_revision)
       IS DISTINCT FROM
       ROW(OLD.start_intent_id, OLD.start_intent_at, OLD.start_intent_worker_id,
           OLD.start_intent_lease_digest, OLD.start_intent_approval_digest,
           OLD.start_intent_stop_revision) THEN
        RAISE EXCEPTION 'host runtime start intent is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.actual_start_ack_at IS NOT NULL AND NEW.actual_start_ack_at IS DISTINCT FROM OLD.actual_start_ack_at THEN
        RAISE EXCEPTION 'host runtime actual-start acknowledgment is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.actual_start_ack_at IS NULL AND NEW.actual_start_ack_at IS NOT NULL AND
       (OLD.start_intent_id IS NULL OR NEW.start_intent_id IS DISTINCT FROM OLD.start_intent_id OR
        NEW.start_intent_worker_id IS DISTINCT FROM NEW.worker_id OR
        NEW.start_intent_lease_digest IS DISTINCT FROM NEW.lease_digest OR
        NEW.start_intent_approval_digest IS DISTINCT FROM NEW.approval_digest OR
        NEW.start_intent_stop_revision IS DISTINCT FROM NEW.stop_revision) THEN
        RAISE EXCEPTION 'host runtime actual-start acknowledgment does not match its consumed intent'
            USING ERRCODE = '55000';
    END IF;

    RETURN NEW;
END
$function$;

DROP TRIGGER IF EXISTS trg_host_runtime_start_binding_immutable ON public.host_runtime_jobs;
CREATE TRIGGER trg_host_runtime_start_binding_immutable
BEFORE UPDATE ON public.host_runtime_jobs
FOR EACH ROW
EXECUTE FUNCTION public.prevent_host_runtime_start_binding_rewrite();
