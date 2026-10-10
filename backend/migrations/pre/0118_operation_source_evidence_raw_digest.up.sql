ALTER TABLE public.operations
    ADD COLUMN IF NOT EXISTS source_evidence_raw_sha256 text NOT NULL DEFAULT '';

ALTER TABLE public.operations
    ADD CONSTRAINT chk_operations_source_evidence_raw_sha256
    CHECK (source_evidence_raw_sha256 = '' OR source_evidence_raw_sha256 ~ '^[0-9a-f]{64}$');
