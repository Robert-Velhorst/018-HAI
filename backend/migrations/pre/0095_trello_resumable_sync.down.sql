LOCK TABLE public.trello_sync_states,
           public.trello_sync_pages,
           public.trello_action_receipts,
           public.source_sync_jobs
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.trello_sync_states LIMIT 1)
       OR EXISTS (SELECT 1 FROM public.trello_sync_pages LIMIT 1)
       OR EXISTS (SELECT 1 FROM public.trello_action_receipts LIMIT 1)
       OR EXISTS (
            SELECT 1
              FROM public.source_sync_jobs
             WHERE progress_phase IS NOT NULL
                OR progress_pages IS DISTINCT FROM 0
                OR progress_records IS DISTINCT FROM 0
                OR progress_message IS NOT NULL
             LIMIT 1
       ) THEN
        RAISE EXCEPTION
            'cannot roll back Trello resumability while sync state, action receipts, or progress data exists';
    END IF;
END
$$;

DROP TABLE IF EXISTS public.trello_action_receipts;
DROP TABLE IF EXISTS public.trello_sync_pages;
DROP TABLE IF EXISTS public.trello_sync_states;

-- Keep public.ux_source_raw_items_source_external on rollback: it is a shared
-- source-item idempotency invariant and 0095 may have reused a pre-existing index.

ALTER TABLE public.source_sync_jobs
    DROP COLUMN IF EXISTS progress_message,
    DROP COLUMN IF EXISTS progress_records,
    DROP COLUMN IF EXISTS progress_pages,
    DROP COLUMN IF EXISTS progress_phase;
