package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawRetainedArtifactMigrationBindsOwnerToSourceAndReceipt(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0093_openclaw_artifact_owner_binding.up.sql")
	if err != nil {
		t.Fatalf("read retained-artifact owner-binding migration: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"uq_automation_launch_events_artifact_owner_binding",
		"on public.automation_launch_events (id, owner_identity, execution_reference)",
		"uq_openclaw_session_receipts_artifact_owner_binding",
		"on public.openclaw_gateway_session_receipts (execution_reference, owner_identity)",
		"fk_openclaw_retained_artifacts_owner_event",
		"foreign key (source_event_id, owner_identity, execution_reference)",
		"fk_openclaw_retained_artifacts_owner_receipt",
		"foreign key (execution_reference, owner_identity)",
		"reconcile before adding owner-binding constraints",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("retained-artifact owner-binding migration is missing %q", want)
		}
	}

	downBytes, err := Files.ReadFile("pre/0093_openclaw_artifact_owner_binding.down.sql")
	if err != nil {
		t.Fatalf("read retained-artifact owner-binding rollback: %v", err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"drop constraint if exists fk_openclaw_retained_artifacts_owner_receipt",
		"drop constraint if exists fk_openclaw_retained_artifacts_owner_event",
		"drop index if exists public.uq_openclaw_session_receipts_artifact_owner_binding",
		"drop index if exists public.uq_automation_launch_events_artifact_owner_binding",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("retained-artifact owner-binding rollback is missing %q", want)
		}
	}
	for _, forbidden := range []string{"delete from", "truncate", "drop table"} {
		if strings.Contains(down, forbidden) {
			t.Fatalf("retained-artifact owner-binding rollback must preserve data, found %q", forbidden)
		}
	}
}
