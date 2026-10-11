//go:build integration

package accountfeed

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test creates its own database from an explicitly approved, exact-name
// loopback connection. The configured database's schema/data are never changed.
// It is never part of the normal unit suite.
func TestPostgresRegistryCommittedReconstructionAndAtomicFailure(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_ACCOUNT_FEED_TEST_DSN", "hai_account_feed_registry_test")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("cannot parse dedicated registry test database DSN")
	}
	config.ConnectTimeout = 5 * time.Second
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	if config.RuntimeParams == nil {
		config.RuntimeParams = make(map[string]string)
	}
	config.RuntimeParams["search_path"] = "public,pg_catalog"
	config.RuntimeParams["statement_timeout"] = "120000"
	config.RuntimeParams["lock_timeout"] = "5000"
	config.RuntimeParams["idle_in_transaction_session_timeout"] = "120000"
	adminPool := stdlib.OpenDB(*config)
	adminPool.SetMaxOpenConns(1)
	adminPool.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := adminPool.Close(); err != nil {
			t.Error("cannot close registry test administration pool")
		}
	})
	var baseDatabase string
	if err := adminPool.QueryRowContext(ctx, "SELECT pg_catalog.current_database()").Scan(&baseDatabase); err != nil {
		t.Fatal("cannot verify dedicated registry test administration database")
	}
	if baseDatabase != "hai_account_feed_registry_test" {
		t.Fatal("registry test administration database differs from the guarded target")
	}
	databaseName := "hai_account_feed_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedDatabase := pgx.Identifier{databaseName}.Sanitize()
	// CREATE, never IF NOT EXISTS: a collision never grants cleanup ownership.
	if _, err := adminPool.ExecContext(ctx, "CREATE DATABASE "+quotedDatabase+" TEMPLATE template0"); err != nil {
		t.Fatal("cannot create a new owned registry test database")
	}
	var ownedDatabaseOID int64
	t.Cleanup(func() {
		// Pools registered below close first. Never terminate unrelated sessions.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		var currentOID int64
		if ownedDatabaseOID == 0 {
			t.Error("cannot prove registry test database identity; refusing cleanup")
			return
		}
		if err := adminPool.QueryRowContext(cleanupCtx, "SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = $1", databaseName).Scan(&currentOID); err != nil {
			t.Error("cannot verify owned registry test database before cleanup")
			return
		}
		if currentOID != ownedDatabaseOID {
			t.Error("registry test database identity changed; refusing cleanup")
			return
		}
		// No FORCE or CASCADE: unexpected connections prevent cleanup.
		if _, err := adminPool.ExecContext(cleanupCtx, "DROP DATABASE "+quotedDatabase); err != nil {
			t.Errorf("cannot drop owned registry test database %s: %v", databaseName, err)
		}
	})
	if err := adminPool.QueryRowContext(ctx, "SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = $1", databaseName).Scan(&ownedDatabaseOID); err != nil {
		t.Fatal("cannot record owned registry test database identity")
	}
	// Every independent pool targets the same newly owned public database.
	openDB := func(maxConnections int) *gorm.DB {
		t.Helper()
		testConfig := config.Copy()
		testConfig.Database = databaseName
		pool := stdlib.OpenDB(*testConfig)
		pool.SetMaxOpenConns(maxConnections)
		pool.SetMaxIdleConns(maxConnections)
		t.Cleanup(func() {
			if err := pool.Close(); err != nil {
				t.Error("cannot close owned registry test pool")
			}
		})
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
			DisableAutomaticPing: true,
			Logger:               logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			t.Fatal("cannot open dedicated registry test database")
		}
		if err := pool.PingContext(ctx); err != nil {
			t.Fatal("cannot reach dedicated registry test database within deadline")
		}
		var databaseName, searchPath string
		if err := pool.QueryRowContext(ctx, "SELECT pg_catalog.current_database(), pg_catalog.current_setting('search_path')").Scan(&databaseName, &searchPath); err != nil {
			t.Fatal("cannot verify dedicated registry test database identity and namespace")
		}
		if databaseName != testConfig.Database || searchPath != testConfig.RuntimeParams["search_path"] {
			t.Fatal("connected registry test database identity or namespace differs from the guarded target")
		}
		return db.WithContext(ctx)
	}
	db := openDB(2)
	for _, phase := range []string{"pre", "post"} {
		if _, err := infra.ApplyMigrations(db, migrations.Files, phase); err != nil {
			t.Fatalf("apply full embedded %s migrations in owned database: %v", phase, err)
		}
	}
	repo := NewGormRegistryRepository(db)
	feed := persistenceSeed()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	registered := newFeedAudit(feed.ID, "registered", "test feed registered", at)
	if got, err := repo.Register(ctx, feed, registered); err != nil || got.ConfigVersion != 1 {
		t.Fatalf("registered feed version incorrect: %+v %v", got, err)
	}
	scope := feedScope(feed)
	disabled := false
	name := "operator choice"
	updated := newFeedAudit(feed.ID, "updated", "operator choice", at.Add(time.Second))
	if got, err := repo.Patch(ctx, scope, feed.ID, FeedPatch{Enabled: &disabled, Name: &name}, updated); err != nil || got.Enabled || got.Name != name || got.ConfigVersion != 2 {
		t.Fatalf("disabled operator patch incorrect: %+v %v", got, err)
	}
	if got, err := repo.Register(ctx, feed, newFeedAudit(feed.ID, "registered", "duplicate seed", at)); err != nil || got.Enabled || got.Name != name || got.ConfigVersion != 2 {
		t.Fatalf("seed overwrote operator choice: %+v %v", got, err)
	}
	if _, err := repo.BeginEnabledSync(ctx, scope, feed.ID, uuid.New(), at.Add(2*time.Second)); !errors.Is(err, ErrFeedDisabled) {
		t.Fatalf("disabled feed admitted to a bulk claim: %v", err)
	}
	if _, err := repo.BeginSync(ctx, scope, feed.ID, uuid.New(), at.Add(2*time.Second)); !errors.Is(err, ErrFeedDisabled) {
		t.Fatalf("disabled feed admitted to a manual claim: %v", err)
	}
	disabledRow, err := repo.Get(ctx, scope, feed.ID)
	if err != nil || disabledRow.Feed.Enabled || disabledRow.Feed.ConfigVersion != 2 || disabledRow.SyncToken != nil ||
		disabledRow.SyncStartedAt != nil || disabledRow.LastAttemptAt != nil {
		t.Fatalf("refused disabled claims changed feed state: %+v %v", disabledRow, err)
	}
	enabled := true
	reenabled := newFeedAudit(feed.ID, "updated", "operator enabled feed", at.Add(2*time.Second))
	if got, err := repo.Patch(ctx, scope, feed.ID, FeedPatch{Enabled: &enabled}, reenabled); err != nil || !got.Enabled || got.Name != name || got.ConfigVersion != 3 {
		t.Fatalf("explicit enable patch incorrect: %+v %v", got, err)
	}
	claimAt := at.Add(3 * time.Second)
	// Two independent repository instances must not admit two observations.
	tokens := []uuid.UUID{uuid.New(), uuid.New()}
	results := make(chan struct {
		index int
		err   error
	}, 2)
	for i := range tokens {
		go func(i int) {
			_, err := NewGormRegistryRepository(db).BeginSync(ctx, scope, feed.ID, tokens[i], claimAt)
			results <- struct {
				index int
				err   error
			}{i, err}
		}(i)
	}
	winner := -1
	var claimErrors []error
	claims := 0
	for range tokens {
		select {
		case result := <-results:
			if result.err == nil {
				claims++
				winner = result.index
			} else if !errors.Is(result.err, ErrFeedSyncBusy) {
				claimErrors = append(claimErrors, result.err)
			}
		case <-ctx.Done():
			t.Fatal("sync claims exceeded test deadline")
		}
	}
	if claims != 1 || len(claimErrors) != 0 {
		t.Fatalf("expected one sync claim, got %d; unexpected errors: %v", claims, claimErrors)
	}
	assertAudits := func(got, want []AuditEvent) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("audit count changed: got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].FeedID != want[i].FeedID || got[i].EventType != want[i].EventType ||
				got[i].Message != want[i].Message || !got[i].CreatedAt.Equal(want[i].CreatedAt) {
				t.Fatalf("audit %d changed: got %+v, want %+v", i, got[i], want[i])
			}
		}
	}
	before, err := repo.Get(ctx, scope, feed.ID)
	if err != nil || !before.Feed.Enabled || before.Feed.Name != name || before.Feed.ConfigVersion != 3 || before.LastSuccessAt != nil || before.LastItemsRead != 0 ||
		before.SyncToken == nil || *before.SyncToken != tokens[winner] || before.SyncStartedAt == nil ||
		!before.SyncStartedAt.Equal(claimAt) || before.LastAttemptAt == nil || !before.LastAttemptAt.Equal(claimAt) {
		t.Fatalf("claim state incorrect: %+v %v", before, err)
	}
	beforeAudits, err := repo.Audit(ctx, scope, feed.ID)
	if err != nil || len(beforeAudits) != 4 {
		t.Fatalf("claim audits incorrect: %+v %v", beforeAudits, err)
	}
	started := beforeAudits[0]
	if _, err := uuid.Parse(started.ID); err != nil || started.FeedID != feed.ID.String() || started.EventType != "sync_started" ||
		started.Message != "feed sync started" || !started.CreatedAt.Equal(claimAt) {
		t.Fatalf("sync start audit incorrect: %+v", started)
	}
	assertAudits(beforeAudits, []AuditEvent{started, reenabled, updated, registered})
	if err := repo.FinishSync(ctx, scope, feed.ID, uuid.New(), SyncOutcome{Event: newFeedAudit(feed.ID, "synced", "wrong writer", at)}); !errors.Is(err, ErrFeedSyncBusy) {
		t.Fatalf("stale writer admitted: %v", err)
	}
	// Force the audit insert to fail after the health update. Both must roll back.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE FUNCTION public.hai_test_reject_feed_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type = 'synced' THEN RAISE EXCEPTION 'synthetic finish failure'; END IF; RETURN NEW; END; $$;`).Error; err != nil {
			return err
		}
		return tx.Exec("CREATE TRIGGER hai_test_feed_finish BEFORE INSERT ON public.account_feed_audits FOR EACH ROW EXECUTE FUNCTION public.hai_test_reject_feed_finish()").Error
	}); err != nil {
		t.Fatal(err)
	}
	outcome := SyncOutcome{ItemsRead: 2, Event: newFeedAudit(feed.ID, "synced", "completed import", at.Add(4*time.Second))}
	if err := repo.FinishSync(ctx, scope, feed.ID, tokens[winner], outcome); !errors.Is(err, ErrFeedStorageUnavailable) || !strings.Contains(err.Error(), "synthetic finish failure") {
		t.Fatalf("failure not surfaced: %v", err)
	}
	row, err := repo.Get(ctx, scope, feed.ID)
	if err != nil || !reflect.DeepEqual(row, before) {
		t.Fatalf("failed finish committed partial state: %+v %v", row, err)
	}
	audits, err := repo.Audit(ctx, scope, feed.ID)
	if err != nil {
		t.Fatalf("failed finish added audit: %+v %v", audits, err)
	}
	assertAudits(audits, beforeAudits)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP TRIGGER hai_test_feed_finish ON public.account_feed_audits").Error; err != nil {
			return err
		}
		return tx.Exec("DROP FUNCTION public.hai_test_reject_feed_finish()").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishSync(ctx, scope, feed.ID, tokens[winner], outcome); err != nil {
		t.Fatal(err)
	}
	// A fresh pool must read committed data, not a test transaction or memory cache.
	reopened := openDB(1)
	reconstructed := NewGormRegistryRepository(reopened)
	row, err = reconstructed.Get(ctx, scope, feed.ID)
	if err != nil || !row.Feed.Enabled || row.Feed.Name != name || row.Feed.ConfigVersion != 3 || row.SyncToken != nil || row.SyncStartedAt != nil ||
		row.LastAttemptAt == nil || !row.LastAttemptAt.Equal(claimAt) ||
		row.LastSuccessAt == nil || !row.LastSuccessAt.Equal(outcome.Event.CreatedAt) || row.LastItemsRead != 2 {
		t.Fatalf("committed reconstruction lost state: %+v %v", row, err)
	}
	audits, err = reconstructed.Audit(ctx, scope, feed.ID)
	if err != nil {
		t.Fatalf("committed audits lost: %+v %v", audits, err)
	}
	wantAudits := []AuditEvent{outcome.Event, started, reenabled, updated, registered}
	assertAudits(audits, wantAudits)
	for _, foreign := range []FeedScope{{OwnerUserID: "other", WorkspaceID: scope.WorkspaceID}, {OwnerUserID: scope.OwnerUserID, WorkspaceID: "other"}} {
		if _, err := reconstructed.Get(ctx, foreign, feed.ID); !errors.Is(err, ErrFeedNotFound) {
			t.Fatalf("foreign scope exposed: %v", err)
		}
	}
	if err := db.Model(&accountFeedAuditRow{}).Where("feed_id = ?", feed.ID).Update("message", "changed").Error; err == nil || !strings.Contains(err.Error(), "account feed audits are immutable") {
		t.Fatalf("expected immutable audit trigger rejection: %v", err)
	}
	audits, err = reconstructed.Audit(ctx, scope, feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertAudits(audits, wantAudits)
}
