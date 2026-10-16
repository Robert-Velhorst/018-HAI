-- Serialize sync writes before checking that no live checkpoint would be lost.
LOCK TABLE public.account_feeds IN ACCESS EXCLUSIVE MODE;

-- Refuse to discard a live checkpoint: replaying a bounded first page may not
-- reach older feed records. Reset or export state explicitly before rollback.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.account_feeds WHERE sync_cursor <> '' LIMIT 1
    ) THEN
        RAISE EXCEPTION 'rollback refused: account feed cursor exists; preserve or explicitly reset the checkpoint first'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE public.account_feeds
    DROP CONSTRAINT chk_account_feeds_sync_cursor_length,
    DROP COLUMN sync_cursor;
