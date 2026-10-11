DO $$
BEGIN
    IF EXISTS (
        SELECT receipts.owner_identity, receipts.task_review_decision_id
        FROM public.execution_authorization_consumptions AS consumptions
        JOIN public.execution_authorization_receipts AS receipts
          ON receipts.owner_identity = consumptions.owner_identity
         AND receipts.id = consumptions.receipt_id
         AND receipts.decision_digest = consumptions.receipt_digest
        WHERE receipts.task_review_decision_id IS NOT NULL
        GROUP BY receipts.owner_identity, receipts.task_review_decision_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION
            'task-review approval decisions already authorize multiple consumptions; resolve duplicate execution history before enabling one-shot claims';
    END IF;
END $$;

CREATE TABLE public.execution_authorization_task_review_claims (
    owner_identity character varying(256) NOT NULL,
    task_review_decision_id uuid NOT NULL,
    receipt_id uuid NOT NULL,
    claimed_at timestamp with time zone NOT NULL,
    CONSTRAINT execution_authorization_task_review_claims_pkey
        PRIMARY KEY (owner_identity, receipt_id),
    CONSTRAINT uq_execution_authorization_task_review_claim_decision
        UNIQUE (owner_identity, task_review_decision_id),
    CONSTRAINT fk_execution_authorization_task_review_claim_decision
        FOREIGN KEY (owner_identity, task_review_decision_id)
        REFERENCES public.task_review_decisions (owner_identity, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_execution_authorization_task_review_claim_consumption
        FOREIGN KEY (owner_identity, receipt_id)
        REFERENCES public.execution_authorization_consumptions (owner_identity, receipt_id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_execution_authorization_task_review_claim_decision
        CHECK (task_review_decision_id <> '00000000-0000-0000-0000-000000000000'::uuid)
);

INSERT INTO public.execution_authorization_task_review_claims (
    owner_identity, task_review_decision_id, receipt_id, claimed_at
)
SELECT
    receipts.owner_identity,
    receipts.task_review_decision_id,
    consumptions.receipt_id,
    consumptions.consumed_at
FROM public.execution_authorization_consumptions AS consumptions
JOIN public.execution_authorization_receipts AS receipts
  ON receipts.owner_identity = consumptions.owner_identity
 AND receipts.id = consumptions.receipt_id
 AND receipts.decision_digest = consumptions.receipt_digest
WHERE receipts.task_review_decision_id IS NOT NULL;

CREATE TRIGGER trg_execution_authorization_task_review_claims_immutable
    BEFORE UPDATE OR DELETE
    ON public.execution_authorization_task_review_claims
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();

CREATE TRIGGER trg_execution_authorization_task_review_claims_no_truncate
    BEFORE TRUNCATE
    ON public.execution_authorization_task_review_claims
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();

CREATE OR REPLACE FUNCTION public.hai_claim_execution_authorization_task_review_consumption()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    decision_id uuid;
BEGIN
    SELECT receipts.task_review_decision_id
      INTO decision_id
      FROM public.execution_authorization_receipts AS receipts
     WHERE receipts.owner_identity = NEW.owner_identity
       AND receipts.id = NEW.receipt_id
       AND receipts.decision_digest = NEW.receipt_digest;

    IF decision_id IS NOT NULL THEN
        INSERT INTO public.execution_authorization_task_review_claims (
            owner_identity, task_review_decision_id, receipt_id, claimed_at
        ) VALUES (
            NEW.owner_identity, decision_id, NEW.receipt_id, NEW.consumed_at
        );
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_execution_authorization_consumptions_claim_task_review
    AFTER INSERT
    ON public.execution_authorization_consumptions
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_claim_execution_authorization_task_review_consumption();
