CREATE TABLE public.opscontrol_approval_requests (
    id uuid NOT NULL,
    owner_identity text NOT NULL,
    idempotency_key text NOT NULL,
    task_id text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    target text NOT NULL,
    binding_digest text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT opscontrol_approval_requests_pkey PRIMARY KEY (id),
    CONSTRAINT uq_opscontrol_approval_request_owner_id UNIQUE (owner_identity, id),
    CONSTRAINT uq_opscontrol_approval_request_idempotency UNIQUE (owner_identity, idempotency_key),
    CONSTRAINT chk_opscontrol_approval_request_owner CHECK (
        owner_identity = btrim(owner_identity)
        AND char_length(owner_identity) BETWEEN 1 AND 255
        AND created_by = owner_identity
    ),
    CONSTRAINT chk_opscontrol_approval_request_identity CHECK (
        idempotency_key = btrim(idempotency_key)
        AND char_length(idempotency_key) BETWEEN 1 AND 255
        AND task_id = btrim(task_id)
        AND char_length(task_id) BETWEEN 1 AND 255
    ),
    CONSTRAINT chk_opscontrol_approval_request_effect CHECK (
        binding_digest ~ '^[0-9a-f]{64}$'
        AND resource_id = btrim(resource_id)
        AND char_length(resource_id) BETWEEN 1 AND 512
        AND target = btrim(target)
        AND char_length(target) BETWEEN 1 AND 64
        AND (
            (
                action = 'opscontrol.emergency-stop.clear'
                AND resource_type = 'opscontrol-emergency-stop'
                AND resource_id LIKE 'emergency-stop:revision-%'
                AND target = 'disengaged'
            )
            OR (
                action = 'opscontrol.autonomy-mode.escalate'
                AND resource_type = 'opscontrol-autonomy-mode'
                AND resource_id LIKE 'autonomy-mode:%-to-%'
                AND target IN (
                    'read_only', 'draft_only', 'approval_required',
                    'autonomous_safe'
                )
            )
        )
    ),
    CONSTRAINT chk_opscontrol_approval_request_expiry CHECK (
        expires_at > created_at
        AND expires_at <= created_at + interval '5 minutes'
    )
);

CREATE TABLE public.opscontrol_approval_decisions (
    id uuid NOT NULL,
    request_id uuid NOT NULL,
    owner_identity text NOT NULL,
    decision text NOT NULL,
    reason text NOT NULL DEFAULT '',
    actor text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT opscontrol_approval_decisions_pkey PRIMARY KEY (id),
    CONSTRAINT uq_opscontrol_approval_decision_owner_id UNIQUE (owner_identity, id),
    CONSTRAINT uq_opscontrol_approval_decision_request UNIQUE (owner_identity, request_id),
    CONSTRAINT fk_opscontrol_approval_decision_request
        FOREIGN KEY (owner_identity, request_id)
        REFERENCES public.opscontrol_approval_requests (owner_identity, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_opscontrol_approval_decision_value CHECK (
        decision IN ('approved', 'rejected')
        AND actor = owner_identity
        AND reason = btrim(reason)
        AND char_length(reason) <= 2048
    )
);

CREATE INDEX idx_opscontrol_approval_requests_owner_created
    ON public.opscontrol_approval_requests (owner_identity, created_at DESC);
CREATE INDEX idx_opscontrol_approval_requests_binding
    ON public.opscontrol_approval_requests (owner_identity, binding_digest, expires_at DESC);
CREATE INDEX idx_opscontrol_approval_decisions_owner_created
    ON public.opscontrol_approval_decisions (owner_identity, created_at DESC);

CREATE OR REPLACE FUNCTION public.hai_enforce_opscontrol_approval_decision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.opscontrol_approval_requests request
        WHERE request.id = NEW.request_id
          AND request.owner_identity = NEW.owner_identity
          AND request.created_by = NEW.actor
          AND NEW.created_at >= request.created_at
          AND NEW.created_at <= request.expires_at
    ) THEN
        RAISE EXCEPTION 'opscontrol decision does not match a live owner request'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_opscontrol_approval_decisions_binding
    BEFORE INSERT ON public.opscontrol_approval_decisions
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_enforce_opscontrol_approval_decision();

CREATE TRIGGER trg_opscontrol_approval_requests_immutable
    BEFORE UPDATE OR DELETE ON public.opscontrol_approval_requests
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();
CREATE TRIGGER trg_opscontrol_approval_requests_no_truncate
    BEFORE TRUNCATE ON public.opscontrol_approval_requests
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();
CREATE TRIGGER trg_opscontrol_approval_decisions_immutable
    BEFORE UPDATE OR DELETE ON public.opscontrol_approval_decisions
    FOR EACH ROW
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();
CREATE TRIGGER trg_opscontrol_approval_decisions_no_truncate
    BEFORE TRUNCATE ON public.opscontrol_approval_decisions
    FOR EACH STATEMENT
    EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();

ALTER TABLE public.execution_authorization_receipts
    ADD COLUMN control_decision_id uuid;

ALTER TABLE public.execution_authorization_receipts
    DROP CONSTRAINT chk_execution_authorization_receipt_approval_binding,
    DROP CONSTRAINT chk_execution_authorization_receipt_evidence_refs;

ALTER TABLE public.execution_authorization_receipts
    ADD CONSTRAINT fk_execution_authorization_receipt_control_decision
        FOREIGN KEY (owner_identity, control_decision_id)
        REFERENCES public.opscontrol_approval_decisions (owner_identity, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    ADD CONSTRAINT chk_execution_authorization_receipt_approval_binding CHECK (
        (
            approval_source_id = ''
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NULL
            AND control_decision_id IS NULL
        )
        OR (
            task_review_decision_id IS NOT NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NULL
            AND control_decision_id IS NULL
            AND approval_source_id LIKE 'task-review:%'
        )
        OR (
            approval_source_id = 'workflow-decision:' || workflow_decision_id::text
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NOT NULL
            AND portfolio_proposal_decision_id IS NULL
            AND control_decision_id IS NULL
        )
        OR (
            approval_source_id = 'portfolio-decision:' || portfolio_proposal_decision_id::text
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NOT NULL
            AND control_decision_id IS NULL
        )
        OR (
            approval_source_id = 'control-decision:' || control_decision_id::text
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NULL
            AND control_decision_id IS NOT NULL
        )
    ),
    ADD CONSTRAINT chk_execution_authorization_receipt_evidence_refs CHECK (
        (
            (
                COALESCE(evidence_json #>> '{constitution,source}', '') LIKE 'builtin-%'
                AND constitution_id IS NULL
                AND constitution_version IS NULL
                AND constitution_digest IS NULL
            )
            OR (
                COALESCE(evidence_json #>> '{constitution,source}', '') NOT LIKE 'builtin-%'
                AND COALESCE(evidence_json #>> '{constitution,id}', '') = COALESCE(constitution_id::text, '')
                AND COALESCE((evidence_json #>> '{constitution,version}')::integer, 0) = COALESCE(constitution_version, 0)
                AND COALESCE(evidence_json #>> '{constitution,digest}', '') = COALESCE(constitution_digest, '')
            )
        )
        AND COALESCE(evidence_json #>> '{mandate,id}', '') = COALESCE(mandate_id::text, '')
        AND COALESCE(evidence_json #>> '{mandate,decisionId}', '') = COALESCE(mandate_decision_id::text, '')
        AND COALESCE(evidence_json #>> '{agent,agentId}', '') = COALESCE(agent_id, '')
        AND COALESCE(evidence_json #>> '{agent,assignmentId}', '') = COALESCE(assignment_id, '')
        AND (
            (
                approval_source_id = ''
                AND task_review_decision_id IS NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NULL
                AND control_decision_id IS NULL
                AND COALESCE(evidence_json #>> '{approval,sourceId}', '') = ''
                AND COALESCE(evidence_json #>> '{approval,decisionId}', '') = ''
            )
            OR (
                task_review_decision_id IS NOT NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NULL
                AND control_decision_id IS NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = task_review_decision_id::text
            )
            OR (
                task_review_decision_id IS NULL
                AND workflow_decision_id IS NOT NULL
                AND portfolio_proposal_decision_id IS NULL
                AND control_decision_id IS NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = workflow_decision_id::text
            )
            OR (
                task_review_decision_id IS NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NOT NULL
                AND control_decision_id IS NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = portfolio_proposal_decision_id::text
            )
            OR (
                task_review_decision_id IS NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NULL
                AND control_decision_id IS NOT NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = control_decision_id::text
            )
        )
    );

CREATE INDEX idx_execution_authorization_receipts_control_decision
    ON public.execution_authorization_receipts
    (owner_identity, control_decision_id, evaluated_at DESC)
    WHERE control_decision_id IS NOT NULL;

CREATE UNIQUE INDEX uq_execution_authorization_control_decision_once
    ON public.execution_authorization_receipts (owner_identity, control_decision_id)
    WHERE control_decision_id IS NOT NULL AND outcome = 'authorized';
