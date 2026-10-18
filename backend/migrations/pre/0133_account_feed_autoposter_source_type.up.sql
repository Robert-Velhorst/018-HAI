ALTER TABLE public.account_feeds
    DROP CONSTRAINT chk_account_feeds_source_type;

ALTER TABLE public.account_feeds
    ADD CONSTRAINT chk_account_feeds_source_type
    CHECK (source_type IN ('local_json_file', 'http_json_feed', 'autoposter_api'));
