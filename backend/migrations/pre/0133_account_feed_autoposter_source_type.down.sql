SET LOCAL lock_timeout = '5s';

LOCK TABLE public.account_feeds IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.account_feeds WHERE source_type = 'autoposter_api' LIMIT 1
    ) THEN
        RAISE EXCEPTION 'rollback refused: Autoposter source configuration exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE public.account_feeds
    DROP CONSTRAINT chk_account_feeds_source_type;

ALTER TABLE public.account_feeds
    ADD CONSTRAINT chk_account_feeds_source_type
    CHECK (source_type IN ('local_json_file', 'http_json_feed'));
