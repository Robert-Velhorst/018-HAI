package migrations

import (
	"strings"
	"testing"
)

func TestLLMModelMaintenanceAdmissionClaimMigrationIsAdditiveAndFenced(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0101_llm_model_maintenance_admission_claims.up.sql")
	if err != nil {
		t.Fatalf("read model-maintenance admission migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0101_llm_model_maintenance_admission_claims.down.sql")
	if err != nil {
		t.Fatalf("read model-maintenance admission rollback: %v", err)
	}
	up := strings.Join(strings.Fields(strings.ToLower(string(upBytes))), " ")
	down := strings.Join(strings.Fields(strings.ToLower(string(downBytes))), " ")
	for _, fragment := range []string{
		"create table public.llm_model_maintenance_admission_claims",
		"primary key (provider_id, model_id, configuration_fingerprint)",
		"claim_token uuid not null",
		"state = 'in_progress'",
		"state = 'retry_wait'",
		"state = 'succeeded'",
		"configuration_fingerprint ~ '^[0-9a-f]{64}$'",
		"no endpoint, credential, or prompt data is stored",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("admission migration is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"delete from public.llm_model_maintenances", "truncate public.llm_model_maintenances", "cascade"} {
		if strings.Contains(up, forbidden) || strings.Contains(down, forbidden) {
			t.Errorf("admission migration must not alter maintenance history or cascade: found %q", forbidden)
		}
	}
	for _, fragment := range []string{
		"lock table public.llm_model_maintenance_admission_claims in access exclusive mode",
		"where state = 'in_progress'",
		"or retry_after > clock_timestamp()",
		"raise exception",
		"drop table if exists public.llm_model_maintenance_admission_claims",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("admission rollback is missing safety guard %q", fragment)
		}
	}
}
