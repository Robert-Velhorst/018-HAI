-- GUARDED OPERATOR ROLLBACK: refuse removal while any operation carries even
-- partial structured source identity. Lock writers before checking provenance.
-- Explicit operator approval and a verified backup/export are required before
-- execution; stop operation writers and deploy compatible application code first.
-- Empty-only rollback removes owned identity schema and immutability guards.
-- Operations and operation events remain; original URI and revision fields remain.
SET LOCAL lock_timeout = '5s';

LOCK TABLE public.operations IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.operations
        WHERE source_provider <> ''
           OR source_account <> ''
           OR source_external_id <> ''
           OR source_identity_hash <> ''
    ) THEN
        RAISE EXCEPTION 'rollback refused: operations contain structured source identity'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_operations_source_identity_immutable
    ON public.operations;
DROP FUNCTION IF EXISTS public.hai_reject_operation_source_identity_mutation();
DROP INDEX IF EXISTS public.idx_operations_owner_workspace_source_identity;

ALTER TABLE public.operations
    DROP CONSTRAINT IF EXISTS chk_operations_source_identity_group,
    DROP COLUMN IF EXISTS source_provider,
    DROP COLUMN IF EXISTS source_account,
    DROP COLUMN IF EXISTS source_external_id,
    DROP COLUMN IF EXISTS source_identity_hash;
