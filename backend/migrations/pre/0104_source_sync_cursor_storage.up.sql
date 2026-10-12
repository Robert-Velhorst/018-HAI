-- Google provider page and sync tokens are opaque and have no published size
-- limit. Store the encoded durable cursor without imposing an arbitrary 512
-- character truncation boundary. varchar-to-text is binary compatible in
-- PostgreSQL; the lock timeout prevents a delayed schema lock from stalling
-- startup indefinitely.
SET LOCAL lock_timeout = '5s';

ALTER TABLE public.connected_sources
    ALTER COLUMN cursor TYPE text;

ALTER TABLE public.source_sync_jobs
    ALTER COLUMN cursor_before TYPE text,
    ALTER COLUMN cursor_after TYPE text;
