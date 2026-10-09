package migrations_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
)

// This defines actual SQL acceptance, but skips before any database creation
// unless the operator opts in with the exact isolated loopback test DSN.
func TestSourceConfigurationPostgresCanonicalRevocationAndRawMutationRefusal(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := openIsolatedMigrationDatabase(t).WithContext(ctx)
	files := migrationFilesThrough(t, "pre/0114_operation_source_configuration")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatal(err)
	}
	if err := infra.RollbackMigration(db, files, "pre", "pre/0114_operation_source_configuration"); err != nil {
		t.Fatal(err)
	}
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatal(err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "post"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(`{"items":[{"externalId":"one","title":"Review real SQL source","content":"source","itemType":"email","provider":"gmail"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := operations.NewGormRepository(db)
	service := operations.NewService(repo)
	registry, err := accountfeed.NewRegistryWithRepository(accountfeed.NewGormRegistryRepository(db), service, nil, accountfeed.FetchOptions{FeedsRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	feed, err := registry.RegisterContext(ctx, accountfeed.Feed{Name: "original", Provider: "generic_json_feed", SourceType: accountfeed.SourceLocalJSONFile,
		Path: "feed.json", OwnerUserID: "config-pg-owner", WorkspaceID: "local", Enabled: true})
	if err != nil || feed.ConfigVersion != 1 {
		t.Fatalf("canonical register: %+v / %v", feed, err)
	}
	scope := accountfeed.FeedScope{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}
	report, err := registry.SyncContext(ctx, scope, feed.ID)
	if err != nil || !report.Recorded || !report.ReadCompleted || report.OperationsCreated != 1 || len(report.Errors) != 0 {
		t.Fatalf("source sync: %+v / %v", report, err)
	}
	// A raw config update cannot leave a managed origin at the previous version.
	assertSourceObservationSQLState(t, db.Exec("UPDATE public.account_feeds SET name = ?, config_version = config_version + 1 WHERE id = ?", "bypassed", feed.ID).Error, "23000")
	current, err := registry.GetContext(ctx, scope, feed.ID)
	if err != nil || current.Name != feed.Name || current.ConfigVersion != 1 {
		t.Fatalf("refused SQL mutation persisted: %+v / %v", current, err)
	}
	changedName := "changed"
	changed, err := registry.PatchContext(ctx, scope, feed.ID, accountfeed.FeedPatch{Name: &changedName})
	if err != nil || changed.ConfigVersion != 2 {
		t.Fatalf("canonical patch: %+v / %v", changed, err)
	}
	if _, err := repo.BeginSourceObservation(ctx, feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) {
		t.Fatalf("stale SQL snapshot admitted: %v", err)
	}
	claim, err := service.ClaimNext(ctx, feed.OwnerUserID, feed.WorkspaceID, uuid.New(), time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("old head eligible before resync: %+v / %v", claim, err)
	}
	restored, err := registry.PatchContext(ctx, scope, feed.ID, accountfeed.FeedPatch{Name: &feed.Name})
	if err != nil || restored.ConfigVersion != 3 {
		t.Fatalf("A-B-A version: %+v / %v", restored, err)
	}
	if _, err := repo.BeginSourceObservation(ctx, feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) {
		t.Fatalf("stale A resurrected: %v", err)
	}
	disabled := false
	if _, err := registry.PatchContext(ctx, scope, feed.ID, accountfeed.FeedPatch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SyncContext(ctx, scope, feed.ID); !errors.Is(err, accountfeed.ErrFeedDisabled) {
		t.Fatalf("disabled sync admitted: %v", err)
	}
	assertSourceObservationSQLState(t, db.Exec("UPDATE public.operation_source_origins SET enabled = true, registry_config_version = registry_config_version + 1, config_epoch = config_epoch + 1 WHERE origin_id = ?", feed.ID).Error, "23000")
	assertSourceObservationSQLState(t, db.Exec("UPDATE public.operation_source_origins SET registry_managed = false, config_epoch = config_epoch + 1 WHERE origin_id = ?", feed.ID).Error, "23000")
	assertSourceObservationSQLState(t, db.Exec("DELETE FROM public.account_feeds WHERE id = ?", feed.ID).Error, "23000")
	assertSourceObservationSQLState(t, infra.RollbackMigration(db, files, "pre", "pre/0114_operation_source_configuration"), "55000")
}
