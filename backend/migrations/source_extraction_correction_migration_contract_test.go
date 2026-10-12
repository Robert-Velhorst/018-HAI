package migrations

import (
	"strings"
	"testing"
)

func TestSourceExtractionCorrectionMigrationPersistsPrivateOutboxAndQueueLink(t *testing.T) {
	up, err := Files.ReadFile("pre/0088_source_extraction_correction_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0088_source_extraction_correction_outbox.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	downSQL := strings.ToLower(string(down))
	for _, required := range []string{
		"create table public.source_extraction_corrections",
		"owner_identity varchar(255) not null",
		"expected_revision timestamptz not null",
		"patch_json jsonb not null",
		"before_state_json jsonb not null",
		"references public.durable_jobs(id) on delete restrict",
		"ck_source_extraction_correction_status",
		"ck_source_extraction_correction_phase",
		"ux_source_extraction_corrections_owner_idempotency",
		"ux_source_extraction_corrections_active_extraction",
		"ux_source_extraction_corrections_durable_job",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("correction outbox migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{"delete from", "truncate", "cascade"} {
		if strings.Contains(upSQL, forbidden) || strings.Contains(downSQL, forbidden) {
			t.Errorf("correction outbox migration must preserve records; found %q", forbidden)
		}
	}
	preflight := strings.Index(downSQL, "do $$")
	lock := strings.Index(downSQL, "lock table public.source_extraction_corrections in access exclusive mode")
	check := strings.Index(downSQL, "if exists (select 1 from public.source_extraction_corrections)")
	drop := strings.Index(downSQL, "drop table")
	if preflight < 0 || lock < preflight || check < lock || drop < check || !strings.Contains(downSQL, "source extraction correction history exists") {
		t.Fatal("down migration must lock the table, refuse persisted history, then drop within the same transaction block")
	}
}
