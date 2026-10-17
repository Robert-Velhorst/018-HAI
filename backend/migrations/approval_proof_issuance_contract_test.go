package migrations

import (
	"strings"
	"testing"
)

func TestApprovalProofIssuanceMigrationIsOwnerScopedAndImmutable(t *testing.T) {
	up, err := Files.ReadFile("pre/0129_automation_approval_proof_issuances.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0129_automation_approval_proof_issuances.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	for _, required := range []string{
		"primary key (owner_identity, approval_source_id)",
		"unique (owner_identity, proof_id)",
		"'automation.api.read'",
		"before update or delete",
		"before truncate",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("issuance migration is missing %q", required)
		}
	}
	downSQL := strings.ToUpper(string(down))
	lock := strings.Index(downSQL, "LOCK TABLE PUBLIC.AUTOMATION_APPROVAL_PROOF_ISSUANCES")
	guard := strings.Index(downSQL, "RAISE EXCEPTION")
	drop := strings.Index(downSQL, "DROP TABLE")
	if lock < 0 || guard < 0 || drop < 0 || !(lock < guard && guard < drop) {
		t.Fatalf("rollback must lock and refuse before dropping issuance history: lock=%d guard=%d drop=%d", lock, guard, drop)
	}
}

func TestAPIReadProofScopeMigrationIsForwardOnlyAndRollbackSafe(t *testing.T) {
	up, err := Files.ReadFile("pre/0130_automation_approval_proof_api_read_scope.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0130_automation_approval_proof_api_read_scope.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(up)), "'automation.api.read'") {
		t.Fatal("forward migration does not allow the supported API-read approval scope")
	}
	downSQL := strings.ToLower(string(down))
	if !strings.Contains(downSQL, "where scope = 'automation.api.read'") ||
		!strings.Contains(downSQL, "rollback refused") {
		t.Fatal("rollback must preserve API-read proof history instead of silently invalidating it")
	}
}
