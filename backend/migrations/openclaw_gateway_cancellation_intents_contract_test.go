package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawGatewayCancellationIntentMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0082_openclaw_gateway_cancellation_intents.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"cancellation_intent_id", "cancellation_status", "cancellation_attempts",
		"'requested'", "'awaiting_identity'", "'acknowledged'", "'delivery_failed'",
		"idx_openclaw_gateway_cancellation_intent_id", "where cancellation_intent_id <> ''",
		"idx_openclaw_gateway_cancellations_pending", "reconciled_at is null",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("cancellation-intent migration is missing %q", want)
		}
	}
	downBytes, err := Files.ReadFile("pre/0082_openclaw_gateway_cancellation_intents.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"cancellation_intent_id <> ''", "raise exception",
		"drop index if exists idx_openclaw_gateway_cancellations_pending",
		"drop index if exists idx_openclaw_gateway_cancellation_intent_id",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("cancellation-intent rollback does not preserve audit evidence: missing %q", want)
		}
	}
}
