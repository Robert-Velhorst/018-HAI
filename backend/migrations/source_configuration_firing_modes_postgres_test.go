package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

// Enrollment-first acceptance only. The two firing modes exercise default
// deferred COMMIT and already-immediate origin DML without provider/source reads.
func TestSourceConfigurationPostgresEnrollmentFiringModes(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := openIsolatedMigrationDatabase(t).WithContext(ctx)
	for _, phase := range []string{"pre", "post"} {
		if _, err := infra.ApplyMigrations(db, migrations.Files, phase); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	for _, isolation := range []struct {
		name  string
		level sql.IsolationLevel
	}{
		{"read_committed", sql.LevelReadCommitted},
		{"repeatable_read", sql.LevelRepeatableRead},
	} {
		for _, enrollment := range []string{"insert", "update_unmanaged"} {
			for _, firing := range []string{"commit", "immediate_dml"} {
				for _, mutation := range []string{"rename", "disable", "delete"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", isolation.name, enrollment, firing, mutation), func(t *testing.T) {
						feed := accountfeed.Feed{ID: uuid.New(), Name: "canonical firing fixture", Provider: "generic_json_feed",
							SourceType: accountfeed.SourceLocalJSONFile, Path: "firing-mode.json", OwnerUserID: "source-firing-owner",
							WorkspaceID: "local", Enabled: true, ConfigVersion: 1}
						if _, err := sqlDB.ExecContext(ctx, `INSERT INTO public.account_feeds
(id, owner_user_id, workspace_id, name, provider, source_type, path, enabled, config_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, feed.ID, feed.OwnerUserID, feed.WorkspaceID,
							feed.Name, feed.Provider, feed.SourceType, feed.Path, feed.Enabled, feed.ConfigVersion); err != nil {
							t.Fatal(err)
						}
						if enrollment == "update_unmanaged" {
							if _, err := sqlDB.ExecContext(ctx, `INSERT INTO public.operation_source_origins
(owner_user_id, workspace_id, origin_id, config_digest, config_epoch)
VALUES ($1, $2, $3, $4, 1)`, feed.OwnerUserID, feed.WorkspaceID, feed.ID, feed.SourceObservationStart().ConfigDigest); err != nil {
								t.Fatal(err)
							}
						}
						connA, pidA := sourceConfigurationRaceConnection(t, ctx, sqlDB)
						connB, pidB := sourceConfigurationRaceConnection(t, ctx, sqlDB)
						observer, pidObserver := sourceConfigurationRaceConnection(t, ctx, sqlDB)
						if pidA == pidB || pidA == pidObserver || pidB == pidObserver {
							t.Fatal("firing-mode acceptance needs independent owned PostgreSQL connections")
						}
						txA := sourceConfigurationRaceBegin(t, ctx, connA, isolation.level)
						txB := sourceConfigurationRaceBegin(t, ctx, connB, isolation.level)
						var snapshotVersion int64
						var snapshotEnabled bool
						var managedCount int
						if err := txB.QueryRowContext(ctx, `SELECT feed.config_version, feed.enabled,
(SELECT count(*) FROM public.operation_source_origins WHERE owner_user_id = $2 AND workspace_id = $3 AND origin_id = $1 AND registry_managed)
FROM public.account_feeds AS feed WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`,
							feed.ID, feed.OwnerUserID, feed.WorkspaceID).Scan(&snapshotVersion, &snapshotEnabled, &managedCount); err != nil ||
							snapshotVersion != 1 || !snapshotEnabled || managedCount != 0 {
							t.Fatalf("B did not pin a healthy pre-enrollment snapshot: version=%d enabled=%t managed=%d err=%v", snapshotVersion, snapshotEnabled, managedCount, err)
						}
						if firing == "immediate_dml" {
							if _, err := txA.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
								t.Fatalf("set immediate mode before enrollment DML: %v", err)
							}
						}
						var result sql.Result
						var enrollmentErr error
						if enrollment == "insert" {
							result, enrollmentErr = txA.ExecContext(ctx, `INSERT INTO public.operation_source_origins
(owner_user_id, workspace_id, origin_id, config_digest, config_epoch, registry_managed, enabled, registry_config_version)
VALUES ($1, $2, $3, $4, 1, true, true, 1)`, feed.OwnerUserID, feed.WorkspaceID, feed.ID, feed.SourceObservationStart().ConfigDigest)
						} else {
							result, enrollmentErr = txA.ExecContext(ctx, `UPDATE public.operation_source_origins SET registry_managed = true,
registry_config_version = 1, config_epoch = config_epoch + 1
WHERE owner_user_id = $1 AND workspace_id = $2 AND origin_id = $3`, feed.OwnerUserID, feed.WorkspaceID, feed.ID)
						}
						if enrollmentErr != nil {
							t.Fatalf("healthy %s enrollment DML failed in %s mode: %v", enrollment, firing, enrollmentErr)
						}
						if count, err := result.RowsAffected(); err != nil || count != 1 {
							t.Fatalf("enrollment did not write exactly its fixture origin: count=%d err=%v", count, err)
						}
						// In commit mode, never force A's queued constraints beforehand.
						if err := txA.Commit(); err != nil {
							t.Fatalf("healthy %s enrollment commit failed in %s mode: %v", enrollment, firing, err)
						}
						mutationSQL := `UPDATE public.account_feeds SET name = 'canonical B', config_version = config_version + 1 WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`
						if mutation == "disable" {
							mutationSQL = `UPDATE public.account_feeds SET enabled = false, config_version = config_version + 1 WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`
						} else if mutation == "delete" {
							mutationSQL = `DELETE FROM public.account_feeds WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`
						}
						_, mutationErr := txB.ExecContext(ctx, mutationSQL, feed.ID, feed.OwnerUserID, feed.WorkspaceID)
						if mutationErr == nil {
							_, mutationErr = txB.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
						}
						// Also roll back an unexpectedly accepted mutation before asserting.
						if err := txB.Rollback(); err != nil {
							t.Fatalf("roll back owned stale-snapshot writer: %v", err)
						}
						want := "23000"
						if isolation.level == sql.LevelRepeatableRead {
							want = "40001"
						}
						if sourceConfigurationRaceSQLState(mutationErr) != want {
							t.Fatalf("stale B snapshot escaped %s enrollment fence: err=%v SQLSTATE=%q want=%s", firing, mutationErr, sourceConfigurationRaceSQLState(mutationErr), want)
						}
						sourceConfigurationRaceAssertFinal(t, ctx, observer, feed, enrollment, mutation, true, false)
					})
				}
			}
		}
	}
}
