DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.connected_sources
         WHERE length(cursor) > 512
    ) OR EXISTS (
        SELECT 1 FROM public.source_sync_jobs
         WHERE length(cursor_before) > 512 OR length(cursor_after) > 512
    ) THEN
        RAISE EXCEPTION 'cannot restore 512-character source cursor columns while longer cursor data exists';
    END IF;
END $$;

SET LOCAL lock_timeout = '5s';

ALTER TABLE public.connected_sources
    ALTER COLUMN cursor TYPE character varying(512);

ALTER TABLE public.source_sync_jobs
    ALTER COLUMN cursor_before TYPE character varying(512),
    ALTER COLUMN cursor_after TYPE character varying(512);
