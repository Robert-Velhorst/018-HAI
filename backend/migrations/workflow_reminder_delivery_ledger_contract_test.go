package migrations

import (
	"strings"
	"testing"
)

func TestWorkflowReminderDeliveryLedgerIsInternalExplicitAndAppendOnly(t *testing.T) {
	up, err := Files.ReadFile("pre/0055_workflow_reminder_delivery_ledger.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(up)
	for _, required := range []string{
		"CREATE TABLE public.workflow_reminder_delivery_authorizations",
		"CREATE TABLE public.workflow_reminder_delivery_attempts",
		"channel = 'in_app'",
		"AUTHORIZE ONE INTERNAL HAI REMINDER",
		"internal_reminder_delivery_authorization",
		"internal_reminder_delivery_receipt",
		"attempt_number BETWEEN 1 AND 3",
		"workflow_reminder_delivery_authorizations_reject_update",
		"workflow_reminder_delivery_attempts_reject_update",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("delivery migration lacks %q", required)
		}
	}
	for _, forbidden := range []string{"email", "webhook", "calendar_write", "desktop_push"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("internal delivery ledger unexpectedly contains %q", forbidden)
		}
	}
}

func TestWorkflowReminderDeliveryDeadLetterMigrationIsExplicitAndReversible(t *testing.T) {
	up, err := Files.ReadFile("pre/0056_workflow_reminder_delivery_dead_letter.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0056_workflow_reminder_delivery_dead_letter.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "'dead_lettered'") ||
		!strings.Contains(string(down), "refusing to remove the dead-letter status") {
		t.Fatal("dead-letter migration must add the terminal status and fail closed on rollback")
	}
}

func TestWorkflowReminderDeliveryExpiredStatusIsExplicitAndRollbackPreservesReceipts(t *testing.T) {
	up, err := Files.ReadFile("pre/0107_workflow_reminder_delivery_expired_status.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0107_workflow_reminder_delivery_expired_status.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "'expired'") ||
		!strings.Contains(string(down), "refusing to remove expired reminder delivery receipts") {
		t.Fatal("expired delivery migration must add a terminal state and fail closed on rollback")
	}
}

func TestWorkflowReminderDeliveryAllowsOneAuthorizationPerApprovedDecision(t *testing.T) {
	up, err := Files.ReadFile("pre/0057_workflow_reminder_single_delivery_authorization.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(up)
	for _, required := range []string{"owner_identity", "activation_request_id", "activation_decision_id", "channel", "UNIQUE"} {
		if !strings.Contains(text, required) {
			t.Fatalf("single-delivery migration lacks %q", required)
		}
	}
}
