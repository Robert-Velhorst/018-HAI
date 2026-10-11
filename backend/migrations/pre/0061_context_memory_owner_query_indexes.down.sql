DROP INDEX IF EXISTS public.idx_context_memories_search_trgm;
DROP INDEX IF EXISTS public.idx_context_memories_owner_kind_active_updated;
DROP INDEX IF EXISTS public.idx_context_memories_owner_project_active_updated;
DROP INDEX IF EXISTS public.idx_context_memories_owner_active_updated;

-- pg_trgm is intentionally retained because another local feature may share it.
