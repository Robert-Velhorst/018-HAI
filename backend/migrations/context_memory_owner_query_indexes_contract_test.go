package migrations

import (
	"strings"
	"testing"
)

func TestContextMemoryOwnerQueryMigrationProvidesBoundedSearchIndexes(t *testing.T) {
	t.Parallel()

	upBytes, err := Files.ReadFile("pre/0061_context_memory_owner_query_indexes.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, fragment := range []string{
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"idx_context_memories_owner_active_updated",
		"idx_context_memories_owner_project_active_updated",
		"idx_context_memories_owner_kind_active_updated",
		"idx_context_memories_search_trgm",
		"owner_identity",
		"project_key",
		"updated_at DESC",
		"gin_trgm_ops",
		"COALESCE(content, '')",
		"COALESCE(summary, '')",
		"COALESCE(tags, '')",
	} {
		if !strings.Contains(up, fragment) {
			t.Fatalf("0061 up migration missing %q", fragment)
		}
	}

	downBytes, err := Files.ReadFile("pre/0061_context_memory_owner_query_indexes.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, index := range []string{
		"idx_context_memories_search_trgm",
		"idx_context_memories_owner_kind_active_updated",
		"idx_context_memories_owner_project_active_updated",
		"idx_context_memories_owner_active_updated",
	} {
		if !strings.Contains(down, "DROP INDEX IF EXISTS public."+index) {
			t.Fatalf("0061 down migration does not remove %s", index)
		}
	}
	if strings.Contains(strings.ToUpper(down), "DROP EXTENSION") {
		t.Fatal("0061 rollback must not remove a shared pg_trgm extension")
	}
}
