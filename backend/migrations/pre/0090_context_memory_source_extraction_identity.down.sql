DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.context_memories
        WHERE source_extraction_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'source extraction memory provenance exists; refusing to drop source_extraction_id';
    END IF;
END
$$;

DROP INDEX IF EXISTS public.ux_context_memories_owner_source_extraction_kind;

ALTER TABLE public.context_memories
    DROP COLUMN source_extraction_id;
