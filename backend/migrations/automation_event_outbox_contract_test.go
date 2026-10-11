package migrations

import (
	"strings"
	"testing"
)

func TestAutomationEventOutboxMigrationIsDurableBoundedAndReversible(t *testing.T) {
	up, err := Files.ReadFile("post/0004_automation_event_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("post/0004_automation_event_outbox.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(up)
	for _, required := range []string{
		"CREATE TABLE public.automation_event_outbox",
		"payload jsonb NOT NULL",
		"attempt_count integer NOT NULL",
		"max_attempts integer NOT NULL",
		"lease_token uuid",
		"lease_until timestamp with time zone",
		"dead_lettered",
		"idx_automation_event_outbox_claim",
		"WHERE status = 'pending'",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("automation event outbox migration lacks %q", required)
		}
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS public.automation_event_outbox") {
		t.Fatal("automation event outbox migration is not reversible")
	}
}
