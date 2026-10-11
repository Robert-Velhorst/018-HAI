package migrations

import (
	"strings"
	"testing"
)

func TestConnectedSourceSchedulerIndexMigration(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0060_connected_source_scheduler_index.up.sql")
	if err != nil {
		t.Fatalf("read scheduler index migration: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	for _, fragment := range []string{
		"create index if not exists idx_connected_sources_scheduler_active",
		"on public.connected_sources (updated_at desc)",
		"where enabled = true",
		"status not in ('paused', 'revoked')",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("scheduler index migration missing %q", fragment)
		}
	}

	downBytes, err := Files.ReadFile("pre/0060_connected_source_scheduler_index.down.sql")
	if err != nil {
		t.Fatalf("read scheduler index rollback: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(downBytes)), "drop index if exists public.idx_connected_sources_scheduler_active") {
		t.Fatal("scheduler index rollback must remove only the scheduler index")
	}
}
