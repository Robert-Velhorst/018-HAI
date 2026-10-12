ALTER TABLE public.task_operations
    DROP CONSTRAINT chk_task_operations_status,
    DROP CONSTRAINT chk_task_operations_lifecycle;

ALTER TABLE public.task_operations
    ADD CONSTRAINT chk_task_operations_status
        CHECK (status IN ('running', 'completed', 'needs_review', 'canceled')),
    ADD CONSTRAINT chk_task_operations_lifecycle CHECK (
        (status = 'running'
            AND btrim(lease_owner) <> ''
            AND leased_at IS NOT NULL
            AND task_plan_id = ''
            AND completed_at IS NULL
            AND last_error = '')
        OR
        (status = 'completed'
            AND lease_owner = ''
            AND leased_at IS NULL
            AND btrim(task_plan_id) <> ''
            AND completed_at IS NOT NULL
            AND last_error = '')
        OR
        (status = 'needs_review'
            AND lease_owner = ''
            AND leased_at IS NULL
            AND completed_at IS NULL
            AND btrim(last_error) <> '')
        OR
        (status = 'canceled'
            AND lease_owner = ''
            AND leased_at IS NULL
            AND task_plan_id = ''
            AND completed_at IS NULL
            AND btrim(last_error) <> '')
    );

CREATE OR REPLACE FUNCTION public.task_operations_guard_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.owner_identity IS DISTINCT FROM OLD.owner_identity
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.mode IS DISTINCT FROM OLD.mode
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.lease_generation IS DISTINCT FROM OLD.lease_generation THEN
        RAISE EXCEPTION 'task operation identity is immutable';
    END IF;
    IF OLD.status <> 'running' THEN
        RAISE EXCEPTION 'terminal task operation state is immutable';
    END IF;
    IF NEW.status NOT IN ('running', 'completed', 'needs_review', 'canceled') THEN
        RAISE EXCEPTION 'invalid task operation transition';
    END IF;
    RETURN NEW;
END;
$$;
