ALTER TABLE public.source_sync_jobs
    ADD COLUMN IF NOT EXISTS progress_phase character varying(40),
    ADD COLUMN IF NOT EXISTS progress_pages integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS progress_records integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS progress_message character varying(512);

-- This duplicate scan gives operators a clear remediation error without
-- blocking source writes for its duration. The unique-index build below is the
-- authoritative race-safe check; if a concurrent write creates a duplicate,
-- the surrounding migration transaction fails and can be retried after review.
DO $$
DECLARE
    duplicate_identity text;
    identity_column_count integer;
BEGIN
    SELECT COUNT(*)
      INTO identity_column_count
      FROM pg_catalog.pg_attribute
     WHERE attrelid = 'public.source_raw_items'::regclass
       AND attname IN ('source_id', 'external_id')
       AND attnum > 0
       AND NOT attisdropped
       AND attnotnull;
    IF identity_column_count <> 2 THEN
        RAISE EXCEPTION 'source_raw_items.source_id and external_id must both exist and be NOT NULL before enabling idempotent Trello sync';
    END IF;

    SELECT source_id::text || '/' || external_id
      INTO duplicate_identity
      FROM public.source_raw_items
     GROUP BY source_id, external_id
    HAVING COUNT(*) > 1
     LIMIT 1;
    IF duplicate_identity IS NOT NULL THEN
        RAISE EXCEPTION 'duplicate source raw item identity %; resolve duplicates without deleting evidence before enabling idempotent Trello sync', duplicate_identity;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS ux_source_raw_items_source_external
    ON public.source_raw_items (source_id, external_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_class index_relation
          JOIN pg_catalog.pg_namespace index_schema
            ON index_schema.oid = index_relation.relnamespace
          JOIN pg_catalog.pg_index index_state
            ON index_state.indexrelid = index_relation.oid
          JOIN pg_catalog.pg_class table_relation
            ON table_relation.oid = index_state.indrelid
          JOIN pg_catalog.pg_am access_method
            ON access_method.oid = index_relation.relam
         WHERE index_schema.nspname = 'public'
           AND index_relation.relname = 'ux_source_raw_items_source_external'
           AND table_relation.oid = 'public.source_raw_items'::regclass
           AND access_method.amname = 'btree'
           AND index_state.indisunique
           AND index_state.indisvalid
           AND index_state.indisready
           AND index_state.indpred IS NULL
           AND index_state.indexprs IS NULL
           AND index_state.indnkeyatts = 2
           AND index_state.indnatts = 2
           AND ARRAY(
                SELECT attribute.attname
                  FROM unnest(index_state.indkey) WITH ORDINALITY AS key_column(attnum, ordinal_position)
                  JOIN pg_catalog.pg_attribute attribute
                    ON attribute.attrelid = index_state.indrelid
                   AND attribute.attnum = key_column.attnum
                 ORDER BY key_column.ordinal_position
           ) = ARRAY['source_id', 'external_id']::name[]
           AND NOT EXISTS (
                SELECT 1
                  FROM pg_catalog.pg_attribute attribute
                 WHERE attribute.attrelid = index_state.indrelid
                   AND attribute.attname IN ('source_id', 'external_id')
                   AND attribute.attnum > 0
                   AND NOT attribute.attisdropped
                   AND NOT attribute.attnotnull
           )
           AND NOT EXISTS (
                SELECT 1
                  FROM unnest(index_state.indclass) WITH ORDINALITY AS operator_class(opclass_oid, ordinal_position)
                  JOIN pg_catalog.pg_opclass opclass
                    ON opclass.oid = operator_class.opclass_oid
                 WHERE NOT opclass.opcdefault
           )
           AND NOT EXISTS (
                SELECT 1
                  FROM unnest(index_state.indcollation) WITH ORDINALITY AS index_collation(collation_oid, ordinal_position)
                  JOIN unnest(index_state.indkey) WITH ORDINALITY AS key_column(attnum, ordinal_position)
                    ON key_column.ordinal_position = index_collation.ordinal_position
                  JOIN pg_catalog.pg_attribute attribute
                    ON attribute.attrelid = index_state.indrelid
                   AND attribute.attnum = key_column.attnum
                 WHERE index_collation.collation_oid <> attribute.attcollation
           )
    ) THEN
        RAISE EXCEPTION 'index public.ux_source_raw_items_source_external is not a valid unique btree index on source_raw_items(source_id, external_id)';
    END IF;
END $$;

CREATE TABLE public.trello_sync_states (
    source_id uuid PRIMARY KEY,
    owner_identity character varying(255) NOT NULL,
    board_id character varying(32) NOT NULL,
    logical_job_id uuid,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    phase character varying(40) NOT NULL,
    card_cursor character varying(24) NOT NULL DEFAULT '',
    action_cursor character varying(24) NOT NULL DEFAULT '',
    action_since timestamp with time zone,
    cycle_started_at timestamp with time zone NOT NULL,
    last_successful_at timestamp with time zone,
    max_card_activity_at timestamp with time zone,
    pages_processed integer NOT NULL DEFAULT 0 CHECK (pages_processed >= 0),
    records_processed bigint NOT NULL DEFAULT 0 CHECK (records_processed >= 0),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT fk_trello_sync_states_source FOREIGN KEY (source_id)
        REFERENCES public.connected_sources(id) ON DELETE CASCADE
);

CREATE INDEX ix_trello_sync_states_logical_job
    ON public.trello_sync_states (logical_job_id)
    WHERE logical_job_id IS NOT NULL;

CREATE TABLE public.trello_sync_pages (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    source_id uuid NOT NULL REFERENCES public.connected_sources(id) ON DELETE CASCADE,
    logical_job_id uuid NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    phase character varying(40) NOT NULL,
    cursor_before character varying(24) NOT NULL DEFAULT '',
    cursor_after character varying(24) NOT NULL DEFAULT '',
    record_count integer NOT NULL DEFAULT 0 CHECK (record_count >= 0),
    request_count integer NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    response_bytes bigint NOT NULL DEFAULT 0 CHECK (response_bytes >= 0),
    fingerprint character(64) NOT NULL,
    committed_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ux_trello_sync_pages_identity UNIQUE
        (source_id, logical_job_id, generation, phase, cursor_before)
);

CREATE INDEX ix_trello_sync_pages_job
    ON public.trello_sync_pages (source_id, logical_job_id, committed_at);

CREATE TABLE public.trello_action_receipts (
    source_id uuid NOT NULL REFERENCES public.connected_sources(id) ON DELETE CASCADE,
    action_id character varying(255) NOT NULL,
    card_id character varying(255) NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    fingerprint character(64) NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (source_id, action_id)
);

CREATE INDEX ix_trello_action_receipts_occurred
    ON public.trello_action_receipts (source_id, occurred_at DESC);
