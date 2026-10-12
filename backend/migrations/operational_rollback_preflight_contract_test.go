package migrations_test

import (
	"strings"
	"sync"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawmaintenance"
	"automation-hub-backend/migrations"
	"gorm.io/gorm/schema"
)

type operationalRollbackContract struct {
	version         string
	up              []string
	guard           []string
	lock            string
	destructiveSQL  string
	allowedDefaults []string
	forbiddenGuard  []string
	forbiddenUp     []string
}

func TestOperationalRollbackMigrationsGuardMeaningfulPersistedData(t *testing.T) {
	contracts := []operationalRollbackContract{
		{
			version: "0072_openclaw_maintenance",
			up: []string{
				"policy text not null default 'observe'",
				"installed text not null default ''",
				"available text not null default ''",
				"state text not null default 'unknown'",
				"reason text not null default ''",
				"next_check timestamptz not null default now()",
				"receipt_id text not null default ''",
				"updated_by text not null default ''",
				"create table if not exists openclaw_maintenance_jobs",
			},
			guard: []string{
				"from public.openclaw_maintenance_jobs",
				"from public.openclaw_maintenance_targets",
				"policy is distinct from 'observe'",
				"policy is distinct from 'auto_install_verified'",
				"installed is distinct from ''",
				"available is distinct from ''",
				"state is distinct from 'unknown'",
				"reason is distinct from ''",
				"checked_at is not null",
				"verified_at is not null",
				"receipt_id is distinct from ''",
				"updated_by is distinct from ''",
			},
			lock:           "lock table public.openclaw_maintenance_targets, public.openclaw_maintenance_jobs in access exclusive mode",
			destructiveSQL: "drop table if exists public.openclaw_maintenance_jobs",
			allowedDefaults: []string{
				"policy is distinct from 'observe' and policy is distinct from 'auto_install_verified'",
				"state is distinct from 'unknown'",
			},
			forbiddenGuard: []string{"next_check"},
		},
		{
			version: "0073_automation_runtime_model",
			up:      []string{"add column if not exists runtime_model character varying(255) default ''"},
			guard: []string{
				"from public.automations",
				"runtime_model is not null and btrim(runtime_model) <> ''",
			},
			lock:           "lock table public.automations in access exclusive mode",
			destructiveSQL: "drop column if exists runtime_model",
			allowedDefaults: []string{
				"runtime_model is not null and btrim(runtime_model) <> ''",
			},
		},
		{
			version: "0074_openclaw_session_instance",
			up: []string{
				"add column if not exists session_id character varying(256) default ''",
				"add column if not exists requested_model character varying(255) default ''",
			},
			guard: []string{
				"from public.openclaw_gateway_session_receipts",
				"session_id is not null and btrim(session_id) <> ''",
				"requested_model is not null and btrim(requested_model) <> ''",
			},
			lock:           "lock table public.openclaw_gateway_session_receipts in access exclusive mode",
			destructiveSQL: "drop column if exists requested_model",
			allowedDefaults: []string{
				"session_id is not null and btrim(session_id) <> ''",
				"requested_model is not null and btrim(requested_model) <> ''",
			},
		},
		{
			version: "0076_openclaw_usage_recovery",
			up: []string{
				"add column if not exists usage_summary text not null default ''",
				"add column if not exists usage_attempts integer not null default 0",
				"add column if not exists usage_next_attempt_at timestamptz",
				"add column if not exists usage_captured_at timestamptz",
			},
			guard: []string{
				"from public.openclaw_gateway_session_receipts",
				"usage_summary is distinct from ''",
				"usage_attempts is distinct from 0",
				"usage_next_attempt_at is not null",
				"usage_captured_at is not null",
			},
			lock:           "lock table public.openclaw_gateway_session_receipts in access exclusive mode",
			destructiveSQL: "drop index if exists public.idx_openclaw_usage_pending",
			allowedDefaults: []string{
				"usage_summary is distinct from ''",
				"usage_attempts is distinct from 0",
			},
		},
		{
			version: "0077_openclaw_usage_snapshot",
			up:      []string{"add column if not exists usage_snapshot_json jsonb"},
			guard: []string{
				"from public.openclaw_gateway_session_receipts",
				"usage_snapshot_json is not null",
			},
			lock:           "lock table public.openclaw_gateway_session_receipts in access exclusive mode",
			destructiveSQL: "drop index if exists public.idx_openclaw_usage_pending",
			allowedDefaults: []string{
				"usage_snapshot_json is not null",
			},
			forbiddenUp: []string{
				"usage_snapshot_json jsonb not null",
				"usage_snapshot_json jsonb default",
			},
		},
		{
			version: "0083_openclaw_maintenance_retry_budget",
			up: []string{
				"add column check_failures integer not null default 0",
				"check (check_failures >= 0)",
			},
			guard: []string{
				"from public.openclaw_maintenance_targets",
				"check_failures is distinct from 0",
			},
			lock:           "lock table public.openclaw_maintenance_targets in access exclusive mode",
			destructiveSQL: "drop column check_failures",
			allowedDefaults: []string{
				"check_failures is distinct from 0",
			},
		},
	}

	for _, contract := range contracts {
		t.Run(contract.version, func(t *testing.T) {
			up := readOperationalMigration(t, contract.version, "up")
			down := readOperationalMigration(t, contract.version, "down")
			for _, fragment := range contract.up {
				if !strings.Contains(up, fragment) {
					t.Errorf("up migration is missing model/default contract %q", fragment)
				}
			}
			for _, fragment := range contract.forbiddenUp {
				if strings.Contains(up, fragment) {
					t.Errorf("up migration changed expected NULL/backfill semantics: found %q", fragment)
				}
			}

			guardStart := strings.Index(down, "do $$")
			guardEnd := strings.Index(down, "end $$;")
			destructiveAt := strings.Index(down, contract.destructiveSQL)
			if guardStart < 0 || guardEnd < guardStart || destructiveAt < 0 || guardEnd > destructiveAt {
				t.Fatalf("preflight must complete before destructive SQL; guard=%d..%d destructive=%d", guardStart, guardEnd, destructiveAt)
			}
			guard := down[guardStart:guardEnd]
			for _, fragment := range append([]string{contract.lock, "raise exception"}, contract.guard...) {
				if !strings.Contains(guard, fragment) {
					t.Errorf("preflight is missing required lock/data-loss condition %q", fragment)
				}
			}
			if strings.Contains(strings.ToUpper(down), " CASCADE") {
				t.Error("rollback must not cascade through dependent records")
			}
			for _, fragment := range contract.allowedDefaults {
				if strings.Count(guard, fragment) != 1 {
					t.Errorf("default-aware guard must use %q exactly once", fragment)
				}
			}
			for _, fragment := range contract.forbiddenGuard {
				if strings.Contains(guard, fragment) {
					t.Errorf("preflight must not treat bootstrap-only value %q as meaningful data", fragment)
				}
			}
		})
	}
}

func TestOperationalRollbackGuardColumnsExistInCurrentModels(t *testing.T) {
	modelsToCheck := []struct {
		name      string
		model     any
		columns   []string
		defaults  map[string]string
		noDefault []string
	}{
		{
			name:     "automations",
			model:    &models.Automation{},
			columns:  []string{"runtime_model"},
			defaults: map[string]string{"runtime_model": ""},
		},
		{
			name:  "openclaw_gateway_session_receipts",
			model: &models.OpenClawGatewaySessionReceipt{},
			columns: []string{
				"session_id", "requested_model", "usage_summary", "usage_attempts",
				"usage_next_attempt_at", "usage_captured_at", "usage_snapshot_json",
			},
			defaults: map[string]string{
				"session_id": "", "requested_model": "", "usage_summary": "", "usage_attempts": "0",
			},
			noDefault: []string{"usage_next_attempt_at", "usage_captured_at", "usage_snapshot_json"},
		},
		{
			name:    "openclaw_maintenance_targets",
			model:   &openclawmaintenance.Target{},
			columns: []string{"policy", "installed", "available", "state", "reason", "checked_at", "next_check", "check_failures", "verified_at", "receipt_id", "updated_by"},
		},
		{
			name:    "openclaw_maintenance_jobs",
			model:   &openclawmaintenance.Job{},
			columns: []string{"id", "target", "kind", "version", "status", "evidence", "worker", "lease_digest", "lease_until", "started_at", "created_at", "finished_at", "result"},
		},
	}

	for _, item := range modelsToCheck {
		t.Run(item.name, func(t *testing.T) {
			parsed, err := schema.Parse(item.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatalf("parse model schema: %v", err)
			}
			for _, column := range item.columns {
				if parsed.FieldsByDBName[column] == nil {
					t.Errorf("current model %s no longer maps rollback-guard column %q", item.name, column)
				}
			}
			for column, want := range item.defaults {
				field := parsed.FieldsByDBName[column]
				if field == nil {
					continue
				}
				if !field.HasDefaultValue || field.DefaultValue != want {
					t.Errorf("current model default for %s.%s = (%q, %t), want (%q, true)", item.name, column, field.DefaultValue, field.HasDefaultValue, want)
				}
			}
			for _, column := range item.noDefault {
				if field := parsed.FieldsByDBName[column]; field != nil && field.HasDefaultValue {
					t.Errorf("current model unexpectedly adds a default for nullable/derived column %s.%s", item.name, column)
				}
			}
		})
	}
}

func readOperationalMigration(t *testing.T, version, direction string) string {
	t.Helper()
	path := "pre/" + version + "." + direction + ".sql"
	data, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	return strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(string(data), "\r\n", "\n"))), " ")
}
