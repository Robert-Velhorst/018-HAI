package migrations

import (
	"strings"
	"testing"
)

func TestTrelloWebhookReconciliationGenerationMigrationPreservesPendingWork(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0109_trello_webhook_reconciliation_generations.up.sql")
	if err != nil {
		t.Fatalf("read Trello reconciliation migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0109_trello_webhook_reconciliation_generations.down.sql")
	if err != nil {
		t.Fatalf("read Trello reconciliation rollback: %v", err)
	}

	up := strings.Join(strings.Fields(strings.ToLower(string(upBytes))), " ")
	down := strings.Join(strings.Fields(strings.ToLower(string(downBytes))), " ")
	lockAt := strings.Index(up, "lock table public.connected_sources, public.trello_webhook_receipts in access exclusive mode")
	preflightAt := strings.Index(up, "do $migration$")
	alterAt := strings.Index(up, "alter table public.trello_webhook_receipts")
	if lockAt < 0 || preflightAt < 0 || alterAt < 0 || lockAt > preflightAt || lockAt > alterAt {
		t.Fatal("migration must lock connected sources before receipts, owner preflight, and schema changes")
	}
	for _, required := range []string{
		"pending trello board webhook receipts require a non-empty connected-source owner_identity",
		"nullif(btrim(sources.owner_identity), '') is null",
		"sources.owner_identity !~ '[^[:space:]]'",
		"add column reconciliation_generation bigint not null default 0",
		"create table public.trello_webhook_reconciliation_states",
		"requested_generation bigint not null default 0",
		"completed_generation bigint not null default 0",
		"required_trello_generation bigint not null default 1",
		"constraint ck_trello_reconciliation_owner_identity check (owner_identity ~ '[^[:space:]]')",
		"active_generation bigint",
		"active_required_trello_generation bigint",
		"active_sync_job_id uuid references public.source_sync_jobs(id) on delete set null",
		"row_number() over (partition by source_id order by received_at, id)",
		"where status in ('queued', 'dispatched')",
		"action_type not in ('commentcard', 'copycommentcard', 'updatecomment', 'deletecomment')",
		"left join public.trello_sync_states as sync_states",
		"create index ix_trello_webhook_receipts_reconciliation_pending",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("Trello reconciliation migration is missing contract %q", required)
		}
	}
	for _, required := range []string{
		"lock table public.trello_webhook_receipts, public.trello_webhook_reconciliation_states in access exclusive mode",
		"where reconciliation_generation > 0",
		"requested_generation > 0",
		"required_trello_generation > 1",
		"active_required_trello_generation is not null",
		"rollback refused: trello reconciliation generation history or pending work would be lost",
		"drop table public.trello_webhook_reconciliation_states",
		"drop index public.ix_trello_webhook_receipts_reconciliation_pending",
	} {
		if !strings.Contains(down, required) {
			t.Errorf("Trello reconciliation rollback is missing data-protection contract %q", required)
		}
	}
}
