package migrations

import (
	"strings"
	"testing"
)

func TestVerificationOwnerRunIndexesContract(t *testing.T) {
	up, err := Files.ReadFile("pre/0063_verification_owner_run_indexes.up.sql")
	if err != nil {
		t.Fatalf("read verification owner run indexes migration: %v", err)
	}
	script := strings.ToLower(string(up))
	for _, want := range []string{
		"idx_verification_runs_owner_created",
		"verification_runs (owner_identity, created_at desc)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("migration does not contain %q", want)
		}
	}
}
