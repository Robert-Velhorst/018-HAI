package migrations

import (
	"strings"
	"testing"
)

func TestWorkflowDecisionClaimMigrationIsOwnerScopedAtomicAndAppendOnly(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0099_execution_authorization_workflow_decision_claim.up.sql")
	if err != nil {
		t.Fatalf("read workflow decision claim migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0099_execution_authorization_workflow_decision_claim.down.sql")
	if err != nil {
		t.Fatalf("read workflow decision claim rollback: %v", err)
	}
	up := normalizeMigrationLineEndings(string(upBytes))
	for _, required := range []string{
		"CREATE TABLE public.execution_authorization_workflow_decision_claims",
		"PRIMARY KEY (owner_identity, receipt_id)",
		"UNIQUE (owner_identity, workflow_decision_id)",
		"FOREIGN KEY (owner_identity, workflow_decision_id)",
		"REFERENCES public.workflow_decisions (owner_identity, id)",
		"FOREIGN KEY (owner_identity, receipt_id)",
		"REFERENCES public.execution_authorization_consumptions (owner_identity, receipt_id)",
		"GROUP BY receipts.owner_identity, receipts.workflow_decision_id",
		"HAVING count(*) > 1",
		"INSERT INTO public.execution_authorization_workflow_decision_claims",
		"CREATE TRIGGER trg_execution_authorization_consumptions_claim_workflow_decision",
		"AFTER INSERT",
		"IF receipt_outcome = 'authorized' AND decision_id IS NOT NULL",
		"EXECUTE FUNCTION public.hai_reject_execution_authorization_mutation()",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("workflow decision claim migration is missing contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"DROP TRIGGER IF EXISTS trg_execution_authorization_consumptions_claim_task_review",
		"DROP TABLE public.execution_authorization_task_review_claims",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("workflow decision claim migration changes task-review claim behavior: %q", forbidden)
		}
	}

	down := normalizeMigrationLineEndings(string(downBytes))
	guard := strings.Index(down, "cannot roll back workflow-decision approval claims after execution")
	dropTable := strings.Index(down, "DROP TABLE public.execution_authorization_workflow_decision_claims")
	if guard < 0 || dropTable < 0 || guard > dropTable {
		t.Fatal("rollback must refuse to discard workflow approval claims before dropping the claim table")
	}
	if strings.Contains(down, "execution_authorization_task_review_claims") {
		t.Fatal("workflow claim rollback must not alter task-review claims")
	}
}
