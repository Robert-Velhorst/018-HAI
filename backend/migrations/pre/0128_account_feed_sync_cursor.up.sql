-- Persist the opaque next-page cursor for HTTP JSON account feeds.
-- The checkpoint is private operational state and is never returned in feed configuration JSON.
ALTER TABLE public.account_feeds
    ADD COLUMN sync_cursor text NOT NULL DEFAULT '',
    ADD CONSTRAINT chk_account_feeds_sync_cursor_length
        CHECK (octet_length(sync_cursor) <= 4096);
