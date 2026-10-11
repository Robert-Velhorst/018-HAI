//go:build integration

package migrations_test

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"testing/fstest"
)

const (
	protectedTaskStateMigration     = "pre/0004_task_state_storage"
	protectedAuthorizationMigration = "pre/0014_unified_execution_authorization"
	protectedHostRuntimeMigration   = "pre/0065_host_runtime_jobs"
	accountFeedRegistryRollback     = "pre/0110_account_feed_registry"
	protectedRollbackMarker         = 73
)

func TestAccountFeedRegistryRollbackRefusesDataAndAllowsEmptySchema(t *testing.T) {
	for _, test := range []struct {
		name   string
		insert func(*testing.T, *gorm.DB)
	}{
		{
			name: "feed configuration",
			insert: func(t *testing.T, db *gorm.DB) {
				id := uuid.New()
				execRollbackFixture(t, db, `INSERT INTO public.account_feeds
					(id, owner_user_id, workspace_id, name, provider, source_type, enabled)
					VALUES (?, 'rollback-owner', 'rollback-workspace', 'feed', 'manual', 'local_json_file', true)`, id)
			},
		},
		{
			name: "audit history",
			insert: func(t *testing.T, db *gorm.DB) {
				feedID := uuid.New()
				execRollbackFixture(t, db, `INSERT INTO public.account_feeds
					(id, owner_user_id, workspace_id, name, provider, source_type, enabled)
					VALUES (?, 'rollback-owner', 'rollback-workspace', 'feed', 'manual', 'local_json_file', true)`, feedID)
				execRollbackFixture(t, db, `INSERT INTO public.account_feed_audits
					(id, feed_id, owner_user_id, workspace_id, event_type, message, created_at)
					VALUES (?, ?, 'rollback-owner', 'rollback-workspace', 'sync', 'preserve this audit', now())`, uuid.New(), feedID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openIsolatedMigrationDatabase(t)
			files := applyRealAccountFeedRegistryMigration(t, db)
			test.insert(t, db)
			var appliedAt time.Time
			if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, accountFeedRegistryRollback).Row().Scan(&appliedAt); err != nil {
				t.Fatalf("read migration ledger timestamp: %v", err)
			}

			err := infra.RollbackMigration(db, files, "pre", accountFeedRegistryRollback)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "account feed configuration or audit history exists") {
				t.Fatalf("rollback error = %v, want account-feed data refusal", err)
			}
			assertRollbackRelation(t, db, "account_feeds", true)
			assertRollbackRelation(t, db, "account_feed_audits", true)
			var feedCount, auditCount int64
			if err := db.Table("public.account_feeds").Count(&feedCount).Error; err != nil {
				t.Fatalf("count preserved feeds: %v", err)
			}
			if err := db.Table("public.account_feed_audits").Count(&auditCount).Error; err != nil {
				t.Fatalf("count preserved feed audits: %v", err)
			}
			if feedCount == 0 || (test.name == "audit history" && auditCount == 0) {
				t.Fatalf("rollback lost feed data: feeds=%d audits=%d", feedCount, auditCount)
			}
			assertRollbackMigrationLedger(t, db, accountFeedRegistryRollback, true)
			var after time.Time
			if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, accountFeedRegistryRollback).Row().Scan(&after); err != nil || !after.Equal(appliedAt) {
				t.Fatalf("migration ledger timestamp changed after refusal: before=%s after=%s err=%v", appliedAt, after, err)
			}
		})
	}

	t.Run("empty schema rolls back", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		files := applyRealAccountFeedRegistryMigration(t, db)
		if err := infra.RollbackMigration(db, files, "pre", accountFeedRegistryRollback); err != nil {
			t.Fatalf("rollback empty account-feed migration: %v", err)
		}
		assertRollbackRelation(t, db, "account_feeds", false)
		assertRollbackRelation(t, db, "account_feed_audits", false)
		assertRollbackMigrationLedger(t, db, accountFeedRegistryRollback, false)
	})
}

func applyRealAccountFeedRegistryMigration(t *testing.T, db *gorm.DB) fs.FS {
	t.Helper()
	up, err := migrations.Files.ReadFile("pre/0110_account_feed_registry.up.sql")
	if err != nil {
		t.Fatalf("read account-feed migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0110_account_feed_registry.down.sql")
	if err != nil {
		t.Fatalf("read account-feed rollback: %v", err)
	}
	files := fstest.MapFS{
		"pre":                                   &fstest.MapFile{Mode: fs.ModeDir},
		"post":                                  &fstest.MapFile{Mode: fs.ModeDir},
		"pre/0110_account_feed_registry.up.sql": {Data: up, Mode: 0o600},
		"pre/0110_account_feed_registry.down.sql": {Data: down, Mode: 0o600},
	}
	if count, err := infra.ApplyMigrations(db, files, "pre"); err != nil || count != 1 {
		t.Fatalf("apply real account-feed migration: count=%d err=%v", count, err)
	}
	return files
}

func TestProtectedDataRollbacksRefuseWithoutLosingRowsOrLedger(t *testing.T) {
	tests := []struct {
		name      string
		migration string
		table     string
		message   string
	}{
		{name: "0004 completion plan logs", migration: protectedTaskStateMigration, table: "task_completion_plan_logs", message: "task state data exists"},
		{name: "0004 review items", migration: protectedTaskStateMigration, table: "task_review_items", message: "task state data exists"},
		{name: "0004 review decisions", migration: protectedTaskStateMigration, table: "task_review_decisions", message: "task state data exists"},
		{name: "0014 authorization consumptions", migration: protectedAuthorizationMigration, table: "execution_authorization_consumptions", message: "authorization or workflow decision data exists"},
		{name: "0014 final effect exercises", migration: protectedAuthorizationMigration, table: "execution_authorization_final_effect_exercises", message: "authorization or workflow decision data exists"},
		{name: "0014 authorization receipts", migration: protectedAuthorizationMigration, table: "execution_authorization_receipts", message: "authorization or workflow decision data exists"},
		{name: "0014 workflow decisions", migration: protectedAuthorizationMigration, table: "workflow_decisions", message: "authorization or workflow decision data exists"},
		{name: "0065 host runtime jobs", migration: protectedHostRuntimeMigration, table: "host_runtime_jobs", message: "host runtime job data exists"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openIsolatedMigrationDatabase(t)
			createProtectedRollbackFixture(t, db, test.migration)
			files := applyRollbackFixtureMigration(t, db, test.migration)
			execRollbackFixture(t, db, "INSERT INTO public."+test.table+" (marker) VALUES ("+strconv.Itoa(protectedRollbackMarker)+")")
			appliedAt := protectedRollbackAppliedAt(t, db, test.migration)

			err := infra.RollbackMigration(db, files, "pre", test.migration)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.message) {
				t.Fatalf("rollback error = %v, want refusal containing %q", err, test.message)
			}
			assertProtectedRollbackRow(t, db, test.table)
			assertRollbackMigrationLedger(t, db, test.migration, true)
			if got := protectedRollbackAppliedAt(t, db, test.migration); !got.Equal(appliedAt) {
				t.Fatalf("migration ledger applied_at changed from %s to %s", appliedAt, got)
			}
			if test.table == "workflow_decisions" {
				assertRollbackColumn(t, db, "workflow_decisions", "owner_identity", true)
			}
		})
	}
}

func TestProtectedDataRollbacksSucceedWhenProtectedTablesAreEmpty(t *testing.T) {
	tests := []struct {
		name      string
		migration string
		tables    []string
	}{
		{
			name:      "0004 task state storage",
			migration: protectedTaskStateMigration,
			tables:    []string{"task_completion_plan_logs", "task_review_items", "task_review_decisions"},
		},
		{
			name:      "0014 unified execution authorization",
			migration: protectedAuthorizationMigration,
			tables: []string{
				"execution_authorization_consumptions",
				"execution_authorization_final_effect_exercises",
				"execution_authorization_receipts",
			},
		},
		{name: "0065 host runtime jobs", migration: protectedHostRuntimeMigration, tables: []string{"host_runtime_jobs"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openIsolatedMigrationDatabase(t)
			createProtectedRollbackFixture(t, db, test.migration)
			files := applyRollbackFixtureMigration(t, db, test.migration)

			if err := infra.RollbackMigration(db, files, "pre", test.migration); err != nil {
				t.Fatalf("rollback empty %s migration: %v", test.migration, err)
			}
			assertRollbackMigrationLedger(t, db, test.migration, false)
			for _, table := range test.tables {
				assertRollbackRelation(t, db, table, false)
			}
			if test.migration == protectedAuthorizationMigration {
				assertRollbackColumn(t, db, "workflow_decisions", "owner_identity", false)
			}
		})
	}
}

func createProtectedRollbackFixture(t *testing.T, db *gorm.DB, migration string) {
	t.Helper()
	var tables []string
	switch migration {
	case protectedTaskStateMigration:
		tables = []string{"task_completion_plan_logs", "task_review_items", "task_review_decisions"}
	case protectedAuthorizationMigration:
		tables = []string{
			"execution_authorization_consumptions",
			"execution_authorization_final_effect_exercises",
			"execution_authorization_receipts",
		}
		for _, table := range tables {
			execRollbackFixture(t, db, "CREATE TABLE public."+table+" (marker integer PRIMARY KEY)")
		}
		execRollbackFixture(t, db, `CREATE TABLE public.workflow_decisions (
			marker integer PRIMARY KEY, owner_identity text)`)
		for _, table := range []string{
			"workflow_items",
			"task_review_decisions",
			"standing_mandate_authorization_decisions",
			"robert_constitution_versions",
		} {
			execRollbackFixture(t, db, "CREATE TABLE public."+table+" (marker integer PRIMARY KEY)")
		}
		return
	case protectedHostRuntimeMigration:
		tables = []string{"host_runtime_jobs"}
	default:
		t.Fatalf("unknown protected rollback migration %q", migration)
	}

	for _, table := range tables {
		execRollbackFixture(t, db, "CREATE TABLE public."+table+" (marker integer PRIMARY KEY)")
	}
}

func assertProtectedRollbackRow(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var marker int
	if err := db.Raw("SELECT marker FROM public." + table).Row().Scan(&marker); err != nil {
		t.Fatalf("read protected row from %s: %v", table, err)
	}
	if marker != protectedRollbackMarker {
		t.Fatalf("protected row marker in %s = %d, want %d", table, marker, protectedRollbackMarker)
	}
	var count int64
	if err := db.Raw("SELECT count(*) FROM public." + table).Row().Scan(&count); err != nil {
		t.Fatalf("count protected rows in %s: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("protected rows in %s = %d, want 1", table, count)
	}
}

func protectedRollbackAppliedAt(t *testing.T, db *gorm.DB, migration string) time.Time {
	t.Helper()
	var appliedAt time.Time
	if err := db.Raw("SELECT applied_at FROM public.schema_migrations WHERE version = ?", migration).Row().Scan(&appliedAt); err != nil {
		t.Fatalf("read applied_at for %s: %v", migration, err)
	}
	return appliedAt
}
