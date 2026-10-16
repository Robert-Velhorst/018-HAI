-- Serialize sync writes and reject rollback while a checkpoint or claim is live.
LOCK TABLE public.account_feeds IN ACCESS EXCLUSIVE MODE;

-- Replay of a bounded first page may not reach older feed records; reset or export state explicitly.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.account_feeds WHERE sync_cursor <> '' OR sync_token IS NOT NULL LIMIT 1
    ) THEN
        RAISE EXCEPTION 'rollback refused: account feed cursor or active sync claim exists; preserve state before rollback'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE public.account_feeds
    DROP CONSTRAINT chk_account_feeds_sync_cursor_length,
    DROP COLUMN sync_cursor;
