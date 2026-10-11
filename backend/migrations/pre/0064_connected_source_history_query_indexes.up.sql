-- Source reindexing and owner-scoped extraction retrieval retain complete
-- history for auditability. These indexes keep the existing history-preserving
-- reads from degrading into table scans as connected accounts grow.
CREATE INDEX IF NOT EXISTS idx_source_raw_items_source_updated
    ON public.source_raw_items (source_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_extractions_source_archive_updated
    ON public.source_extractions (source_id, archived, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_extractions_source_project_archive_updated
    ON public.source_extractions (source_id, project_key, archived, updated_at DESC);
