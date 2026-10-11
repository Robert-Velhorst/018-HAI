package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawGatewayAdmissionIntentMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0081_openclaw_gateway_admission_intents.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"'admitting'", "'not_admitted'", "'admitted'", "'needs_review'", "'terminal'",
		"idx_openclaw_gateway_session_receipts_unresolved", "reconciled_at is null",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("admission-intent migration is missing %q", want)
		}
	}
	downBytes, err := Files.ReadFile("pre/0081_openclaw_gateway_admission_intents.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{"status in ('admitting', 'not_admitted')", "raise exception", "drop index if exists idx_openclaw_gateway_session_receipts_unresolved"} {
		if !strings.Contains(down, want) {
			t.Fatalf("admission-intent rollback does not preserve unresolved evidence: missing %q", want)
		}
	}
}
