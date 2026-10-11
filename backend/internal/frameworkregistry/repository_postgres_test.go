//go:build integration

package frameworkregistry

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const frameworkRegistryMigrationVersion = "pre/0003_framework_registry"

func frameworkRegistryLifecycleMigrations(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, version := range []string{
		"0001_extensions",
		"0002_baseline",
		"0003_framework_registry",
		"0004_task_state_storage",
		"0005_framework_operating_contract",
		"0006_durable_job_fencing",
	} {
		for _, suffix := range []string{"up.sql", "down.sql"} {
			path := "pre/" + version + "." + suffix
			data, err := migrations.Files.ReadFile(path)
			if err != nil {
				t.Fatalf("read lifecycle migration %s: %v", path, err)
			}
			files[path] = &fstest.MapFile{Data: data}
		}
	}
	return files
}

func openFrameworkRegistryPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	destructiveFlag := os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")
	if !strings.EqualFold(strings.TrimSpace(destructiveFlag), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for destructive Postgres integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if err := validateDestructiveIntegrationTarget(destructiveFlag, databaseName); err != nil {
		t.Fatalf("refusing destructive integration setup: %v", err)
	}
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return db
}

func executeEmbeddedMigration(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	sql, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if err := db.Exec(string(sql)).Error; err != nil {
		t.Fatalf("execute migration %s: %v", path, err)
	}
}

func relationExists(t *testing.T, db *gorm.DB, relation string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", "public."+relation).Scan(&exists).Error; err != nil {
		t.Fatalf("query relation %s: %v", relation, err)
	}
	return exists
}

func functionExists(t *testing.T, db *gorm.DB, functionName string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public' AND p.proname = ?
		)
	`, functionName).Scan(&exists).Error; err != nil {
		t.Fatalf("query function %s: %v", functionName, err)
	}
	return exists
}

func triggerCount(t *testing.T, db *gorm.DB, triggerName string) int64 {
	t.Helper()
	var count int64
	if err := db.Raw(`
		SELECT count(*)
		FROM pg_trigger
		WHERE tgname = ? AND NOT tgisinternal
	`, triggerName).Scan(&count).Error; err != nil {
		t.Fatalf("query trigger %s: %v", triggerName, err)
	}
	return count
}

func expectDatabaseRejection(t *testing.T, label string, operation func() error) {
	t.Helper()
	if err := operation(); err == nil {
		t.Fatalf("Postgres accepted %s", label)
	}
}

func TestFrameworkRegistryPostgresIntegrationRequiredEnvironment(t *testing.T) {
	if !strings.EqualFold(
		strings.TrimSpace(os.Getenv("HAI_REQUIRE_POSTGRES_INTEGRATION")),
		"true",
	) {
		t.Skip("HAI_REQUIRE_POSTGRES_INTEGRATION is not enabled")
	}
	if strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN")) == "" {
		t.Fatal("required Postgres integration run has no HAI_TEST_DATABASE_DSN")
	}
	if !strings.EqualFold(
		strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")),
		"true",
	) {
		t.Fatal("required Postgres integration run has not enabled HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")
	}
}

func TestFrameworkRegistryPostgresMigrationApplyRollbackAndRerun(t *testing.T) {
	db := openFrameworkRegistryPostgresTestDB(t)
	files := frameworkRegistryLifecycleMigrations(t)

	applied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	if applied < 3 {
		t.Fatalf("applied %d pre migrations, want at least 3", applied)
	}
	if rerun, err := infra.ApplyMigrations(db, files, "pre"); err != nil || rerun != 0 {
		t.Fatalf("migration runner rerun = %d, %v; want 0, nil", rerun, err)
	}

	for _, table := range []string{
		"framework_preferences",
		"framework_selection_records",
		"robert_constitution_versions",
	} {
		if !relationExists(t, db, table) {
			t.Fatalf("expected %s after migration", table)
		}
	}
	for _, functionName := range []string{
		"hai_reject_framework_selection_mutation",
		"hai_reject_framework_registry_truncate",
		"hai_enforce_constitution_lifecycle",
		"hai_require_active_constitution_after_history",
	} {
		if !functionExists(t, db, functionName) {
			t.Fatalf("expected function %s after migration", functionName)
		}
	}

	// Re-executing the SQL itself must not duplicate triggers or fail. This
	// catches defects that the schema_migrations ledger would otherwise hide.
	executeEmbeddedMigration(t, db, "pre/0003_framework_registry.up.sql")
	executeEmbeddedMigration(t, db, "pre/0003_framework_registry.up.sql")
	for _, triggerName := range []string{
		"trg_framework_selection_records_immutable",
		"trg_framework_selection_records_no_truncate",
		"trg_robert_constitution_versions_lifecycle",
		"trg_robert_constitution_active_history",
		"trg_robert_constitution_versions_no_truncate",
	} {
		if count := triggerCount(t, db, triggerName); count != 1 {
			t.Fatalf("trigger %s count = %d, want 1", triggerName, count)
		}
	}

	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		frameworkRegistryMigrationVersion,
	); err == nil || !strings.Contains(err.Error(), "rollback later migrations first") {
		t.Fatalf("out-of-order rollback error = %v, want later-migration rejection", err)
	}
	if !relationExists(t, db, "framework_selection_records") {
		t.Fatal("rejected out-of-order rollback changed the registry schema")
	}

	for _, version := range []string{
		"pre/0006_durable_job_fencing",
		"pre/0005_framework_operating_contract",
		"pre/0004_task_state_storage",
		frameworkRegistryMigrationVersion,
	} {
		if err := infra.RollbackMigration(
			db,
			files,
			"pre",
			version,
		); err != nil {
			t.Fatalf("rollback %s: %v", version, err)
		}
	}
	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		frameworkRegistryMigrationVersion,
	); err == nil || !strings.Contains(err.Error(), "is not applied") {
		t.Fatalf("repeated rollback error = %v, want not-applied rejection", err)
	}
	for _, table := range []string{
		"framework_preferences",
		"framework_selection_records",
		"robert_constitution_versions",
	} {
		if relationExists(t, db, table) {
			t.Fatalf("%s survived rollback", table)
		}
	}
	for _, functionName := range []string{
		"hai_reject_framework_selection_mutation",
		"hai_reject_framework_registry_truncate",
		"hai_enforce_constitution_lifecycle",
		"hai_require_active_constitution_after_history",
	} {
		if functionExists(t, db, functionName) {
			t.Fatalf("function %s survived rollback", functionName)
		}
	}
	if !relationExists(t, db, "pursuits") {
		t.Fatal("framework registry rollback removed baseline schema")
	}

	// The down SQL is intentionally safe to run again after all objects are
	// gone. This protects manual recovery and repeated local test teardown.
	executeEmbeddedMigration(t, db, "pre/0003_framework_registry.down.sql")
	reapplied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("reapply framework registry migration: %v", err)
	}
	if reapplied != 4 {
		t.Fatalf("reapplied %d migrations, want 4", reapplied)
	}
	if !relationExists(t, db, "framework_selection_records") {
		t.Fatal("framework registry schema was not restored")
	}
}

func TestFrameworkRegistryPostgresOwnerScopeConstraintsAndHistory(t *testing.T) {
	db := openFrameworkRegistryPostgresTestDB(t)
	executeEmbeddedMigration(t, db, "pre/0001_extensions.up.sql")
	executeEmbeddedMigration(t, db, "pre/0003_framework_registry.up.sql")
	executeEmbeddedMigration(t, db, "pre/0005_framework_operating_contract.up.sql")
	executeEmbeddedMigration(t, db, "pre/0029_framework_selector_v5_digest.up.sql")
	repo := NewGormRepository(db)

	t.Run("preferences are owner scoped and unique per owner and framework", func(t *testing.T) {
		if _, err := repo.UpsertPreference("alice", Preference{
			FrameworkID: "truth-evidence",
			State:       PreferenceEnabled,
			Pinned:      true,
		}); err != nil {
			t.Fatalf("create alice preference: %v", err)
		}
		if _, err := repo.UpsertPreference("bob", Preference{
			FrameworkID: "truth-evidence",
			State:       PreferenceDisabled,
		}); err != nil {
			t.Fatalf("create bob preference: %v", err)
		}
		alice, err := repo.ListPreferences("alice")
		if err != nil || len(alice) != 1 || alice[0].State != PreferenceEnabled {
			t.Fatalf("alice preferences = %#v, %v", alice, err)
		}
		bob, err := repo.ListPreferences("bob")
		if err != nil || len(bob) != 1 || bob[0].State != PreferenceDisabled {
			t.Fatalf("bob preferences = %#v, %v", bob, err)
		}

		var row models.FrameworkPreference
		if err := db.Where(
			"owner_identity = ? AND framework_id = ?",
			"alice",
			"truth-evidence",
		).First(&row).Error; err != nil {
			t.Fatalf("read preference row: %v", err)
		}
		row.ID = uuid.New()
		expectDatabaseRejection(t, "duplicate owner/framework preference", func() error {
			return db.Create(&row).Error
		})
		row.ID = uuid.New()
		row.OwnerIdentity = " "
		expectDatabaseRejection(t, "blank preference owner", func() error {
			return db.Create(&row).Error
		})
		row.ID = uuid.New()
		row.OwnerIdentity = "charlie"
		row.State = "unsafe"
		expectDatabaseRejection(t, "invalid preference state", func() error {
			return db.Create(&row).Error
		})
		row.ID = uuid.New()
		row.State = PreferenceEnabled
		invalidAutonomy := 11
		row.MaximumAutonomyLevel = &invalidAutonomy
		expectDatabaseRejection(t, "out-of-range preference autonomy", func() error {
			return db.Create(&row).Error
		})
		row.ID = uuid.New()
		row.MaximumAutonomyLevel = nil
		row.AdaptationsJSON = `{"not":"an array"}`
		expectDatabaseRejection(t, "non-array preference adaptations", func() error {
			return db.Create(&row).Error
		})
	})

	t.Run("selection history is owner scoped append only and constrained", func(t *testing.T) {
		aliceDecision := postgresSelectionDecision("alice-task")
		if err := repo.CreateSelection(
			"alice",
			aliceDecision,
			postgresDigest("alice request"),
			"Review source evidence.",
		); err != nil {
			t.Fatalf("create alice selection: %v", err)
		}
		bobDecision := postgresSelectionDecision("bob-task")
		if err := repo.CreateSelection(
			"bob",
			bobDecision,
			postgresDigest("bob request"),
			"Review a different source.",
		); err != nil {
			t.Fatalf("create bob selection: %v", err)
		}
		alice, err := repo.ListSelections("alice", 20)
		if err != nil || len(alice) != 1 || alice[0].ID != aliceDecision.ID {
			t.Fatalf("alice selections = %#v, %v", alice, err)
		}
		bob, err := repo.ListSelections("bob", 20)
		if err != nil || len(bob) != 1 || bob[0].ID != bobDecision.ID {
			t.Fatalf("bob selections = %#v, %v", bob, err)
		}
		exact, err := repo.GetSelection(context.Background(), "alice", aliceDecision.ID)
		if err != nil || exact.ID != aliceDecision.ID {
			t.Fatalf("exact alice selection = %#v, %v", exact, err)
		}
		if _, err := repo.GetSelection(context.Background(), "bob", aliceDecision.ID); err == nil {
			t.Fatal("exact selection lookup crossed the owner boundary")
		}

		var row models.FrameworkSelectionRecord
		if err := db.Where("id = ?", aliceDecision.ID).First(&row).Error; err != nil {
			t.Fatalf("read selection row: %v", err)
		}
		expectDatabaseRejection(t, "selection reproducibility update", func() error {
			return db.Model(&models.FrameworkSelectionRecord{}).
				Where("id = ?", row.ID).
				Update("catalog_digest", postgresDigest("tampered")).Error
		})
		expectDatabaseRejection(t, "selection deletion", func() error {
			return db.Delete(&models.FrameworkSelectionRecord{}, "id = ?", row.ID).Error
		})
		expectDatabaseRejection(t, "selection history truncate", func() error {
			return db.Exec("TRUNCATE TABLE public.framework_selection_records").Error
		})

		duplicate := row
		expectDatabaseRejection(t, "duplicate selection UUID", func() error {
			return db.Create(&duplicate).Error
		})
		mismatchedSource := selectionRowCopy(row)
		mismatchedSource.ConstitutionSource = "builtin-robert-constitution-v1:v2"
		expectDatabaseRejection(t, "mismatched Constitution source version", func() error {
			return db.Create(&mismatchedSource).Error
		})
		blankDomain := selectionRowCopy(row)
		blankDomain.LifeDomain = " "
		expectDatabaseRejection(t, "blank selection life domain", func() error {
			return db.Create(&blankDomain).Error
		})
		invalidSelected := selectionRowCopy(row)
		invalidSelected.SelectedJSON = `{"not":"an array"}`
		expectDatabaseRejection(t, "non-array selected framework payload", func() error {
			return db.Create(&invalidSelected).Error
		})

		var count int64
		if err := db.Model(&models.FrameworkSelectionRecord{}).
			Where("id = ? AND catalog_digest = ?", row.ID, row.CatalogDigest).
			Count(&count).Error; err != nil {
			t.Fatalf("verify immutable selection: %v", err)
		}
		if count != 1 {
			t.Fatal("selection row changed after rejected mutations")
		}
	})

	t.Run("Constitution lifecycle is owner scoped immutable and single active", func(t *testing.T) {
		aliceV1, err := repo.CreateConstitution("alice", postgresConstitution(1, "Alice version one"))
		if err != nil {
			t.Fatalf("create alice v1: %v", err)
		}
		if _, err := repo.ActivateConstitution(
			"bob",
			aliceV1.ID,
			"bob",
			"Bob cannot approve Alice.",
			time.Now().UTC(),
		); err == nil {
			t.Fatal("wrong owner activated Alice Constitution")
		}
		if _, err := repo.ActivateConstitution(
			"alice",
			aliceV1.ID,
			"alice",
			"Alice reviewed version one.",
			time.Now().UTC(),
		); err != nil {
			t.Fatalf("activate alice v1: %v", err)
		}

		aliceV2, err := repo.CreateConstitution("alice", postgresConstitution(2, "Alice version two"))
		if err != nil {
			t.Fatalf("create alice v2: %v", err)
		}
		var v2 models.RobertConstitutionVersion
		if err := db.Where("id = ?", aliceV2.ID).First(&v2).Error; err != nil {
			t.Fatalf("read alice v2: %v", err)
		}
		expectDatabaseRejection(t, "draft Constitution content update", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", v2.ID).
				Update("values_json", `["tampered"]`).Error
		})
		expectDatabaseRejection(t, "draft Constitution base version update", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", v2.ID).
				Update("base_version", 0).Error
		})
		expectDatabaseRejection(t, "draft to superseded transition", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", v2.ID).
				Update("status", ConstitutionSuperseded).Error
		})

		directActive := constitutionRowCopy(v2)
		directActive.Status = ConstitutionActive
		directActive.ApprovedBy = "alice"
		directActive.ApprovalNote = "Bypass activation"
		approvedAt := time.Now().UTC()
		directActive.ApprovedAt = &approvedAt
		expectDatabaseRejection(t, "direct active Constitution insertion", func() error {
			return db.Create(&directActive).Error
		})
		draftWithApproval := constitutionRowCopy(v2)
		draftWithApproval.ApprovedBy = "alice"
		draftWithApproval.ApprovalNote = "Premature approval"
		draftWithApproval.ApprovedAt = &approvedAt
		expectDatabaseRejection(t, "draft Constitution with approval metadata", func() error {
			return db.Create(&draftWithApproval).Error
		})
		blankSummary := constitutionRowCopy(v2)
		blankSummary.ChangeSummary = " "
		expectDatabaseRejection(t, "blank Constitution change summary", func() error {
			return db.Create(&blankSummary).Error
		})

		expectDatabaseRejection(t, "second active Constitution for one owner", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", v2.ID).
				Updates(map[string]interface{}{
					"status":        ConstitutionActive,
					"approved_by":   "alice",
					"approval_note": "Would conflict with active v1.",
					"approved_at":   approvedAt,
				}).Error
		})
		if _, err := repo.ActivateConstitution(
			"alice",
			aliceV2.ID,
			"alice",
			"Alice reviewed version two.",
			approvedAt,
		); err != nil {
			t.Fatalf("activate alice v2: %v", err)
		}
		expectDatabaseRejection(t, "active Constitution cannot be superseded without a replacement", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", aliceV2.ID).
				Update("status", ConstitutionSuperseded).Error
		})

		bobV1, err := repo.CreateConstitution("bob", postgresConstitution(1, "Bob version one"))
		if err != nil {
			t.Fatalf("create bob v1: %v", err)
		}
		if _, err := repo.ActivateConstitution(
			"bob",
			bobV1.ID,
			"bob",
			"Bob reviewed version one.",
			approvedAt,
		); err != nil {
			t.Fatalf("activate bob v1: %v", err)
		}

		alice, err := repo.ListConstitutions("alice")
		if err != nil || len(alice) != 2 ||
			alice[0].Status != ConstitutionActive ||
			alice[1].Status != ConstitutionSuperseded {
			t.Fatalf("alice Constitutions = %#v, %v", alice, err)
		}
		bob, err := repo.ListConstitutions("bob")
		if err != nil || len(bob) != 1 || bob[0].Status != ConstitutionActive {
			t.Fatalf("bob Constitutions = %#v, %v", bob, err)
		}
		expectDatabaseRejection(t, "superseded Constitution reactivation", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", aliceV1.ID).
				Update("status", ConstitutionActive).Error
		})
		expectDatabaseRejection(t, "Constitution deletion", func() error {
			return db.Delete(&models.RobertConstitutionVersion{}, "id = ?", aliceV1.ID).Error
		})
		expectDatabaseRejection(t, "Constitution history truncate", func() error {
			return db.Exec("TRUNCATE TABLE public.robert_constitution_versions").Error
		})

		duplicateVersion := postgresConstitution(2, "Duplicate Alice version")
		expectDatabaseRejection(t, "duplicate owner Constitution version", func() error {
			_, err := repo.CreateConstitution("alice", duplicateVersion)
			return err
		})
	})

	t.Run("stale Constitution activation is rejected after repository restart", func(t *testing.T) {
		first, err := repo.CreateConstitution(
			"restart-owner",
			postgresConstitution(1, "Restart owner version one"),
		)
		if err != nil {
			t.Fatalf("create version one: %v", err)
		}
		if _, err := repo.ActivateConstitution(
			"restart-owner",
			first.ID,
			"restart-owner",
			"Reviewed version one.",
			time.Now().UTC(),
		); err != nil {
			t.Fatalf("activate version one: %v", err)
		}
		current, err := repo.CreateConstitution(
			"restart-owner",
			postgresConstitution(2, "Current amendment"),
		)
		if err != nil {
			t.Fatalf("create current amendment: %v", err)
		}
		staleDraft := postgresConstitution(3, "Stale competing amendment")
		staleDraft.BaseVersion = 1
		stale, err := repo.CreateConstitution("restart-owner", staleDraft)
		if err != nil {
			t.Fatalf("create stale amendment: %v", err)
		}
		if _, err := repo.ActivateConstitution(
			"restart-owner",
			current.ID,
			"restart-owner",
			"Reviewed current amendment.",
			time.Now().UTC(),
		); err != nil {
			t.Fatalf("activate current amendment: %v", err)
		}

		restarted := NewGormRepository(db)
		if _, err := restarted.ActivateConstitution(
			"restart-owner",
			stale.ID,
			"restart-owner",
			"Attempted stale activation.",
			time.Now().UTC(),
		); err == nil || !strings.Contains(strings.ToLower(err.Error()), "stale") {
			t.Fatalf("stale repository activation returned %v", err)
		}
		expectDatabaseRejection(t, "direct stale Constitution activation", func() error {
			return db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", stale.ID).
				Updates(map[string]interface{}{
					"status":        ConstitutionActive,
					"approved_by":   "restart-owner",
					"approval_note": "Attempted SQL bypass.",
					"approved_at":   time.Now().UTC(),
				}).Error
		})
		constitutions, err := restarted.ListConstitutions("restart-owner")
		if err != nil {
			t.Fatalf("list restart-owner Constitutions: %v", err)
		}
		if len(constitutions) != 3 ||
			constitutions[0].Status != ConstitutionDraft ||
			constitutions[1].Status != ConstitutionActive ||
			constitutions[2].Status != ConstitutionSuperseded {
			t.Fatalf("stale activation changed lifecycle: %#v", constitutions)
		}
	})

	t.Run("declared checks and deliberate external references are explicit", func(t *testing.T) {
		expectedChecks := []string{
			"chk_framework_preferences_owner",
			"chk_framework_preferences_framework",
			"chk_framework_preferences_state",
			"chk_framework_preferences_autonomy",
			"chk_framework_preferences_adaptations_array",
			"chk_framework_selection_records_constitution_source",
			"chk_framework_selection_records_life_domain",
			"chk_framework_selection_records_need",
			"chk_framework_selection_records_authority",
			"chk_framework_selection_records_reason",
			"chk_framework_selection_records_task_risk_level",
			"chk_framework_selection_records_effective_risk_ceiling",
			"chk_framework_selection_records_v5_risk_contract",
			"chk_framework_selection_records_risk_ceiling_rank",
			"chk_robert_constitution_approval",
			"chk_robert_constitution_base_version",
			"chk_robert_constitution_change_summary",
		}
		var checks []string
		if err := db.Raw(`
			SELECT conname
			FROM pg_constraint
			WHERE contype = 'c'
			  AND conrelid IN (
				'public.framework_preferences'::regclass,
				'public.framework_selection_records'::regclass,
				'public.robert_constitution_versions'::regclass
			  )
		`).Scan(&checks).Error; err != nil {
			t.Fatalf("query check constraints: %v", err)
		}
		available := make(map[string]bool, len(checks))
		for _, check := range checks {
			available[check] = true
		}
		for _, expected := range expectedChecks {
			if !available[expected] {
				t.Fatalf("missing check constraint %s; available = %#v", expected, checks)
			}
		}

		// These records deliberately have no database FK: owner identity is
		// issued by the external IDP, framework IDs are code-owned catalog IDs,
		// task_plan_id is an optional cross-engine correlation key, and the
		// built-in Constitution has no database row. Assert that a later schema
		// change cannot silently add a cascading or mismatched relationship.
		var foreignKeys int64
		if err := db.Raw(`
			SELECT count(*)
			FROM pg_constraint
			WHERE contype = 'f'
			  AND conrelid IN (
				'public.framework_preferences'::regclass,
				'public.framework_selection_records'::regclass,
				'public.robert_constitution_versions'::regclass
			  )
		`).Scan(&foreignKeys).Error; err != nil {
			t.Fatalf("query foreign key constraints: %v", err)
		}
		if foreignKeys != 0 {
			t.Fatalf("framework registry has %d unexpected foreign keys", foreignKeys)
		}
	})
}

func postgresSelectionDecision(taskPlanID string) SelectionDecision {
	return SelectionDecision{
		ID:                        uuid.NewString(),
		TaskPlanID:                taskPlanID,
		CreatedAt:                 time.Now().UTC(),
		CatalogVersion:            "v1",
		CatalogDigest:             postgresDigest("catalog"),
		SelectorAlgorithmVersion:  "selector-v2",
		EffectivePreferenceDigest: postgresDigest("preferences"),
		ConstitutionDigest:        postgresDigest("constitution"),
		OperatingContractDigest:   postgresDigest("operating-contract"),
		LifeDomain:                "work",
		NeedOrCommitment:          "Verify source evidence",
		Selected: []SelectedFramework{{
			ID:                   "truth-evidence",
			Version:              "1.0.0",
			Name:                 "Truth and evidence",
			Family:               "verification",
			Score:                1,
			Reasons:              []string{"Source evidence is required."},
			MaximumAutonomyLevel: 2,
			AuthorityRequirement: "recommend only",
			EvidenceRequirements: []string{"source record"},
			EvaluationMethod:     []string{"claim support"},
		}},
		RequiredAgents:       []string{"evidence_reviewer"},
		MaximumAutonomyLevel: 2,
		AuthoritySummary:     "Recommend only",
		RequiresApproval:     true,
		ApprovalReasons:      []string{"consequential output"},
		EvidenceRequirements: []string{"source record"},
		CompletionCriteria:   []string{"claims verified"},
		LearningPlan:         []string{"record confirmed correction"},
		ContextRequirements:  []string{"source context"},
		SelectionReason:      "Evidence discipline applies.",
		ConstitutionVersion:  1,
		ConstitutionSource:   "builtin-robert-constitution-v1:v1",
	}
}

func selectionRowCopy(source models.FrameworkSelectionRecord) models.FrameworkSelectionRecord {
	copied := source
	copied.ID = uuid.New()
	copied.CreatedAt = time.Now().UTC()
	return copied
}

func postgresConstitution(version int, summary string) Constitution {
	baseVersion := version - 1
	if baseVersion < 0 {
		baseVersion = 0
	}
	return Constitution{
		ID:             uuid.NewString(),
		Version:        version,
		BaseVersion:    baseVersion,
		Status:         ConstitutionDraft,
		Values:         []string{"Keep the owner in control."},
		ProtectedRules: []string{"Never self-approve."},
		ChangeSummary:  summary,
		CreatedAt:      time.Now().UTC(),
	}
}

func constitutionRowCopy(source models.RobertConstitutionVersion) models.RobertConstitutionVersion {
	copied := source
	copied.ID = uuid.New()
	copied.Version += 100
	copied.CreatedAt = time.Now().UTC()
	return copied
}

func postgresDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
