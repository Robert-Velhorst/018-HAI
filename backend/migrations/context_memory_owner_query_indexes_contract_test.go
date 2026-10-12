package migrations

import (
	"strings"
	"testing"
)

func TestContextMemoryOwnerQueryIndexesContract(t *testing.T) {
	up, err := Files.ReadFile("pre/0062_context_memory_owner_query_indexes.up.sql")
	if err != nil {
		t.Fatalf("read context memory owner query indexes migration: %v", err)
	}
	script := strings.ToLower(string(up))
	for _, want := range []string{
		"idx_context_memories_owner_project_archive_updated",
		"context_memories (owner_identity, project_key, archived, updated_at desc)",
		"idx_context_memories_owner_archive_updated",
		"context_memories (owner_identity, archived, updated_at desc)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("migration does not contain %q", want)
		}
	}
}
