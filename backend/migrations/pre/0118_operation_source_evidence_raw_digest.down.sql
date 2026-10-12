ALTER TABLE public.operations
    DROP CONSTRAINT IF EXISTS chk_operations_source_evidence_raw_sha256;

ALTER TABLE public.operations
    DROP COLUMN IF EXISTS source_evidence_raw_sha256;
