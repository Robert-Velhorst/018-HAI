package migrations

import (
	"strings"
	"testing"
)

func TestWorkflowCompletionSettlementProofsMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0044_workflow_completion_settlement_proofs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := normalizeMigrationLineEndings(string(upBytes))

	for _, required := range []string{
		"CREATE TABLE public.workflow_completion_attestations",
		"workflow_id uuid NOT NULL UNIQUE",
		"owner_identity character varying(255) NOT NULL",
		"task_plan_id character varying(120) NOT NULL",
		"completion_status character varying(32) NOT NULL",
		"verification_status character varying(80) NOT NULL",
		"runtime_evidence_uri character varying(2048) NOT NULL",
		"runtime_evidence_digest character(64) NOT NULL",
		"REFERENCES public.workflow_items (id)",
		"ON UPDATE RESTRICT ON DELETE RESTRICT",
		"completion_status = 'completed'",
		"verification_status IN ('verified', 'test_passed')",
		"workflow_completion_attestations_validate_insert",
		"CREATE TABLE public.pursuit_portfolio_workflow_settlement_proofs",
		"settlement_id uuid NOT NULL UNIQUE",
		"reservation_id uuid NOT NULL UNIQUE",
		"proposal_item_id uuid NOT NULL UNIQUE",
		"workflow_id uuid NOT NULL UNIQUE",
		"completion_attestation_id uuid NOT NULL UNIQUE",
		"proposal_item_digest character(64) NOT NULL",
		"approval_decision_digest character(64) NOT NULL",
		"authorization_receipt_digest character(64) NOT NULL",
		"authorization_consumption_digest character(64) NOT NULL",
		"authorization_consumption_target character varying(1024) NOT NULL",
		"completion_attestation_digest character(64) NOT NULL",
		"request_digest character(64) NOT NULL",
		"actual_effort_minutes bigint NOT NULL",
		"actual_cost_micros bigint NOT NULL",
		"actor character varying(255) NOT NULL",
		"settled_at timestamp with time zone NOT NULL",
		"REFERENCES public.pursuit_resource_reservation_settlements (id)",
		"REFERENCES public.pursuit_resource_reservations (id)",
		"REFERENCES public.pursuit_portfolio_execution_proposal_items (id)",
		"REFERENCES public.pursuit_portfolio_execution_proposal_decisions (id)",
		"REFERENCES public.execution_authorization_receipts",
		"REFERENCES public.execution_authorization_consumptions",
		"REFERENCES public.workflow_completion_attestations",
		"pursuit_portfolio_workflow_settlement_proofs_validate_insert",
		"workflow completion and portfolio settlement proofs are append-only",
		"workflow_completion_attestations_reject_update",
		"workflow_completion_attestations_reject_delete",
		"workflow_completion_attestations_reject_truncate",
		"pursuit_portfolio_workflow_settlement_proofs_reject_update",
		"pursuit_portfolio_workflow_settlement_proofs_reject_delete",
		"pursuit_portfolio_workflow_settlement_proofs_reject_truncate",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("workflow completion settlement proof migration missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"schema_validated",
		"source_supported",
		"human_approved",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("workflow completion attestation must not accept %q as completion proof", forbidden)
		}
	}
	if strings.Contains(strings.ToUpper(up), " CASCADE") {
		t.Fatal("workflow completion settlement proof migration must not use CASCADE")
	}

	downBytes, err := Files.ReadFile("pre/0044_workflow_completion_settlement_proofs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := normalizeMigrationLineEndings(string(downBytes))
	if !strings.Contains(
		down,
		"refusing to remove non-empty workflow completion and portfolio settlement proof ledgers",
	) {
		t.Fatal("rollback must explicitly refuse to discard immutable proof records")
	}
	for _, table := range []string{
		"public.pursuit_portfolio_workflow_settlement_proofs",
		"public.workflow_completion_attestations",
	} {
		if !strings.Contains(down, "FROM "+table+"\n        LIMIT 1") {
			t.Errorf("rollback must refuse a non-empty %s table", table)
		}
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("workflow completion settlement proof rollback must not use CASCADE")
	}
}
