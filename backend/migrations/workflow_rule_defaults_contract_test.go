package migrations

import (
	"strings"
	"testing"
)

func TestWorkflowRuleDefaultsMigrationIsIdempotentAndRollbackSafe(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0066_workflow_rule_defaults.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := Files.ReadFile("pre/0066_workflow_rule_defaults.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))

	if !strings.Contains(up, "on conflict (rule_key) do nothing") {
		t.Fatal("workflow defaults must preserve existing operator-edited rules")
	}
	keys := []string{
		"approval.legal_external", "approval.public_posting", "approval.financial_limit_25",
		"safety.no_permanent_delete", "safety.account_changes", "workflow.checklist_required",
		"workflow.blocked_has_reason", "workflow.external_followup", "workflow.retry_limits",
		"verification.before_done", "verification.claims_need_sources", "developer.github_quality_gate",
		"content.medium_draft_only", "learning.corrections_feed_memory",
	}
	for _, key := range keys {
		if strings.Count(up, "'"+key+"'") != 1 {
			t.Fatalf("up migration must seed %q exactly once", key)
		}
		if strings.Count(down, "'"+key+"'") != 1 {
			t.Fatalf("down migration must identify %q exactly once", key)
		}
	}
	if !strings.Contains(down, "where (id, rule_key) in") {
		t.Fatal("rollback must remove only rows created with deterministic migration identities")
	}
}
