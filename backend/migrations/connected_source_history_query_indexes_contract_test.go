package migrations

import (
	"strings"
	"testing"
)

func TestConnectedSourceHistoryQueryIndexesContract(t *testing.T) {
	up, err := Files.ReadFile("pre/0064_connected_source_history_query_indexes.up.sql")
	if err != nil {
		t.Fatalf("read connected source history indexes migration: %v", err)
	}
	script := strings.ToLower(string(up))
	for _, want := range []string{
		"idx_source_raw_items_source_updated",
		"source_raw_items (source_id, updated_at desc)",
		"idx_source_extractions_source_archive_updated",
		"source_extractions (source_id, archived, updated_at desc)",
		"idx_source_extractions_source_project_archive_updated",
		"source_extractions (source_id, project_key, archived, updated_at desc)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("migration does not contain %q", want)
		}
	}
}
