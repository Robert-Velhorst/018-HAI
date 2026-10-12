package migrations

import (
	"strings"
	"testing"
)

func TestModelTelemetryUsageSourceMigrationPreservesProvenance(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0102_model_telemetry_usage_source.up.sql")
	if err != nil {
		t.Fatalf("read token usage source migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0102_model_telemetry_usage_source.down.sql")
	if err != nil {
		t.Fatalf("read token usage source rollback: %v", err)
	}
	up := strings.Join(strings.Fields(strings.ToLower(string(upBytes))), " ")
	down := strings.Join(strings.Fields(strings.ToLower(string(downBytes))), " ")
	for _, fragment := range []string{
		"add column usage_source character varying(32) not null default 'estimated'",
		"'provider_reported'",
		"'provider_reported_partial'",
		"'estimated_uncertain'",
		"'provider_report_invalid'",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("usage source migration is missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"lock table public.model_run_telemetries in access exclusive mode",
		"where usage_source <> 'estimated'",
		"rollback refused: token usage provenance must remain attached to telemetry rows",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("usage source rollback is missing data-preservation guard %q", fragment)
		}
	}
}
