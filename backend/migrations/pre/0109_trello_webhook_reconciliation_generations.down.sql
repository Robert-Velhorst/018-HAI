DO $migration$
BEGIN
    IF to_regclass('public.trello_webhook_receipts') IS NULL OR
       to_regclass('public.trello_webhook_reconciliation_states') IS NULL THEN
        RAISE EXCEPTION 'rollback refused: Trello reconciliation schema is incomplete';
    END IF;

    EXECUTE 'LOCK TABLE public.trello_webhook_receipts, public.trello_webhook_reconciliation_states IN ACCESS EXCLUSIVE MODE';

    IF EXISTS (
        SELECT 1
          FROM public.trello_webhook_receipts
         WHERE reconciliation_generation > 0
         LIMIT 1
    ) OR EXISTS (
        SELECT 1
          FROM public.trello_webhook_reconciliation_states
         WHERE requested_generation > 0
            OR completed_generation > 0
            OR required_trello_generation > 1
            OR active_generation IS NOT NULL
            OR active_required_trello_generation IS NOT NULL
            OR failed_generation IS NOT NULL
         LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'rollback refused: Trello reconciliation generation history or pending work would be lost';
    END IF;
END
$migration$;

DROP TABLE public.trello_webhook_reconciliation_states;
DROP INDEX public.ix_trello_webhook_receipts_reconciliation_pending;
ALTER TABLE public.trello_webhook_receipts
    DROP CONSTRAINT ck_trello_webhook_reconciliation_generation,
    DROP COLUMN reconciliation_generation;
