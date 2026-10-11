package migrations

import (
	"strings"
	"testing"
)

func TestFrameworkEvidencePreflightMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0030_framework_evidence_preflights.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := normalizeMigrationLineEndings(string(upBytes))
	for _, required := range []string{
		"framework_evidence_preflights",
		"PRIMARY KEY (\n        owner_identity,\n        task_plan_id,\n        framework_selection_id,\n        preflight_digest",
		"preflight_digest ~ '^[0-9a-f]{64}$'",
		"status = 'passed'",
		"assertions_json bytea NOT NULL",
		"hai_framework_evidence_json_bytes_valid",
		"owner_identity = btrim(owner_identity)",
		"BEFORE UPDATE OR DELETE",
		"BEFORE TRUNCATE",
		"hai_reject_framework_evidence_preflight_mutation",
		"framework evidence preflight records are append-only",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("framework evidence preflight migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(up), " CASCADE") {
		t.Fatal("framework evidence preflight migration must not use CASCADE")
	}
	if strings.Contains(up, "IF NOT EXISTS") || strings.Contains(up, "OR REPLACE") {
		t.Fatal("versioned framework evidence migration must fail on schema drift")
	}

	downBytes, err := Files.ReadFile("pre/0030_framework_evidence_preflights.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := normalizeMigrationLineEndings(string(downBytes))
	if !strings.Contains(down, "refusing to remove non-empty immutable framework evidence preflight ledger") {
		t.Fatal("framework evidence rollback must refuse to discard immutable records")
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("framework evidence rollback must not use CASCADE")
	}
}
