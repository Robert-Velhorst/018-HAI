package migrations

import (
	"strings"
	"testing"
)

func TestLLMModelMaintenanceStatusesMigrationAllowsPersistedServiceOutcomes(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0096_llm_model_maintenance_statuses.up.sql")
	if err != nil {
		t.Fatalf("read model-maintenance status migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0096_llm_model_maintenance_statuses.down.sql")
	if err != nil {
		t.Fatalf("read model-maintenance status rollback: %v", err)
	}

	up := strings.Join(strings.Fields(strings.ToLower(string(upBytes))), " ")
	for _, status := range []string{"'not_enforced'", "'failed'", "'current'", "'provider_managed'", "'installed'", "'updated'", "'operator_managed'", "'approval_required'"} {
		if !strings.Contains(up, status) {
			t.Errorf("expanded model-maintenance status constraint is missing %s", status)
		}
	}
	if strings.Contains(up, "delete from public.llm_model_maintenances") || strings.Contains(up, "truncate public.llm_model_maintenances") {
		t.Fatal("model-maintenance status migration must preserve all existing history")
	}

	down := strings.Join(strings.Fields(strings.ToLower(string(downBytes))), " ")
	for _, fragment := range []string{
		"where status in ('operator_managed', 'approval_required')",
		"raise exception",
		"add constraint chk_llm_model_maintenance_status",
		"'not_enforced'",
		"'updated'",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("model-maintenance status rollback is missing safety contract %q", fragment)
		}
	}
	if strings.Contains(down, "delete from public.llm_model_maintenances") || strings.Contains(down, "truncate public.llm_model_maintenances") {
		t.Fatal("model-maintenance status rollback must preserve all history")
	}
}
