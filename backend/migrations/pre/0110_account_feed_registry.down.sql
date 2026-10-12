-- AUDIT-DESTROYING OPERATOR ROLLBACK: this permanently removes all account feed
-- audit history, feed configuration, and sync observations. Explicit operator
-- approval and a verified backup/export would be required before execution;
-- automatic rollback cannot establish either condition. Therefore refuse while
-- either table contains data. Stop account-feed writers before rollback.
-- Remove dependents before their function and tables; do not use CASCADE.
SET LOCAL lock_timeout = '5s';

-- Lock child before parent to match FK insert lock order. The runner holds this
-- transaction through preflight and DDL so no feed or audit row can race in.
LOCK TABLE public.account_feed_audits, public.account_feeds
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.account_feed_audits LIMIT 1)
       OR EXISTS (SELECT 1 FROM public.account_feeds LIMIT 1) THEN
        RAISE EXCEPTION 'rollback refused: account feed configuration or audit history exists'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_account_feed_audits_no_truncate
    ON public.account_feed_audits;
DROP TRIGGER IF EXISTS trg_account_feed_audits_immutable
    ON public.account_feed_audits;
DROP FUNCTION IF EXISTS public.hai_reject_account_feed_audit_mutation();

DROP TABLE IF EXISTS public.account_feed_audits;
DROP TABLE IF EXISTS public.account_feeds;
