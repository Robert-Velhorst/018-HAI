package migrations

import (
	"strings"
	"testing"
)

func TestConnectedSourceOwnerActivityIndexesContract(t *testing.T) {
	up, err := Files.ReadFile("pre/0061_connected_source_owner_activity_indexes.up.sql")
	if err != nil {
		t.Fatalf("read owner activity indexes migration: %v", err)
	}
	script := strings.ToLower(string(up))
	for _, want := range []string{
		"idx_connected_sources_owner_updated",
		"connected_sources (owner_identity, updated_at desc)",
		"idx_source_sync_jobs_source_created",
		"source_sync_jobs (source_id, created_at desc)",
		"idx_source_audit_logs_source_created",
		"source_audit_logs (source_id, created_at desc)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("migration does not contain %q", want)
		}
	}
}
