package migrations

import (
	"strings"
	"testing"
)

func TestTrelloSignedWebhookRollbackRefusesToDiscardDeliveryHistory(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0103_trello_signed_webhook_intake.up.sql")
	if err != nil {
		t.Fatalf("read Trello webhook migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0103_trello_signed_webhook_intake.down.sql")
	if err != nil {
		t.Fatalf("read Trello webhook rollback: %v", err)
	}

	up := strings.Join(strings.Fields(strings.ToLower(string(upBytes))), " ")
	down := strings.Join(strings.Fields(strings.ToLower(string(downBytes))), " ")
	for _, required := range []string{
		"create table public.trello_webhook_receipts",
		"action_id character varying(24) not null",
		"durable_job_id uuid not null",
		"unique (source_id, action_id)",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("Trello webhook migration is missing durable receipt contract %q", required)
		}
	}

	lock := strings.Index(down, "execute 'lock table public.trello_webhook_receipts in access exclusive mode'")
	preflight := strings.Index(down, "if exists ( select 1 from public.trello_webhook_receipts limit 1 )")
	refusal := strings.Index(down, "raise exception 'rollback refused: trello webhook receipts contain delivery history'")
	drop := strings.Index(down, "drop table if exists public.trello_webhook_receipts")
	if lock < 0 || preflight < lock || refusal < preflight || drop < refusal {
		t.Fatalf("rollback must lock, check all receipt history, refuse if found, then drop; positions lock=%d preflight=%d refusal=%d drop=%d", lock, preflight, refusal, drop)
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("Trello webhook rollback must not cascade through related records")
	}
}
