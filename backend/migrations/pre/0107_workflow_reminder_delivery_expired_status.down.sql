DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.workflow_reminder_delivery_attempts
        WHERE status = 'expired'
    ) THEN
        RAISE EXCEPTION 'refusing to remove expired reminder delivery receipts';
    END IF;
END
$$;

ALTER TABLE public.workflow_reminder_delivery_attempts
    DROP CONSTRAINT chk_workflow_reminder_delivery_attempt;

ALTER TABLE public.workflow_reminder_delivery_attempts
    ADD CONSTRAINT chk_workflow_reminder_delivery_attempt CHECK (
        attempt_number BETWEEN 1 AND 3
        AND status IN ('delivered', 'retryable_failure', 'suppressed', 'dead_lettered')
        AND char_length(btrim(reason)) BETWEEN 1 AND 1000
        AND authority = 'internal_reminder_delivery_receipt'
        AND reminder_digest ~ '^[0-9a-f]{64}$'
        AND record_digest ~ '^[0-9a-f]{64}$'
    );
