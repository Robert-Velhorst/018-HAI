package migrations

import (
	"strings"
	"testing"
)

func TestBrainSkillCatalogFingerprintMigrationKeepsLegacyApprovalsFailClosed(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0106_brain_skill_selection_catalog_fingerprint.up.sql")
	if err != nil {
		t.Fatalf("read catalog fingerprint migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0106_brain_skill_selection_catalog_fingerprint.down.sql")
	if err != nil {
		t.Fatalf("read catalog fingerprint rollback: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"add column catalog_fingerprint varchar(64) not null default ''",
		"catalog_fingerprint = '' or catalog_fingerprint ~ '^[0-9a-f]{64}$'",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("catalog fingerprint migration is missing %q", want)
		}
	}
	if strings.Contains(up, "update public.brain_skill_selection_events") {
		t.Fatal("migration must not rewrite append-only historical decisions")
	}
	if !strings.Contains(down, "drop column if exists catalog_fingerprint") || strings.Contains(down, "cascade") {
		t.Fatalf("catalog fingerprint rollback is not narrowly scoped: %s", down)
	}
}
