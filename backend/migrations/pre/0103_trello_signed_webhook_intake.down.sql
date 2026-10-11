DO $migration$
BEGIN
    IF to_regclass('public.trello_webhook_receipts') IS NULL THEN
        RETURN;
    END IF;

    EXECUTE 'LOCK TABLE public.trello_webhook_receipts IN ACCESS EXCLUSIVE MODE';

    IF EXISTS (
        SELECT 1
          FROM public.trello_webhook_receipts
         LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'rollback refused: Trello webhook receipts contain delivery history';
    END IF;
END
$migration$;

DROP TABLE IF EXISTS public.trello_webhook_receipts;
