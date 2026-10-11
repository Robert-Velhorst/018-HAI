package migrations

import (
	"strings"
	"testing"
)

func TestManualSourceSyncMigrationPreflightsUniqueIndexesAndPreservesHistory(t *testing.T) {
	up, err := Files.ReadFile("pre/0087_source_manual_sync_jobs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0087_source_manual_sync_jobs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	downSQL := strings.ToLower(string(down))
	preflight := strings.Index(upSQL, "do $$")
	if preflight < 0 {
		t.Fatal("manual sync migration must preflight before creating uniqueness constraints")
	}
	for _, index := range []string{
		"ux_source_sync_jobs_owner_source_idempotency",
		"ux_source_sync_jobs_durable_job",
		"ux_source_sync_jobs_one_active_manual",
	} {
		position := strings.Index(upSQL, "create unique index if not exists "+index)
		if position < 0 || position < preflight {
			t.Errorf("unique index %s must be created after the data preflight", index)
		}
	}
	for _, phrase := range []string{
		"group by owner_identity, source_id, idempotency_key_hash",
		"group by durable_job_id",
		"group by source_id",
	} {
		if !strings.Contains(upSQL, phrase) {
			t.Errorf("migration is missing a duplicate preflight for %q", phrase)
		}
	}
	for _, forbidden := range []string{"delete from", "truncate", "drop table", "cascade"} {
		if strings.Contains(upSQL, forbidden) || strings.Contains(downSQL, forbidden) {
			t.Errorf("manual sync migration must preserve existing records; found %q", forbidden)
		}
	}
	for _, column := range []string{"owner_identity", "idempotency_key_hash", "request_hash", "durable_job_id"} {
		if !strings.Contains(downSQL, "drop column if exists "+column) {
			t.Errorf("down migration must remove added metadata column %s", column)
		}
	}
}

func TestManualSourceSyncDownMigrationFailsClosedWhileRecordsExist(t *testing.T) {
	down, err := Files.ReadFile("pre/0087_source_manual_sync_jobs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downSQL := strings.ToLower(string(down))
	preflight := strings.Index(downSQL, "do $$")
	firstDrop := strings.Index(downSQL, "drop index")
	if preflight < 0 || firstDrop < 0 || preflight > firstDrop {
		t.Fatal("down migration must preflight persisted records before dropping indexes or metadata")
	}
	for _, required := range []string{
		"from public.source_sync_jobs",
		"where mode = 'manual_async_sync'",
		"from public.durable_jobs",
		"where kind = 'source.manual_sync'",
		"raise exception",
	} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("down migration must fail closed while %q exists", required)
		}
	}
}
