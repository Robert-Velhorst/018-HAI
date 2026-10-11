package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

func sourceConfigurationRaceSQLState(err error) string {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return state.SQLState()
	}
	return ""
}

func sourceConfigurationRaceAcceptConflict(t *testing.T, phase string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	switch sourceConfigurationRaceSQLState(err) {
	case "23000", "23503", "23514", "40001", "40P01", "55P03":
		t.Logf("%s refused safely: %v", phase, err)
	default:
		t.Fatalf("%s failed without an expected integrity/concurrency refusal: %v", phase, err)
	}
}

func sourceConfigurationRaceConnection(t *testing.T, ctx context.Context, db *sql.DB) (*sql.Conn, int) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close owned race connection: %v", err)
		}
	})
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil || pid <= 0 {
		t.Fatalf("identify owned backend: pid=%d err=%v", pid, err)
	}
	return conn, pid
}

func sourceConfigurationRaceBegin(t *testing.T, ctx context.Context, conn *sql.Conn, isolation sql.IsolationLevel) *sql.Tx {
	t.Helper()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, setting := range []string{
		"SET LOCAL lock_timeout = '8s'",
		"SET LOCAL statement_timeout = '12s'",
		"SET CONSTRAINTS ALL DEFERRED",
	} {
		if _, err := tx.ExecContext(ctx, setting); err != nil {
			t.Fatal(err)
		}
	}
	return tx
}

// A completed old-style check and a proven wait on B are both usable barriers.
// The former reproduces the vulnerable schedule; neither depends on a sleep.
func sourceConfigurationRaceCheckOrWait(t *testing.T, ctx context.Context, observer *sql.Conn, pidA, pidB int, result <-chan error) (error, bool) {
	t.Helper()
	watch, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err, false
		case <-watch.Done():
			t.Fatalf("enrollment check neither completed nor waited on owned backend %d: %v", pidB, watch.Err())
		case <-ticker.C:
			poll, stop := context.WithTimeout(watch, time.Second)
			var waiting bool
			err := observer.QueryRowContext(poll, `SELECT
EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1 AND wait_event_type = 'Lock')
AND EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND NOT granted)
AND $2 = ANY(pg_blocking_pids($1))`, pidA, pidB).Scan(&waiting)
			stop()
			if err != nil {
				t.Fatalf("inspect only owned enrollment backend %d: %v", pidA, err)
			}
			if waiting {
				return nil, true
			}
		}
	}
}

func sourceConfigurationRaceJoin(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatalf("enrollment checker did not finish after mutation released its lock: %v", ctx.Err())
		return ctx.Err()
	}
}

func sourceConfigurationRaceAssertFinal(t *testing.T, ctx context.Context, observer *sql.Conn, feed accountfeed.Feed, enrollment, mutation string, committedA, committedB bool) {
	t.Helper()
	if !committedA && !committedB {
		t.Fatal("both transactions failed; the schedule proved no useful acceptance")
	}
	var inconsistent int
	if err := observer.QueryRowContext(ctx, `SELECT count(*)
FROM public.operation_source_origins AS origin
LEFT JOIN public.account_feeds AS feed
ON feed.owner_user_id = origin.owner_user_id AND feed.workspace_id = origin.workspace_id AND feed.id = origin.origin_id
WHERE origin.owner_user_id = $1 AND origin.workspace_id = $2 AND origin.origin_id = $3
AND origin.registry_managed AND (feed.id IS NULL
OR origin.registry_config_version IS DISTINCT FROM feed.config_version
OR origin.enabled IS DISTINCT FROM feed.enabled)`, feed.OwnerUserID, feed.WorkspaceID, feed.ID).Scan(&inconsistent); err != nil {
		t.Fatal(err)
	}
	if inconsistent != 0 {
		t.Fatalf("committed enrollment/configuration mismatch: mutation=%s enrollmentCommitted=%t mutationCommitted=%t", mutation, committedA, committedB)
	}
	var version int64
	var name string
	var enabled bool
	err := observer.QueryRowContext(ctx, `SELECT name, config_version, enabled FROM public.account_feeds
WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`, feed.ID, feed.OwnerUserID, feed.WorkspaceID).Scan(&name, &version, &enabled)
	if committedB && mutation == "delete" {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("committed deletion was not durable: name=%q version=%d err=%v", name, version, err)
		}
	} else {
		wantName, wantVersion, wantEnabled := feed.Name, feed.ConfigVersion, feed.Enabled
		if committedB {
			wantVersion++
			if mutation == "rename" {
				wantName = "canonical B"
			} else {
				wantEnabled = false
			}
		}
		if err != nil || name != wantName || version != wantVersion || enabled != wantEnabled {
			t.Fatalf("final feed not the committed transaction state: name=%q version=%d enabled=%t err=%v", name, version, enabled, err)
		}
	}
	var managed, originEnabled bool
	var originVersion, originEpoch int64
	var digest string
	originErr := observer.QueryRowContext(ctx, `SELECT registry_managed, registry_config_version, config_epoch, enabled, config_digest
FROM public.operation_source_origins WHERE owner_user_id = $1 AND workspace_id = $2 AND origin_id = $3`,
		feed.OwnerUserID, feed.WorkspaceID, feed.ID).Scan(&managed, &originVersion, &originEpoch, &originEnabled, &digest)
	if !committedA && enrollment == "insert" {
		if !errors.Is(originErr, sql.ErrNoRows) {
			t.Fatalf("refused first enrollment left an origin: managed=%t version=%d epoch=%d err=%v", managed, originVersion, originEpoch, originErr)
		}
		return
	}
	wantManaged, wantVersion, wantEpoch := false, int64(0), int64(1)
	if committedA {
		wantManaged, wantVersion = true, 1
		if enrollment == "update_unmanaged" {
			wantEpoch = 2
		}
	}
	if originErr != nil || managed != wantManaged || originVersion != wantVersion || originEpoch != wantEpoch ||
		originEnabled != feed.Enabled || digest != feed.SourceObservationStart().ConfigDigest {
		t.Fatalf("origin not the exact committed/rolled-back enrollment state: enrollment=%s committed=%t managed=%t version=%d epoch=%d enabled=%t digest=%q err=%v",
			enrollment, committedA, managed, originVersion, originEpoch, originEnabled, digest, originErr)
	}
}

func sourceConfigurationRaceRepeatableReadTruncate(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	feed := accountfeed.Feed{ID: uuid.New(), Name: "canonical truncate fixture", Provider: "generic_json_feed",
		SourceType: accountfeed.SourceLocalJSONFile, Path: "truncate-race.json", OwnerUserID: "source-truncate-owner",
		WorkspaceID: "local", Enabled: true, ConfigVersion: 1}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.account_feeds
(id, owner_user_id, workspace_id, name, provider, source_type, path, enabled, config_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, feed.ID, feed.OwnerUserID, feed.WorkspaceID,
		feed.Name, feed.Provider, feed.SourceType, feed.Path, feed.Enabled, feed.ConfigVersion); err != nil {
		t.Fatal(err)
	}
	connA, pidA := sourceConfigurationRaceConnection(t, ctx, db)
	connB, pidB := sourceConfigurationRaceConnection(t, ctx, db)
	observer, pidObserver := sourceConfigurationRaceConnection(t, ctx, db)
	if pidA == pidB || pidA == pidObserver || pidB == pidObserver {
		t.Fatal("truncate acceptance needs independent owned backends")
	}
	txB := sourceConfigurationRaceBegin(t, ctx, connB, sql.LevelRepeatableRead)
	var managedCount, auditCount int
	if err := txB.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM public.operation_source_origins WHERE registry_managed),
(SELECT count(*) FROM public.account_feed_audits)`).Scan(&managedCount, &auditCount); err != nil || managedCount != 0 || auditCount != 0 {
		t.Fatalf("truncate snapshot must precede every managed enrollment and have no audits: managed=%d audits=%d err=%v", managedCount, auditCount, err)
	}
	txA := sourceConfigurationRaceBegin(t, ctx, connA, sql.LevelReadCommitted)
	if _, err := txA.ExecContext(ctx, `INSERT INTO public.operation_source_origins
(owner_user_id, workspace_id, origin_id, config_digest, config_epoch, registry_managed, enabled, registry_config_version)
VALUES ($1, $2, $3, $4, 1, true, true, 1)`, feed.OwnerUserID, feed.WorkspaceID, feed.ID, feed.SourceObservationStart().ConfigDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := txA.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatalf("healthy truncate-fixture enrollment: %v", err)
	}
	if err := txA.Commit(); err != nil {
		t.Fatal(err)
	}
	// TRUNCATE bypasses row-level MVCC conflict checking. These independent
	// schema barriers must still refuse it from the pinned old RR snapshot.
	for _, tc := range []struct {
		name, query, state string
	}{
		{"standalone_feed", "TRUNCATE public.account_feeds", "0A000"},
		{"feed_and_audits", "TRUNCATE public.account_feeds, public.account_feed_audits", "23000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := txB.ExecContext(ctx, "SAVEPOINT source_configuration_truncate"); err != nil {
				t.Fatal(err)
			}
			_, truncateErr := txB.ExecContext(ctx, tc.query)
			// Restore even an unexpectedly successful TRUNCATE before asserting.
			if _, err := txB.ExecContext(ctx, "ROLLBACK TO SAVEPOINT source_configuration_truncate"); err != nil {
				t.Fatalf("restore owned truncate fixture: %v", err)
			}
			if _, err := txB.ExecContext(ctx, "RELEASE SAVEPOINT source_configuration_truncate"); err != nil {
				t.Fatal(err)
			}
			if sourceConfigurationRaceSQLState(truncateErr) != tc.state {
				t.Fatalf("old RR snapshot escaped truncate protection: err=%v SQLSTATE=%q want=%s", truncateErr, sourceConfigurationRaceSQLState(truncateErr), tc.state)
			}
			if tc.name == "feed_and_audits" && !strings.Contains(truncateErr.Error(), "account feed audits are immutable") {
				t.Fatalf("combined truncate did not reach the immutable audit guard: %v", truncateErr)
			}
		})
	}
	if err := txB.Rollback(); err != nil {
		t.Fatal(err)
	}
	sourceConfigurationRaceAssertFinal(t, ctx, observer, feed, "insert", "rename", true, false)
	if err := observer.QueryRowContext(ctx, "SELECT count(*) FROM public.account_feed_audits").Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("refused truncate altered the empty audit fixture: count=%d err=%v", auditCount, err)
	}
}

// Guarded acceptance only: all DDL/DML and lock observation are confined to one
// newly owned database. There is no parallel test or provider/source read.
func TestSourceConfigurationPostgresEnrollmentConcurrency(t *testing.T) {
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
	t.Run("repeatable_read_truncate", func(t *testing.T) {
		sourceConfigurationRaceRepeatableReadTruncate(t, ctx, sqlDB)
	})
	for _, isolation := range []struct {
		name  string
		level sql.IsolationLevel
	}{
		{"read_committed", sql.LevelReadCommitted},
		{"repeatable_read", sql.LevelRepeatableRead},
	} {
		for _, enrollment := range []string{"insert", "update_unmanaged"} {
			for _, schedule := range []string{"mutation_first", "enrollment_first"} {
				for _, mutation := range []string{"rename", "disable", "delete"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", isolation.name, enrollment, schedule, mutation), func(t *testing.T) {
						feed := accountfeed.Feed{ID: uuid.New(), Name: "canonical A", Provider: "generic_json_feed",
							SourceType: accountfeed.SourceLocalJSONFile, Path: "race.json", OwnerUserID: "source-race-owner",
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
							t.Fatal("race needs three independent owned PostgreSQL connections")
						}
						txA := sourceConfigurationRaceBegin(t, ctx, connA, isolation.level)
						txB := sourceConfigurationRaceBegin(t, ctx, connB, isolation.level)
						// Pin B's snapshot before enrollment, including absence of a managed origin.
						var snapshotVersion int64
						var managedCount int
						if err := txB.QueryRowContext(ctx, `SELECT feed.config_version,
(SELECT count(*) FROM public.operation_source_origins WHERE owner_user_id = $2 AND workspace_id = $3 AND origin_id = $1 AND registry_managed)
FROM public.account_feeds AS feed WHERE id = $1 AND owner_user_id = $2 AND workspace_id = $3`,
							feed.ID, feed.OwnerUserID, feed.WorkspaceID).Scan(&snapshotVersion, &managedCount); err != nil || snapshotVersion != 1 || managedCount != 0 {
							t.Fatalf("invalid pre-enrollment snapshot: version=%d managed=%d err=%v", snapshotVersion, managedCount, err)
						}
						if enrollment == "insert" {
							_, err = txA.ExecContext(ctx, `INSERT INTO public.operation_source_origins
(owner_user_id, workspace_id, origin_id, config_digest, config_epoch, registry_managed, enabled, registry_config_version)
VALUES ($1, $2, $3, $4, 1, true, true, 1)`, feed.OwnerUserID, feed.WorkspaceID, feed.ID, feed.SourceObservationStart().ConfigDigest)
						} else {
							_, err = txA.ExecContext(ctx, `UPDATE public.operation_source_origins SET registry_managed = true,
registry_config_version = 1, config_epoch = config_epoch + 1
WHERE owner_user_id = $1 AND workspace_id = $2 AND origin_id = $3`, feed.OwnerUserID, feed.WorkspaceID, feed.ID)
						}
						if err != nil {
							t.Fatalf("prepare uncommitted enrollment: %v", err)
						}
						committedA, committedB := false, false
						if schedule == "enrollment_first" {
							if _, err := txA.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
								t.Fatalf("healthy enrollment check: %v", err)
							}
							if err := txA.Commit(); err != nil {
								t.Fatalf("healthy enrollment commit: %v", err)
							}
							committedA = true
						}
						mutationSQL := `UPDATE public.account_feeds SET name = 'canonical B', config_version = config_version + 1 WHERE id = $1`
						if mutation == "disable" {
							mutationSQL = `UPDATE public.account_feeds SET enabled = false, config_version = config_version + 1 WHERE id = $1`
						} else if mutation == "delete" {
							mutationSQL = `DELETE FROM public.account_feeds WHERE id = $1`
						}
						_, mutationErr := txB.ExecContext(ctx, mutationSQL, feed.ID)
						if mutationErr == nil {
							_, mutationErr = txB.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
						}
						if schedule == "enrollment_first" {
							if mutationErr == nil {
								mutationErr = txB.Commit()
								committedB = mutationErr == nil
							}
							want := "23000"
							if isolation.level == sql.LevelRepeatableRead {
								want = "40001"
							}
							if sourceConfigurationRaceSQLState(mutationErr) != want {
								t.Fatalf("pre-enrollment snapshot escaped the MVCC fence: err=%v SQLSTATE=%q want=%s", mutationErr, sourceConfigurationRaceSQLState(mutationErr), want)
							}
							_ = txB.Rollback()
						} else {
							sourceConfigurationRaceAcceptConflict(t, "mutation/check", mutationErr)
							if mutationErr != nil {
								_ = txB.Rollback()
							}
							checkCtx, stopCheck := context.WithTimeout(ctx, 12*time.Second)
							defer stopCheck()
							result, done := make(chan error, 1), make(chan struct{})
							defer func() {
								stopCheck()
								_ = txB.Rollback()
								select {
								case <-done:
								case <-time.After(15 * time.Second):
									t.Error("enrollment constraint worker did not join after cancellation and lock release")
								}
							}()
							go func() {
								defer close(done)
								_, err := txA.ExecContext(checkCtx, "SET CONSTRAINTS ALL IMMEDIATE")
								result <- err
							}()
							checkErr, waiting := sourceConfigurationRaceCheckOrWait(t, checkCtx, observer, pidA, pidB, result)
							if mutationErr == nil {
								mutationErr = txB.Commit()
								committedB = mutationErr == nil
								sourceConfigurationRaceAcceptConflict(t, "mutation commit", mutationErr)
								if mutationErr != nil {
									_ = txB.Rollback()
								}
							}
							if waiting {
								checkErr = sourceConfigurationRaceJoin(t, checkCtx, result)
							}
							sourceConfigurationRaceAcceptConflict(t, "enrollment check", checkErr)
							if checkErr == nil {
								checkErr = txA.Commit()
								committedA = checkErr == nil
								sourceConfigurationRaceAcceptConflict(t, "enrollment commit", checkErr)
							} else {
								_ = txA.Rollback()
							}
						}
						sourceConfigurationRaceAssertFinal(t, ctx, observer, feed, enrollment, mutation, committedA, committedB)
					})
				}
			}
		}
	}
}
