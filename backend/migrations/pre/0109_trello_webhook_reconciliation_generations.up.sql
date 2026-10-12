-- ApplyMigrations runs each file in one transaction. These locks prevent a
-- receipt or source-owner change from appearing between preflight and backfill.
-- Match webhook writers, which lock the source row before inserting a receipt.
LOCK TABLE public.connected_sources, public.trello_webhook_receipts
    IN ACCESS EXCLUSIVE MODE;

DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM public.trello_webhook_receipts AS receipts
          JOIN public.connected_sources AS sources ON sources.id = receipts.source_id
         WHERE receipts.status IN ('queued', 'dispatched')
           AND receipts.action_type NOT IN ('commentCard', 'copyCommentCard', 'updateComment', 'deleteComment')
           AND (NULLIF(BTRIM(sources.owner_identity), '') IS NULL
                OR sources.owner_identity !~ '[^[:space:]]')
         LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'migration 0109 refused: pending Trello board webhook receipts require a non-empty connected-source owner_identity';
    END IF;
END
$migration$;

ALTER TABLE public.trello_webhook_receipts
    ADD COLUMN reconciliation_generation bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT ck_trello_webhook_reconciliation_generation
        CHECK (reconciliation_generation >= 0);

CREATE TABLE public.trello_webhook_reconciliation_states (
    source_id uuid PRIMARY KEY REFERENCES public.connected_sources(id) ON DELETE CASCADE,
    owner_identity character varying(255) NOT NULL,
    requested_generation bigint NOT NULL DEFAULT 0,
    completed_generation bigint NOT NULL DEFAULT 0,
    required_trello_generation bigint NOT NULL DEFAULT 1,
    active_generation bigint,
    active_required_trello_generation bigint,
    active_sync_job_id uuid REFERENCES public.source_sync_jobs(id) ON DELETE SET NULL,
    dispatch_attempt integer NOT NULL DEFAULT 0,
    failed_generation bigint,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ck_trello_reconciliation_watermarks
        CHECK (requested_generation >= completed_generation AND completed_generation >= 0),
    CONSTRAINT ck_trello_reconciliation_owner_identity
        CHECK (owner_identity ~ '[^[:space:]]'),
    CONSTRAINT ck_trello_reconciliation_active_generation
        CHECK (active_generation IS NULL OR
               (active_generation > completed_generation AND active_generation <= requested_generation)),
    CONSTRAINT ck_trello_reconciliation_required_trello_generation
        CHECK (required_trello_generation > 0),
    CONSTRAINT ck_trello_reconciliation_active_required_trello_generation
        CHECK ((active_generation IS NULL AND active_required_trello_generation IS NULL) OR
               (active_generation IS NOT NULL AND active_required_trello_generation IS NOT NULL AND
                active_required_trello_generation > 0)),
    CONSTRAINT ck_trello_reconciliation_active_job
        CHECK (active_sync_job_id IS NULL OR active_generation IS NOT NULL),
    CONSTRAINT ck_trello_reconciliation_dispatch_attempt
        CHECK ((active_generation IS NULL AND dispatch_attempt = 0) OR
               (active_generation IS NOT NULL AND dispatch_attempt > 0)),
    CONSTRAINT ck_trello_reconciliation_failed_generation
        CHECK (failed_generation IS NULL OR
               (failed_generation > completed_generation AND failed_generation <= requested_generation))
);

-- Preserve already accepted, unfinished card-change callbacks. Comment events
-- have their own per-card refresh path and do not participate in board syncs.
WITH pending AS (
    SELECT id,
           row_number() OVER (PARTITION BY source_id ORDER BY received_at, id) AS generation
      FROM public.trello_webhook_receipts
     WHERE status IN ('queued', 'dispatched')
       AND action_type NOT IN ('commentCard', 'copyCommentCard', 'updateComment', 'deleteComment')
), assigned AS (
    UPDATE public.trello_webhook_receipts AS receipts
       SET reconciliation_generation = pending.generation
      FROM pending
     WHERE receipts.id = pending.id
    RETURNING receipts.source_id, receipts.reconciliation_generation
)
INSERT INTO public.trello_webhook_reconciliation_states
    (source_id, owner_identity, requested_generation, completed_generation,
     required_trello_generation, dispatch_attempt, updated_at)
SELECT assigned.source_id,
       sources.owner_identity,
       max(assigned.reconciliation_generation),
       0,
       greatest(coalesce(max(sync_states.generation), 0) + 1, 1),
       0,
       now()
  FROM assigned
  JOIN public.connected_sources AS sources ON sources.id = assigned.source_id
  LEFT JOIN public.trello_sync_states AS sync_states ON sync_states.source_id = assigned.source_id
 GROUP BY assigned.source_id, sources.owner_identity;

CREATE INDEX ix_trello_webhook_receipts_reconciliation_pending
    ON public.trello_webhook_receipts (source_id, reconciliation_generation)
    WHERE status IN ('queued', 'dispatched')
      AND action_type NOT IN ('commentCard', 'copyCommentCard', 'updateComment', 'deleteComment');
