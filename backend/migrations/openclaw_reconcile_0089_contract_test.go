package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawReconcileMigration0089Contract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0089_openclaw_reconcile_fairness_and_cancellation_retry.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"cancellation_automatic_attempts integer not null default 0",
		"cancellation_next_attempt_at timestamptz",
		"cancellation_delivery_lease_until timestamptz",
		"cancellation_delivery_token varchar(36)",
		"cancellation_review_at timestamptz",
		"'review_required'",
		"cancellation_automatic_attempts <= cancellation_attempts",
		"idx_openclaw_gateway_reconcile_pending_page",
		"idx_openclaw_gateway_cancellations_due",
		"openclaw_gateway_reconcile_cursors",
		"after_created_at",
		"cycle_high_created_at",
		"on conflict (name) do nothing",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("0089 up migration is missing %q", want)
		}
	}
	if strings.Contains(up, "embed.go") {
		t.Fatal("0089 migration must not depend on or modify the migration embed registry")
	}

	downBytes, err := Files.ReadFile("pre/0089_openclaw_reconcile_fairness_and_cancellation_retry.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.ToLower(string(downBytes))
	lockCursor := strings.Index(down, "lock table public.openclaw_gateway_reconcile_cursors in access exclusive mode")
	lockReceipts := strings.Index(down, "lock table public.openclaw_gateway_session_receipts in access exclusive mode")
	guard := strings.Index(down, "if exists (")
	if lockCursor < 0 || lockReceipts < 0 || guard < 0 || lockCursor > lockReceipts || lockReceipts > guard {
		t.Fatal("0089 down migration must lock the cursor then receipts before checking rollback guards")
	}
	for _, want := range []string{
		"lock table public.openclaw_gateway_reconcile_cursors in access exclusive mode",
		"lock table public.openclaw_gateway_session_receipts in access exclusive mode",
		"raise exception",
		"cancellation_status = 'review_required'",
		"cancellation_automatic_attempts > 0",
		"cancellation_delivery_token <> ''",
		"after_created_at is not null or cycle_high_created_at is not null",
		"drop table if exists openclaw_gateway_reconcile_cursors",
		"drop column cancellation_delivery_token",
		"'delivery_failed', 'not_required'))",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("0089 down migration is missing preservation guard or rollback step %q", want)
		}
	}
}
