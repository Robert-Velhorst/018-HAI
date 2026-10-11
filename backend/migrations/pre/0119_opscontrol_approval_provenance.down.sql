DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.execution_authorization_receipts
        WHERE control_decision_id IS NOT NULL
           OR approval_source_id LIKE 'control-decision:%'
    ) THEN
        RAISE EXCEPTION 'cannot remove opscontrol approval provenance while authorization receipts reference it';
    END IF;
END;
$$;

DROP INDEX IF EXISTS public.uq_execution_authorization_control_decision_once;
DROP INDEX IF EXISTS public.idx_execution_authorization_receipts_control_decision;

ALTER TABLE public.execution_authorization_receipts
    DROP CONSTRAINT chk_execution_authorization_receipt_approval_binding,
    DROP CONSTRAINT chk_execution_authorization_receipt_evidence_refs,
    DROP CONSTRAINT fk_execution_authorization_receipt_control_decision,
    DROP COLUMN control_decision_id;

ALTER TABLE public.execution_authorization_receipts
    ADD CONSTRAINT chk_execution_authorization_receipt_approval_binding CHECK (
        (
            approval_source_id = ''
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NULL
        )
        OR (
            task_review_decision_id IS NOT NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NULL
            AND approval_source_id LIKE 'task-review:%'
        )
        OR (
            approval_source_id = 'workflow-decision:' || workflow_decision_id::text
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NOT NULL
            AND portfolio_proposal_decision_id IS NULL
        )
        OR (
            approval_source_id = 'portfolio-decision:' || portfolio_proposal_decision_id::text
            AND task_review_decision_id IS NULL
            AND workflow_decision_id IS NULL
            AND portfolio_proposal_decision_id IS NOT NULL
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
                AND COALESCE(evidence_json #>> '{approval,sourceId}', '') = ''
                AND COALESCE(evidence_json #>> '{approval,decisionId}', '') = ''
            )
            OR (
                task_review_decision_id IS NOT NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = task_review_decision_id::text
            )
            OR (
                task_review_decision_id IS NULL
                AND workflow_decision_id IS NOT NULL
                AND portfolio_proposal_decision_id IS NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = workflow_decision_id::text
            )
            OR (
                task_review_decision_id IS NULL
                AND workflow_decision_id IS NULL
                AND portfolio_proposal_decision_id IS NOT NULL
                AND evidence_json #>> '{approval,sourceId}' = approval_source_id
                AND evidence_json #>> '{approval,decisionId}' = portfolio_proposal_decision_id::text
            )
        )
    );

DROP TRIGGER IF EXISTS trg_opscontrol_approval_decisions_no_truncate
    ON public.opscontrol_approval_decisions;
DROP TRIGGER IF EXISTS trg_opscontrol_approval_decisions_immutable
    ON public.opscontrol_approval_decisions;
DROP TRIGGER IF EXISTS trg_opscontrol_approval_requests_no_truncate
    ON public.opscontrol_approval_requests;
DROP TRIGGER IF EXISTS trg_opscontrol_approval_requests_immutable
    ON public.opscontrol_approval_requests;
DROP TRIGGER IF EXISTS trg_opscontrol_approval_decisions_binding
    ON public.opscontrol_approval_decisions;
DROP FUNCTION IF EXISTS public.hai_enforce_opscontrol_approval_decision();
DROP TABLE public.opscontrol_approval_decisions;
DROP TABLE public.opscontrol_approval_requests;
