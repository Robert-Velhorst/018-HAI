package migrations

import (
	"strings"
	"testing"
)

func TestSourceSyncIdempotencyIndexExcludesEmptyMetadataWithoutDeletingHistory(t *testing.T) {
	up, err := Files.ReadFile("pre/0105_source_sync_idempotency_nonempty.up.sql")
	if err != nil {
		t.Fatalf("read source sync idempotency migration: %v", err)
	}
	down, err := Files.ReadFile("pre/0105_source_sync_idempotency_nonempty.down.sql")
	if err != nil {
		t.Fatalf("read source sync idempotency rollback: %v", err)
	}

	upSQL := strings.Join(strings.Fields(strings.ToLower(string(up))), " ")
	for _, required := range []string{
		"drop index if exists public.ux_source_sync_jobs_owner_source_idempotency",
		"create unique index ux_source_sync_jobs_owner_source_idempotency",
		"on public.source_sync_jobs (owner_identity, source_id, idempotency_key_hash)",
		"btrim(owner_identity) <> ''",
		"btrim(idempotency_key_hash) <> ''",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("source sync idempotency migration is missing contract %q", required)
		}
	}
	for _, forbidden := range []string{"delete from public.source_sync_jobs", "truncate public.source_sync_jobs"} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("source sync idempotency migration must preserve job history; found %q", forbidden)
		}
	}

	downSQL := strings.Join(strings.Fields(strings.ToLower(string(down))), " ")
	for _, required := range []string{
		"cannot restore the legacy source sync idempotency index while duplicate metadata rows exist",
		"create unique index ux_source_sync_jobs_owner_source_idempotency",
		"on public.source_sync_jobs (owner_identity, source_id, idempotency_key_hash)",
	} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("source sync idempotency rollback is missing contract %q", required)
		}
	}
	for _, forbidden := range []string{"delete from public.source_sync_jobs", "truncate public.source_sync_jobs"} {
		if strings.Contains(downSQL, forbidden) {
			t.Errorf("source sync idempotency rollback must preserve job history; found %q", forbidden)
		}
	}
}
