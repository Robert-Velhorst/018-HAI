CREATE TABLE public.automation_approval_proof_issuances (
    contract_version character varying(64) NOT NULL,
    owner_identity character varying(256) NOT NULL,
    approval_source_id character varying(256) NOT NULL,
    proof_id uuid NOT NULL,
    automation_id uuid NOT NULL,
    action_digest character(64) NOT NULL,
    scope character varying(64) NOT NULL,
    nonce_digest character(64) NOT NULL,
    signature_digest character(64) NOT NULL,
    record_digest character(64) NOT NULL,
    issued_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT automation_approval_proof_issuances_pkey
        PRIMARY KEY (owner_identity, approval_source_id),
    CONSTRAINT automation_approval_proof_issuances_proof_key
        UNIQUE (owner_identity, proof_id),
    CONSTRAINT chk_automation_approval_proof_issuance_contract CHECK (
        contract_version = 'automation-approval-proof-issuance.v1'
    ),
    CONSTRAINT chk_automation_approval_proof_issuance_owner CHECK (
        length(btrim(owner_identity)) BETWEEN 1 AND 256
        AND owner_identity !~ E'[\\r\\n\\x00]'
    ),
    CONSTRAINT chk_automation_approval_proof_issuance_digests CHECK (
        action_digest ~ '^[0-9a-f]{64}$'
        AND nonce_digest ~ '^[0-9a-f]{64}$'
        AND signature_digest ~ '^[0-9a-f]{64}$'
        AND record_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_automation_approval_proof_issuance_scope CHECK (
        scope IN (
            'automation.api.read',
            'automation.api.mutate',
            'automation.script.execute',
            'automation.docker.start',
            'automation.agent-runtime.execute'
        )
    ),
    CONSTRAINT chk_automation_approval_proof_issuance_source CHECK (
        length(btrim(approval_source_id)) BETWEEN 1 AND 256
        AND approval_source_id !~ E'[\\r\\n\\x00]'
        AND (
            approval_source_id LIKE 'task-review:%'
            OR approval_source_id LIKE 'workflow-decision:%'
        )
    ),
    CONSTRAINT chk_automation_approval_proof_issuance_time CHECK (
        expires_at > issued_at
        AND expires_at <= issued_at + interval '15 minutes'
    )
);

CREATE TRIGGER trg_automation_approval_proof_issuances_immutable
BEFORE UPDATE OR DELETE ON public.automation_approval_proof_issuances
FOR EACH ROW EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();

CREATE TRIGGER trg_automation_approval_proof_issuances_no_truncate
BEFORE TRUNCATE ON public.automation_approval_proof_issuances
FOR EACH STATEMENT EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation();
