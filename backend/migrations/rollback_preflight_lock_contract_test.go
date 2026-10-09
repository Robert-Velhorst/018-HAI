package migrations

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestDestructiveRollbacksLockAndRejectNonEmptyTables(t *testing.T) {
	tests := []struct {
		migration  string
		dataTables []string
		dropTables []string
	}{
		{
			migration: "pre/0004_task_state_storage.down.sql",
			dataTables: []string{
				"public.task_completion_plan_logs",
				"public.task_review_items",
				"public.task_review_decisions",
			},
			dropTables: []string{
				"public.task_review_decisions",
				"public.task_review_items",
				"public.task_completion_plan_logs",
			},
		},
		{
			migration: "pre/0014_unified_execution_authorization.down.sql",
			dataTables: []string{
				"public.execution_authorization_consumptions",
				"public.execution_authorization_final_effect_exercises",
				"public.execution_authorization_receipts",
				"public.workflow_decisions",
			},
			dropTables: []string{
				"public.execution_authorization_final_effect_exercises",
				"public.execution_authorization_consumptions",
				"public.execution_authorization_receipts",
			},
		},
		{
			migration:  "pre/0065_host_runtime_jobs.down.sql",
			dataTables: []string{"public.host_runtime_jobs"},
			dropTables: []string{"public.host_runtime_jobs"},
		},
	}

	lockPattern := regexp.MustCompile(`(?is)\block\s+table\s+(.+?)\s+in\s+access\s+exclusive\s+mode\s*;`)
	blockPattern := regexp.MustCompile(`(?is)\bdo\s+\$\$(.*?)\$\$\s*;`)
	fromPattern := regexp.MustCompile(`(?i)\bfrom\s+(public\.[a-z_][a-z0-9_]*)\b`)
	existsPattern := regexp.MustCompile(`(?i)\bexists\s*\(`)
	dropPattern := regexp.MustCompile(`(?im)^\s*drop\s+table\s+(?:if\s+exists\s+)?(public\.[a-z_][a-z0-9_]*)\s*;`)
	destructivePattern := regexp.MustCompile(`(?im)^\s*(?:drop\b|alter\s+table\b)`)

	for _, test := range tests {
		t.Run(test.migration, func(t *testing.T) {
			contents, err := Files.ReadFile(test.migration)
			if err != nil {
				t.Fatalf("read migration: %v", err)
			}
			sql := strings.ToLower(string(contents))

			lockMatch := lockPattern.FindStringSubmatchIndex(sql)
			if lockMatch == nil {
				t.Fatal("rollback must acquire ACCESS EXCLUSIVE locks before its data preflight")
			}
			lockTables := strings.Split(sql[lockMatch[2]:lockMatch[3]], ",")
			for i := range lockTables {
				lockTables[i] = strings.TrimSpace(lockTables[i])
			}
			assertSameStrings(t, "locked tables", test.dataTables, lockTables)

			blockMatch := blockPattern.FindStringSubmatchIndex(sql)
			if blockMatch == nil {
				t.Fatal("rollback must use a preflight block that can reject populated tables")
			}
			if lockMatch[1] > blockMatch[0] {
				t.Fatal("table locks must be acquired before the preflight checks rows")
			}
			preflight := sql[blockMatch[2]:blockMatch[3]]
			compact := strings.Join(strings.Fields(preflight), " ")

			checks := fromPattern.FindAllStringSubmatch(preflight, -1)
			checkedTables := make([]string, 0, len(checks))
			for _, check := range checks {
				checkedTables = append(checkedTables, check[1])
			}
			assertSameStrings(t, "preflight tables", test.dataTables, checkedTables)
			if got := existsPattern.FindAllStringIndex(preflight, -1); len(got) != len(test.dataTables) {
				t.Fatalf("preflight has %d EXISTS checks; want one for each of %d protected tables", len(got), len(test.dataTables))
			}

			conditions := make([]string, 0, len(test.dataTables))
			for _, table := range test.dataTables {
				conditions = append(conditions, "exists (select 1 from "+table+")")
			}
			guard := "if " + strings.Join(conditions, " or ") + " then"
			if !strings.Contains(compact, guard) {
				t.Fatal("every non-empty protected table must trigger the same refusal branch")
			}
			raiseAt := strings.Index(compact, "raise exception")
			endIfAt := strings.Index(compact, "end if")
			if raiseAt < len(guard) || endIfAt < raiseAt {
				t.Fatal("the non-empty-table guard must fail closed with a RAISE EXCEPTION")
			}
			messageEnd := strings.Index(compact[raiseAt:], ";")
			if messageEnd < 0 {
				t.Fatal("RAISE EXCEPTION must have a clear message")
			}
			message := compact[raiseAt : raiseAt+messageEnd]
			if !strings.Contains(message, "refused") || !strings.Contains(message, "data exists") {
				t.Fatalf("exception must explain that populated data blocks rollback; got %q", message)
			}

			drops := dropPattern.FindAllStringSubmatch(sql, -1)
			droppedTables := make([]string, 0, len(drops))
			for _, drop := range drops {
				droppedTables = append(droppedTables, drop[1])
			}
			assertSameStrings(t, "dropped tables", test.dropTables, droppedTables)
			firstDestructive := destructivePattern.FindStringIndex(sql)
			if firstDestructive == nil || blockMatch[1] > firstDestructive[0] {
				t.Fatal("the complete data preflight must finish before any destructive DDL")
			}
		})
	}
}

func assertSameStrings(t *testing.T, description string, want, got []string) {
	t.Helper()
	want = append([]string(nil), want...)
	got = append([]string(nil), got...)
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Fatalf("%s mismatch: got %v, want %v", description, got, want)
	}
}

func TestOperationalRollbackPreflightsLockProtectedData(t *testing.T) {
	tests := []struct {
		migration string
		tables    []string
	}{
		{
			migration: "pre/0087_source_manual_sync_jobs.down.sql",
			tables:    []string{"public.source_sync_jobs", "public.durable_jobs"},
		},
		{
			migration: "pre/0088_source_extraction_correction_outbox.down.sql",
			tables:    []string{"public.source_extraction_corrections"},
		},
		{
			migration: "pre/0089_openclaw_reconcile_fairness_and_cancellation_retry.down.sql",
			tables: []string{
				"openclaw_gateway_session_receipts",
				"openclaw_gateway_reconcile_cursors",
			},
		},
		{
			migration: "pre/0101_llm_model_maintenance_admission_claims.down.sql",
			tables:    []string{"public.llm_model_maintenance_admission_claims"},
		},
		{
			migration: "pre/0102_model_telemetry_usage_source.down.sql",
			tables:    []string{"public.model_run_telemetries"},
		},
		{
			migration: "pre/0109_trello_webhook_reconciliation_generations.down.sql",
			tables: []string{
				"public.trello_webhook_receipts",
				"public.trello_webhook_reconciliation_states",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.migration, func(t *testing.T) {
			sql, err := Files.ReadFile(test.migration)
			if err != nil {
				t.Fatalf("read migration: %v", err)
			}
			normalized := strings.ToLower(string(sql))
			checkAt := strings.Index(normalized, "if exists")
			if checkAt < 0 {
				t.Fatal("rollback migration has no data preflight")
			}
			lockAt := strings.Index(normalized, "lock table")
			if lockAt < 0 || lockAt > checkAt {
				t.Fatal("rollback must acquire its table lock before checking whether operational data exists")
			}
			lockClause := normalized[lockAt:checkAt]
			if !strings.Contains(lockClause, "access exclusive mode") {
				t.Fatal("rollback preflight must hold an exclusive table lock until its transaction ends")
			}
			for _, table := range test.tables {
				if !strings.Contains(lockClause, table) {
					t.Fatalf("rollback must lock %s before the data preflight", table)
				}
			}
		})
	}
}
