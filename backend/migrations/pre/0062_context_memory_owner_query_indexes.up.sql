-- Authenticated memory views filter by owner, project, archive state, and
-- recency. Keep those reads index-backed as personal memory grows.
CREATE INDEX IF NOT EXISTS idx_context_memories_owner_project_archive_updated
    ON public.context_memories (owner_identity, project_key, archived, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_context_memories_owner_archive_updated
    ON public.context_memories (owner_identity, archived, updated_at DESC);
