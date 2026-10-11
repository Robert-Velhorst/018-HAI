package frameworkregistry

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

func TestFrameworkRegistryMigrationDeclaresVersionedImmutableConstitutionContract(t *testing.T) {
	t.Parallel()

	upBytes, err := migrations.Files.ReadFile("pre/0003_framework_registry.up.sql")
	if err != nil {
		t.Fatalf("read framework registry up migration: %v", err)
	}
	downBytes, err := migrations.Files.ReadFile("pre/0003_framework_registry.down.sql")
	if err != nil {
		t.Fatalf("read framework registry down migration: %v", err)
	}
	up := string(upBytes)
	down := string(downBytes)

	requiredUpFragments := []string{
		"base_version integer DEFAULT 0 NOT NULL",
		"CONSTRAINT chk_robert_constitution_base_version CHECK",
		"NEW.base_version IS DISTINCT FROM OLD.base_version",
		"stale constitution activation",
		"(NEW.version > 1 AND NEW.base_version = 1)",
		"status IN ('active', 'superseded')",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_robert_constitution_active_owner",
		"CREATE TRIGGER trg_framework_selection_records_immutable",
		"CREATE TRIGGER trg_robert_constitution_versions_lifecycle",
		"CREATE CONSTRAINT TRIGGER trg_robert_constitution_active_history",
		"DEFERRABLE INITIALLY DEFERRED",
		"CREATE TRIGGER trg_robert_constitution_versions_no_truncate",
	}
	for _, fragment := range requiredUpFragments {
		if !strings.Contains(up, fragment) {
			t.Errorf("framework registry up migration is missing %q", fragment)
		}
	}

	requiredDownFragments := []string{
		"DROP TABLE IF EXISTS public.robert_constitution_versions",
		"DROP FUNCTION IF EXISTS public.hai_enforce_constitution_lifecycle",
		"DROP FUNCTION IF EXISTS public.hai_require_active_constitution_after_history",
		"DROP TABLE IF EXISTS public.framework_selection_records",
		"DROP FUNCTION IF EXISTS public.hai_reject_framework_selection_mutation",
		"DROP FUNCTION IF EXISTS public.hai_reject_framework_registry_truncate",
		"DROP TABLE IF EXISTS public.framework_preferences",
	}
	for _, fragment := range requiredDownFragments {
		if !strings.Contains(down, fragment) {
			t.Errorf("framework registry down migration is missing %q", fragment)
		}
	}
}

func TestFrameworkSelectorV5MigrationRequiresRealOperatingDigest(t *testing.T) {
	t.Parallel()

	upBytes, err := migrations.Files.ReadFile("pre/0029_framework_selector_v5_digest.up.sql")
	if err != nil {
		t.Fatalf("read selector-v5 digest migration: %v", err)
	}
	downBytes, err := migrations.Files.ReadFile("pre/0029_framework_selector_v5_digest.down.sql")
	if err != nil {
		t.Fatalf("read selector-v5 digest rollback: %v", err)
	}

	up := strings.ToLower(strings.ReplaceAll(string(upBytes), "\r\n", "\n"))
	down := strings.ToLower(strings.ReplaceAll(string(downBytes), "\r\n", "\n"))
	for _, fragment := range []string{
		"add column if not exists task_risk_level varchar(16)",
		"add column if not exists effective_risk_ceiling varchar(16)",
		"drop constraint if exists chk_framework_selection_records_operating_digest",
		"selector_algorithm_version not in ('selector-v4', 'selector-v5')",
		"operating_contract_digest <>\n                    '0000000000000000000000000000000000000000000000000000000000000000'",
		"selector_algorithm_version <> 'selector-v5'",
		"task_risk_level is not null and effective_risk_ceiling is not null",
		"chk_framework_selection_records_risk_ceiling_rank",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("selector-v5 migration missing %q", fragment)
		}
	}
	if !strings.Contains(down, "selector_algorithm_version <> 'selector-v4'") {
		t.Error("selector-v5 rollback does not restore the selector-v4 digest contract")
	}
	for _, fragment := range []string{
		"cannot roll back selector-v5 risk contract while selector-v5 records exist",
		"drop column if exists effective_risk_ceiling",
		"drop column if exists task_risk_level",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("selector-v5 rollback missing %q", fragment)
		}
	}
}

func TestFrameworkOperatingContractMigrationIsTypedIndexedAndReversible(t *testing.T) {
	t.Parallel()

	upBytes, err := migrations.Files.ReadFile("pre/0005_framework_operating_contract.up.sql")
	if err != nil {
		t.Fatalf("read operating contract migration: %v", err)
	}
	downBytes, err := migrations.Files.ReadFile("pre/0005_framework_operating_contract.down.sql")
	if err != nil {
		t.Fatalf("read operating contract rollback: %v", err)
	}

	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))
	jsonColumns := []string{
		"life_domains_json",
		"needs_state_json",
		"capacity_json",
		"agent_cards_json",
		"delegations_json",
		"communication_json",
		"coordination_json",
		"action_autonomy_json",
		"stop_conditions_json",
		"outcome_monitoring_json",
		"chief_of_staff_json",
	}
	for _, column := range jsonColumns {
		if !strings.Contains(up, "add column if not exists "+column+" jsonb") {
			t.Errorf("up migration does not add typed JSON column %q", column)
		}
		if !strings.Contains(up, "jsonb_typeof("+column+")") {
			t.Errorf("up migration does not constrain JSON shape for %q", column)
		}
		if !strings.Contains(down, "drop column if exists "+column) {
			t.Errorf("down migration does not remove %q", column)
		}
	}
	for _, fragment := range []string{
		"operating_contract_digest character(64)",
		"operating_contract_digest ~ '^[0-9a-f]{64}$'",
		"selector_algorithm_version <> 'selector-v4'",
		"create index if not exists idx_framework_selection_records_operating_digest",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"drop index if exists public.idx_framework_selection_records_operating_digest",
		"drop constraint if exists chk_framework_selection_records_operating_digest",
		"drop column if exists operating_contract_digest",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("down migration missing %q", fragment)
		}
	}
}
