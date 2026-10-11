package migrations

import (
	"regexp"
	"strings"
	"testing"
)

func TestUnifiedExecutionAuthorizationMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0014_unified_execution_authorization.up.sql")
	if err != nil {
		t.Fatalf("read unified execution authorization up migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0014_unified_execution_authorization.down.sql")
	if err != nil {
		t.Fatalf("read unified execution authorization down migration: %v", err)
	}

	up := normalizeMigrationLineEndings(string(upBytes))
	for _, fragment := range []string{
		"CREATE TABLE public.execution_authorization_receipts",
		"CREATE TABLE public.execution_authorization_consumptions",
		"CREATE TABLE public.execution_authorization_final_effect_exercises",
		"contract_version integer NOT NULL",
		"resource_type character varying(256) NOT NULL",
		"resource_id character varying(256) NOT NULL",
		"project_key character varying(256) NOT NULL",
		"runtime_id character varying(256) NOT NULL",
		"approval_source_id character varying(256) NOT NULL",
		"effect_digest character(64) NOT NULL",
		"required_authority smallint NOT NULL",
		"requested_autonomy smallint NOT NULL",
		"effective_autonomy smallint NOT NULL",
		"estimated_cost_eur double precision NOT NULL",
		"notification_required boolean NOT NULL",
		"consumer character varying(256) NOT NULL",
		"UNIQUE (owner_identity, idempotency_key)",
		"PRIMARY KEY (owner_identity, receipt_id)",
		"FOREIGN KEY (owner_identity, constitution_id, constitution_version)",
		"FOREIGN KEY (owner_identity, mandate_id)",
		"FOREIGN KEY (owner_identity, mandate_decision_id, mandate_id)",
		"FOREIGN KEY (owner_identity, agent_id)",
		"FOREIGN KEY (owner_identity, assignment_id, agent_id)",
		"UNIQUE (owner_identity, id, approval_source_id)",
		"task_review_decision_id,\n            approval_source_id",
		"REFERENCES public.task_review_decisions (\n            owner_identity,\n            id,\n            approval_source_id",
		"FOREIGN KEY (owner_identity, workflow_decision_id)",
		"FOREIGN KEY (owner_identity, receipt_id, receipt_digest)",
		"FOREIGN KEY (owner_identity, receipt_id, effect_digest)",
		"FOREIGN KEY (owner_identity, workflow_id)",
		"(owner_identity, id, decision_digest)",
		"request_digest ~ '^[0-9a-f]{64}$'",
		"decision_digest ~ '^[0-9a-f]{64}$'",
		"effect_digest ~ '^[0-9a-f]{64}$'",
		"receipt_digest ~ '^[0-9a-f]{64}$'",
		"constitution_digest ~ '^[0-9a-f]{64}$'",
		"requested_autonomy BETWEEN 0 AND 10",
		"effective_autonomy BETWEEN 0 AND 10",
		"risk IN ('low', 'medium', 'high', 'critical')",
		"octet_length(evidence_json::text) BETWEEN 2 AND 65536",
		"evidence_json ?& ARRAY[",
		"LIKE 'builtin-%'",
		"COALESCE(evidence_json #>> '{constitution,id}', '')",
		"evidence_json #>> '{approval,sourceId}' =\n                    approval_source_id",
		"evidence_json #>> '{approval,decisionId}' =\n                    task_review_decision_id::text",
		"evidence_json #>> '{approval,decisionId}' =\n                    workflow_decision_id::text",
		"approval_source_id LIKE 'task-review:%'",
		"'workflow-decision:' || workflow_decision_id::text",
		"trg_workflow_decisions_bind_owner",
		"trg_execution_authorization_receipts_immutable",
		"trg_execution_authorization_receipts_no_truncate",
		"trg_execution_authorization_consumptions_immutable",
		"trg_execution_authorization_consumptions_no_truncate",
		"trg_execution_authorization_final_effects_immutable",
		"trg_execution_authorization_final_effects_no_truncate",
		"execution authorization ledgers are append-only",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration is missing contract fragment %q", fragment)
		}
	}
	for _, obsolete := range []string{
		"consumer_kind",
		"payload_json",
		"authority_level",
		"autonomy_level",
		"approval_id uuid",
		"approval_decision_id uuid",
		"'task-review:' || task_review_decision_id::text",
	} {
		if strings.Contains(up, obsolete) {
			t.Errorf("up migration retained obsolete field %q", obsolete)
		}
	}

	down := normalizeMigrationLineEndings(string(downBytes))
	for _, fragment := range []string{
		"DROP TRIGGER IF EXISTS trg_execution_authorization_final_effects_no_truncate",
		"DROP TRIGGER IF EXISTS trg_execution_authorization_final_effects_immutable",
		"DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_no_truncate",
		"DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_immutable",
		"DROP TRIGGER IF EXISTS trg_execution_authorization_receipts_no_truncate",
		"DROP TRIGGER IF EXISTS trg_execution_authorization_receipts_immutable",
		"DROP FUNCTION IF EXISTS public.hai_reject_execution_authorization_mutation()",
		"DROP TABLE IF EXISTS public.execution_authorization_final_effect_exercises",
		"DROP TABLE IF EXISTS public.execution_authorization_consumptions",
		"DROP TABLE IF EXISTS public.execution_authorization_receipts",
		"DROP TRIGGER IF EXISTS trg_workflow_decisions_bind_owner",
		"DROP FUNCTION IF EXISTS public.hai_bind_workflow_decision_owner()",
		"DROP CONSTRAINT IF EXISTS uq_workflow_decision_owner_id",
		"DROP CONSTRAINT IF EXISTS uq_workflow_item_owner_id",
		"DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id_source",
		"DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id",
		"DROP CONSTRAINT IF EXISTS uq_standing_mandate_decision_owner_id_mandate",
		"DROP CONSTRAINT IF EXISTS uq_robert_constitution_owner_id_version",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("down migration is missing contract fragment %q", fragment)
		}
	}
}

func TestUnifiedExecutionAuthorizationApprovalProvenanceIsOwnerScopedAndExclusive(
	t *testing.T,
) {
	up := readUnifiedExecutionAuthorizationMigration(
		t,
		"pre/0014_unified_execution_authorization.up.sql",
	)

	for _, column := range []string{
		"task_review_decision_id",
		"workflow_decision_id",
	} {
		pattern := regexp.MustCompile(
			`(?m)^\s*` + column + ` uuid,\s*$`,
		)
		if !pattern.MatchString(up) {
			t.Errorf("%s must be a separately nullable UUID column", column)
		}
	}

	requireMigrationFragments(t, up, []string{
		"CONSTRAINT fk_execution_authorization_receipt_task_review",
		"FOREIGN KEY (\n            owner_identity,\n            task_review_decision_id,\n            approval_source_id\n        )",
		"REFERENCES public.task_review_decisions (\n            owner_identity,\n            id,\n            approval_source_id\n        )",
		"CONSTRAINT fk_execution_authorization_receipt_workflow_decision",
		"FOREIGN KEY (owner_identity, workflow_decision_id)",
		"REFERENCES public.workflow_decisions (owner_identity, id)",
		"approval_source_id = ''\n            AND task_review_decision_id IS NULL\n            AND workflow_decision_id IS NULL",
		"task_review_decision_id IS NOT NULL\n            AND workflow_decision_id IS NULL\n            AND approval_source_id LIKE 'task-review:%'",
		"approval_source_id =\n                'workflow-decision:' || workflow_decision_id::text",
	})

	if count := strings.Count(
		up,
		"CONSTRAINT chk_execution_authorization_receipt_approval_binding",
	); count != 1 {
		t.Fatalf("approval exclusivity constraint count = %d, want 1", count)
	}
}

func TestUnifiedExecutionAuthorizationEvidenceBindsExactSourceAndDecisionIDs(
	t *testing.T,
) {
	up := readUnifiedExecutionAuthorizationMigration(
		t,
		"pre/0014_unified_execution_authorization.up.sql",
	)
	requireMigrationFragments(t, up, []string{
		"evidence_json #>> '{approval,sourceId}' =\n                    approval_source_id",
		"evidence_json #>> '{approval,decisionId}' =\n                    task_review_decision_id::text",
		"evidence_json #>> '{approval,decisionId}' =\n                    workflow_decision_id::text",
		"COALESCE(\n                    evidence_json #>> '{approval,sourceId}',\n                    ''\n                ) = ''",
		"COALESCE(\n                    evidence_json #>> '{approval,decisionId}',\n                    ''\n                ) = ''",
	})

	if strings.Contains(
		up,
		"COALESCE(\n                task_review_decision_id::text,\n                workflow_decision_id::text",
	) {
		t.Fatal("approval evidence still uses ambiguous coalesced decision provenance")
	}
}

func TestUnifiedExecutionAuthorizationRollbackIsDependencyOrderedAndComplete(
	t *testing.T,
) {
	down := readUnifiedExecutionAuthorizationMigration(
		t,
		"pre/0014_unified_execution_authorization.down.sql",
	)
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("rollback must not use CASCADE")
	}

	requireMigrationOrder(t, down,
		"DROP TABLE IF EXISTS public.execution_authorization_final_effect_exercises",
		"DROP TABLE IF EXISTS public.execution_authorization_consumptions",
		"DROP TABLE IF EXISTS public.execution_authorization_receipts",
		"DROP TRIGGER IF EXISTS trg_workflow_decisions_bind_owner",
		"DROP FUNCTION IF EXISTS public.hai_bind_workflow_decision_owner()",
		"DROP CONSTRAINT IF EXISTS fk_workflow_decision_owner_workflow",
		"DROP COLUMN IF EXISTS owner_identity",
		"DROP CONSTRAINT IF EXISTS uq_workflow_item_owner_id",
		"DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id_source",
		"DROP CONSTRAINT IF EXISTS uq_task_review_decision_owner_id;",
	)
}

func readUnifiedExecutionAuthorizationMigration(t *testing.T, path string) string {
	t.Helper()
	data, err := Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return normalizeMigrationLineEndings(string(data))
}

func requireMigrationFragments(t *testing.T, text string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("migration is missing contract fragment %q", fragment)
		}
	}
}

func requireMigrationOrder(t *testing.T, text string, fragments ...string) {
	t.Helper()
	last := -1
	for _, fragment := range fragments {
		index := strings.Index(text, fragment)
		if index < 0 {
			t.Fatalf("migration is missing ordered fragment %q", fragment)
		}
		if index <= last {
			t.Fatalf("fragment %q is not in dependency-safe order", fragment)
		}
		last = index
	}
}
