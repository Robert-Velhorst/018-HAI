ALTER TABLE public.brain_skill_selection_events
    ADD COLUMN catalog_fingerprint VARCHAR(64) NOT NULL DEFAULT ''
    CHECK (catalog_fingerprint = '' OR catalog_fingerprint ~ '^[0-9a-f]{64}$');
