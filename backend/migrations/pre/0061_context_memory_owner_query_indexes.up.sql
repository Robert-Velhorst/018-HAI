CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_context_memories_owner_active_updated
    ON public.context_memories (
        owner_identity,
        archived,
        updated_at DESC,
        created_at DESC,
        id DESC
    );

CREATE INDEX IF NOT EXISTS idx_context_memories_owner_project_active_updated
    ON public.context_memories (
        owner_identity,
        project_key,
        archived,
        updated_at DESC,
        created_at DESC,
        id DESC
    );

CREATE INDEX IF NOT EXISTS idx_context_memories_owner_kind_active_updated
    ON public.context_memories (
        owner_identity,
        LOWER(kind),
        archived,
        updated_at DESC,
        id DESC
    );

CREATE INDEX IF NOT EXISTS idx_context_memories_search_trgm
    ON public.context_memories
    USING gin ((
        LOWER(
            COALESCE(content, '') || ' ' ||
            COALESCE(summary, '') || ' ' ||
            COALESCE(tags, '')
        )
    ) gin_trgm_ops);
