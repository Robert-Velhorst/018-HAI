ALTER TABLE public.context_memories
    ADD COLUMN source_extraction_id uuid;

-- Canonical extraction URIs retain their existing backfill behavior. The
-- candidate pass below leaves every colliding owner/extraction/kind identity
-- null so the unique index can be installed without rewriting or dropping rows.
WITH canonical_candidates AS (
    SELECT
        memory.id AS memory_id,
        memory.owner_identity,
        memory.kind,
        substring(
            memory.source_uri FROM '^source-extraction://([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$'
        )::uuid AS extraction_id
    FROM public.context_memories AS memory
    WHERE source_uri ~ '^source-extraction://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
), correction_matches AS (
    SELECT DISTINCT memory.id AS memory_id, memory.owner_identity, memory.kind, extraction.id AS extraction_id
    FROM public.context_memories AS memory
    JOIN public.connected_sources AS source
      ON convert_to(source.owner_identity::text, 'UTF8') = convert_to(memory.owner_identity::text, 'UTF8')
    JOIN public.source_extractions AS extraction
      ON extraction.source_id = source.id
    CROSS JOIN LATERAL regexp_split_to_table(COALESCE(memory.tags, ''), ',') AS tag(value)
    WHERE memory.source_extraction_id IS NULL
      AND memory.owner_identity IS NOT NULL
      AND btrim(memory.owner_identity) <> ''
      AND memory.kind = 'lesson'
      AND memory.source_uri !~ '^source-extraction://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
      AND lower(btrim(tag.value)) = 'source-correction'
      AND memory.source_uri IS NOT NULL
      AND extraction.source_uri IS NOT NULL
      AND convert_to(memory.source_uri::text, 'UTF8') = convert_to(extraction.source_uri::text, 'UTF8')
), correction_candidates AS (
    SELECT memory_id, owner_identity, kind, (array_agg(extraction_id))[1] AS extraction_id
    FROM correction_matches
    GROUP BY memory_id, owner_identity, kind
    HAVING COUNT(*) = 1
), candidate_identities AS (
    SELECT memory_id, owner_identity, kind, extraction_id FROM canonical_candidates
    UNION ALL
    SELECT memory_id, owner_identity, kind, extraction_id FROM correction_candidates
), distinct_candidate_identities AS (
    SELECT DISTINCT memory_id, owner_identity, kind, extraction_id
    FROM candidate_identities
), unambiguous_identity_keys AS (
    SELECT owner_identity, source_extraction_id, kind
    FROM (
        SELECT memory_id, owner_identity, kind, extraction_id AS source_extraction_id
        FROM distinct_candidate_identities
    ) AS candidates
    GROUP BY owner_identity, source_extraction_id, kind
    HAVING COUNT(DISTINCT memory_id) = 1
), safe_candidates AS (
    SELECT candidate.memory_id, candidate.extraction_id
    FROM distinct_candidate_identities AS candidate
    JOIN unambiguous_identity_keys AS safe_key
      ON safe_key.owner_identity IS NOT DISTINCT FROM candidate.owner_identity
     AND safe_key.kind = candidate.kind
     AND safe_key.source_extraction_id = candidate.extraction_id
)
UPDATE public.context_memories AS memory
SET source_extraction_id = substring(
    CASE
        WHEN memory.source_uri ~ '^source-extraction://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            THEN memory.source_uri
        ELSE 'source-extraction://' || safe.extraction_id::text
    END
    FROM '^source-extraction://([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$'
)::uuid
FROM safe_candidates AS safe
WHERE memory.id = safe.memory_id;

CREATE UNIQUE INDEX ux_context_memories_owner_source_extraction_kind
    ON public.context_memories (owner_identity, source_extraction_id, kind)
    WHERE source_extraction_id IS NOT NULL;
