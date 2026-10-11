package migrations

import (
	"strings"
	"testing"
)

func TestTrelloResumableSyncMigrationPreservesEvidenceAndCheckpointsOwnerBoundPages(t *testing.T) {
	up, err := Files.ReadFile("pre/0095_trello_resumable_sync.up.sql")
	if err != nil {
		t.Fatalf("read Trello sync migration: %v", err)
	}
	down, err := Files.ReadFile("pre/0095_trello_resumable_sync.down.sql")
	if err != nil {
		t.Fatalf("read Trello sync rollback: %v", err)
	}
	upSQL := strings.Join(strings.Fields(strings.ToLower(string(up))), " ")
	for _, required := range []string{
		"duplicate source raw item identity",
		"ux_source_raw_items_source_external",
		"index_state.indisunique",
		"index_state.indisvalid",
		"index_state.indisready",
		"index_state.indnkeyatts = 2",
		"index_state.indnatts = 2",
		"index_relation.relname = 'ux_source_raw_items_source_external'",
		"array['source_id', 'external_id']::name[]",
		"opclass.opcdefault",
		"identity_column_count <> 2",
		"source_raw_items.source_id and external_id must both exist and be not null",
		"attribute.attnotnull",
		"attribute.attname in ('source_id', 'external_id') and attribute.attnum > 0 and not attribute.attisdropped and not attribute.attnotnull",
		"attribute.attcollation",
		"is not a valid unique btree index",
		"trello_sync_states",
		"owner_identity character varying(255) not null",
		"board_id character varying(32) not null",
		"logical_job_id uuid",
		"card_cursor character varying(24)",
		"action_cursor character varying(24)",
		"trello_sync_pages",
		"unique (source_id, logical_job_id, generation, phase, cursor_before)",
		"request_count integer not null",
		"trello_action_receipts",
		"progress_phase",
		"progress_pages",
		"progress_records",
		"progress_message",
		"on delete cascade",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("Trello resumable migration is missing contract %q", required)
		}
	}
	if strings.Contains(upSQL, "lock table public.source_raw_items") {
		t.Fatal("Trello duplicate preflight must not hold a write-blocking table lock; unique-index creation remains the atomic duplicate check")
	}
	if strings.Contains(upSQL, "opclass.opcintype = attribute.atttypid") {
		t.Fatal("Trello index validation must not require the default varchar operator class input type to equal the varchar column type")
	}
	if strings.Contains(upSQL, "delete from public.source_raw_items") || strings.Contains(upSQL, "truncate public.source_raw_items") {
		t.Fatal("Trello migration must not delete imported source evidence")
	}
	downSQL := strings.ToLower(string(down))
	if strings.Contains(downSQL, "delete from public.source_raw_items") || strings.Contains(downSQL, "truncate public.source_raw_items") {
		t.Fatal("Trello rollback must not delete imported source evidence")
	}
	for _, required := range []string{"trello_action_receipts", "trello_sync_pages", "trello_sync_states", "ux_source_raw_items_source_external"} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("Trello rollback does not remove its owned structure %q", required)
		}
	}
}
