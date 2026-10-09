//go:build integration

// These tests run only under `-tags integration` against a real Postgres
// pointed to by HAI_TEST_DATABASE_DSN. They prove the versioned migration
// runner (and the full schema) apply against the actual configured engine, not
// a mock. Normal `go test ./...` never compiles or runs them.
package infra

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for isolated Postgres migration tests")
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse Postgres test DSN: %v", err)
	}
	if !isLoopbackIntegrationConfig(adminConfig) {
		t.Fatal("refusing integration tests: every Postgres host, including fallbacks, must be loopback")
	}
	adminConfig.Database = "postgres"
	adminSQL := stdlib.OpenDB(*adminConfig)
	if err := adminSQL.PingContext(context.Background()); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("open Postgres administration connection: %v", err)
	}
	databaseName := "hai_infra_migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedDatabase := `"` + databaseName + `"`
	if _, err := adminSQL.ExecContext(context.Background(), "CREATE DATABASE "+quotedDatabase); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("create isolated Postgres integration database: %v", err)
	}
	testConfig := adminConfig.Copy()
	testConfig.Database = databaseName
	testSQL := stdlib.OpenDB(*testConfig)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: testSQL}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("open isolated Postgres integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := testSQL.Close(); err != nil {
			t.Errorf("close isolated Postgres integration database: %v", err)
		}
		if _, err := adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated Postgres integration database: %v", err)
		}
		if err := adminSQL.Close(); err != nil {
			t.Errorf("close Postgres administration connection: %v", err)
		}
	})
	// Start from a clean schema so the run is reproducible. Extensions must be
	// removed before their schema; the disposable database allows repeated
	// runs without touching any application or shared-test database.
	if err := db.Exec(`
		DROP EXTENSION IF EXISTS "uuid-ossp" CASCADE;
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
	`).Error; err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return db
}

func isLoopbackIntegrationConfig(config *pgx.ConnConfig) bool {
	if config == nil || !isLoopbackIntegrationHost(config.Host) {
		return false
	}
	for _, fallback := range config.Fallbacks {
		if fallback == nil || !isLoopbackIntegrationHost(fallback.Host) {
			return false
		}
	}
	return true
}

func TestIsLoopbackIntegrationConfig(t *testing.T) {
	for _, test := range []struct {
		name  string
		hosts string
		want  bool
	}{
		{name: "loopback", hosts: "127.0.0.1", want: true},
		{name: "localhost", hosts: "localhost", want: true},
		{name: "all loopback fallbacks", hosts: "127.0.0.1,::1,localhost", want: true},
		{name: "remote primary", hosts: "192.0.2.10,127.0.0.1"},
		{name: "remote fallback", hosts: "127.0.0.1,192.0.2.10"},
		{name: "remote last fallback", hosts: "127.0.0.1,::1,db.example.com"},
		{name: "socket fallback", hosts: "127.0.0.1,/tmp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := pgx.ParseConfig("host=" + test.hosts + " user=postgres dbname=postgres sslmode=prefer")
			if err != nil {
				t.Fatalf("parse test config: %v", err)
			}
			if got := isLoopbackIntegrationConfig(config); got != test.want {
				t.Fatalf("isLoopbackIntegrationConfig = %t, want %t", got, test.want)
			}
		})
	}
	if isLoopbackIntegrationConfig(nil) {
		t.Fatal("nil config must be rejected")
	}
}

func isLoopbackIntegrationHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func TestIsLoopbackIntegrationHost(t *testing.T) {
	for _, test := range []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "LOCALHOST", want: true},
		{host: "127.0.0.1", want: true},
		{host: "[::1]", want: true},
		{host: "host.docker.internal", want: false},
		{host: "localhost.example", want: false},
		{host: "192.0.2.10", want: false},
	} {
		t.Run(test.host, func(t *testing.T) {
			if got := isLoopbackIntegrationHost(test.host); got != test.want {
				t.Fatalf("isLoopbackIntegrationHost(%q) = %t, want %t", test.host, got, test.want)
			}
		})
	}
}

func TestConcurrentMigrationRunnersSerializeAndRecheck(t *testing.T) {
	db := integrationDB(t)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, err := ApplyMigrations(db, migrations.Files, "pre")
			errors <- err
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent migration runner failed: %v", err)
		}
	}
	status, err := Status(db, migrations.Files, "pre")
	if err != nil {
		t.Fatalf("pre migration status: %v", err)
	}
	if len(status.Pending) != 0 {
		t.Fatalf("pending migrations after concurrent apply: %#v", status.Pending)
	}
}

func TestMigrationStatusAndRollbackDoNotCreateLedgerOnFreshDatabase(t *testing.T) {
	db := integrationDB(t)
	for _, phase := range []string{"pre", "post"} {
		status, err := Status(db, migrations.Files, phase)
		if err != nil {
			t.Fatalf("status %s: %v", phase, err)
		}
		all, err := loadMigrations(migrations.Files, phase)
		if err != nil {
			t.Fatalf("load %s migrations: %v", phase, err)
		}
		if len(status.Applied) != 0 || len(status.Pending) != len(all) {
			t.Fatalf("fresh %s status = applied %d pending %d; want 0 and %d", phase, len(status.Applied), len(status.Pending), len(all))
		}
	}
	if err := RollbackMigration(db, migrations.Files, "pre", "pre/0001_extensions"); err == nil {
		t.Fatal("rollback succeeded without an applied ledger entry")
	}
	exists, err := schemaMigrationsTableExists(db)
	if err != nil {
		t.Fatalf("check migration ledger: %v", err)
	}
	if exists {
		t.Fatal("read-only status or refused rollback created schema_migrations")
	}
}

func TestMigrationOperationsRejectAppliedVersionsMissingFromSources(t *testing.T) {
	db := integrationDB(t)
	known := fstest.MapFS{
		"pre/0001_known.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE known_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_known.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE known_probe;")},
	}
	if count, err := ApplyMigrations(db, known, "pre"); err != nil || count != 1 {
		t.Fatalf("apply known migration: count=%d err=%v", count, err)
	}
	if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", "pre/0002_removed").Error; err != nil {
		t.Fatalf("insert simulated removed migration version: %v", err)
	}

	withPending := fstest.MapFS{
		"pre/0001_known.up.sql":     &fstest.MapFile{Data: []byte("CREATE TABLE known_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_known.down.sql":   &fstest.MapFile{Data: []byte("DROP TABLE known_probe;")},
		"pre/0003_pending.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE pending_probe (id INTEGER PRIMARY KEY);")},
		"pre/0003_pending.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE pending_probe;")},
	}

	if _, err := Status(db, withPending, "pre"); err == nil || !strings.Contains(err.Error(), "pre/0002_removed") {
		t.Fatalf("Status error = %v, want missing applied migration to be reported", err)
	}
	if _, err := ApplyMigrations(db, withPending, "pre"); err == nil || !strings.Contains(err.Error(), "pre/0002_removed") {
		t.Fatalf("ApplyMigrations error = %v, want missing applied migration to block apply", err)
	}
	if db.Migrator().HasTable("pending_probe") {
		t.Fatal("pending migration ran despite an unknown applied predecessor")
	}
	if err := RollbackMigration(db, withPending, "pre", "pre/0001_known"); err == nil || !strings.Contains(err.Error(), "pre/0002_removed") {
		t.Fatalf("RollbackMigration error = %v, want missing applied migration to block rollback", err)
	}
	if !db.Migrator().HasTable("known_probe") {
		t.Fatal("known migration was rolled back despite an unknown applied version")
	}
}

func TestLegacyMigrationChecksumAdoptionIsExplicitAndDriftFailsClosed(t *testing.T) {
	db := integrationDB(t)
	fsys := fstest.MapFS{
		"pre/0001_legacy.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE checksum_adoption_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_legacy.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE checksum_adoption_probe;")},
	}
	migration, err := loadMigrations(fsys, "pre")
	if err != nil || len(migration) != 1 {
		t.Fatalf("load fixture migration: migrations=%d err=%v", len(migration), err)
	}
	// Reproduce the pre-checksum table and a legitimately applied legacy row.
	if err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatalf("create legacy ledger: %v", err)
	}
	if err := db.Exec(migration[0].UpSQL).Error; err != nil {
		t.Fatalf("apply legacy fixture SQL: %v", err)
	}
	if err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, migration[0].Version).Error; err != nil {
		t.Fatalf("record legacy migration: %v", err)
	}
	if _, err := ApplyMigrations(db, fsys, "pre"); err == nil || !strings.Contains(err.Error(), "no content checksum") {
		t.Fatalf("legacy apply error = %v, want explicit-adoption requirement", err)
	}
	if err := AdoptLegacyMigrationChecksums(db, fsys, "pre", map[string]string{migration[0].Version: "not-the-source-digest"}); err == nil {
		t.Fatal("adoption accepted an unapproved checksum")
	}
	var stillNull bool
	if err := db.Raw(`SELECT checksum IS NULL FROM schema_migrations WHERE version = ?`, migration[0].Version).Row().Scan(&stillNull); err != nil || !stillNull {
		t.Fatalf("failed adoption changed legacy row: null=%t err=%v", stillNull, err)
	}
	if err := AdoptLegacyMigrationChecksums(db, fsys, "pre", map[string]string{migration[0].Version: MigrationChecksum(migration[0])}); err != nil {
		t.Fatalf("explicit checksum adoption: %v", err)
	}
	status, err := Status(db, fsys, "pre")
	if err != nil || len(status.Applied) != 1 || len(status.Pending) != 0 {
		t.Fatalf("status after adoption = %#v err=%v", status, err)
	}

	drifted := fstest.MapFS{
		"pre/0001_legacy.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE checksum_adoption_probe (id INTEGER PRIMARY KEY); -- drift")},
		"pre/0001_legacy.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE checksum_adoption_probe;")},
	}
	if _, err := Status(db, drifted, "pre"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("status with changed source = %v, want checksum mismatch", err)
	}
	if _, err := ApplyMigrations(db, drifted, "pre"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("apply with changed source = %v, want checksum mismatch", err)
	}
	if err := RollbackMigration(db, drifted, "pre", migration[0].Version); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("rollback with changed source = %v, want checksum mismatch", err)
	}
}

func TestPreMigrationApplyPreflightsAppliedPostMigrationChecksums(t *testing.T) {
	db := integrationDB(t)
	fsys := fstest.MapFS{
		"pre/0001_preflight.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE migration_preflight_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_preflight.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE migration_preflight_probe;")},
		"post/0001_postflight.up.sql": &fstest.MapFile{Data: []byte("CREATE INDEX migration_preflight_probe_idx ON migration_preflight_probe (id);")},
		"post/0001_postflight.down.sql": &fstest.MapFile{Data: []byte(
			"DROP INDEX migration_preflight_probe_idx;",
		)},
		"pre/0002_pending.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE migration_preflight_pending (id INTEGER PRIMARY KEY);")},
		"pre/0002_pending.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE migration_preflight_pending;")},
	}
	if _, err := ApplyMigrations(db, fsys, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	if _, err := ApplyMigrations(db, fsys, "post"); err != nil {
		t.Fatalf("apply post migration: %v", err)
	}
	drifted := fstest.MapFS{}
	for name, file := range fsys {
		drifted[name] = file
	}
	drifted["post/0001_postflight.down.sql"] = &fstest.MapFile{Data: []byte("DROP INDEX IF EXISTS migration_preflight_probe_idx;")}
	if _, err := ApplyMigrations(db, drifted, "pre"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("pre apply with drifted applied post source = %v, want preflight checksum mismatch", err)
	}
	if db.Migrator().HasTable("migration_preflight_pending") {
		t.Fatal("pre migration ran before changed post-phase source was detected")
	}
}

func TestRollbackPrePhaseRefusesWhilePostPhaseIsApplied(t *testing.T) {
	db := integrationDB(t)
	fsys := fstest.MapFS{
		"pre/0001_base.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE rollback_phase_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_base.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE rollback_phase_probe;")},
		"post/0001_index.up.sql": &fstest.MapFile{Data: []byte("CREATE INDEX rollback_phase_probe_idx ON rollback_phase_probe (id);")},
		"post/0001_index.down.sql": &fstest.MapFile{Data: []byte(
			"DROP INDEX rollback_phase_probe_idx;",
		)},
	}
	if count, err := ApplyMigrations(db, fsys, "pre"); err != nil || count != 1 {
		t.Fatalf("apply pre migration: count=%d err=%v", count, err)
	}
	if count, err := ApplyMigrations(db, fsys, "post"); err != nil || count != 1 {
		t.Fatalf("apply post migration: count=%d err=%v", count, err)
	}
	if err := RollbackMigration(db, fsys, "pre", "pre/0001_base"); err == nil ||
		!strings.Contains(err.Error(), "post-phase migrations first") {
		t.Fatalf("pre migration rollback = %v, want cross-phase order refusal", err)
	}
	if !db.Migrator().HasTable("rollback_phase_probe") || !indexExists(t, db, "rollback_phase_probe_idx") {
		t.Fatal("refused pre rollback changed schema used by the applied post phase")
	}
	for _, version := range []string{"pre/0001_base", "post/0001_index"} {
		var exists bool
		if err := db.Raw("SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)", version).Row().Scan(&exists); err != nil {
			t.Fatalf("check ledger for %s: %v", version, err)
		}
		if !exists {
			t.Fatalf("refused rollback removed migration ledger entry %s", version)
		}
	}
}

func TestLegacyBaselineRejectsDifferentExistingPrimaryKey(t *testing.T) {
	db := integrationDB(t)
	if err := db.Exec(`
		CREATE TABLE public.ai_conversation_archives (
			id uuid NOT NULL,
			wrong_key bigint PRIMARY KEY
		)`).Error; err != nil {
		t.Fatalf("create drifted legacy table: %v", err)
	}
	if _, err := ApplyMigrations(db, migrations.Files, "pre"); err == nil {
		t.Fatal("baseline accepted a different existing primary key")
	}
	var recorded bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM schema_migrations
			WHERE version = 'pre/0002_baseline'
		)`).Row().Scan(&recorded); err != nil {
		t.Fatalf("check migration ledger: %v", err)
	}
	if recorded {
		t.Fatal("drifted baseline was recorded as applied")
	}
}

func indexExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT count(*) FROM pg_indexes WHERE indexname = ?", name).Scan(&count).Error; err != nil {
		t.Fatalf("query index %s: %v", name, err)
	}
	return count > 0
}

func TestRunMigrationsAppliesAndIsIdempotent(t *testing.T) {
	// The optional development scaffolding must not rewrite any schema that the
	// versioned migrations just created.
	t.Setenv("DB_AUTOMIGRATE", "true")
	db := integrationDB(t)

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations (first): %v", err)
	}

	applied, err := appliedVersions(db)
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}
	for _, want := range []string{"pre/0001_extensions", "post/0001_conversation_owner_identity"} {
		if !applied[want] {
			t.Fatalf("expected %s recorded in schema_migrations, got %#v", want, applied)
		}
	}
	if !indexExists(t, db, "idx_ai_conversation_owner_identity") {
		t.Fatal("expected owner-scoped conversation index to exist after migration")
	}

	// Idempotent: a second run applies nothing new.
	post, err := Status(db, migrations.Files, "post")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(post.Pending) != 0 {
		t.Fatalf("post pending after apply = %#v, want none", post.Pending)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations (second): %v", err)
	}
}

func TestRollbackMigrationReversesPostMigration(t *testing.T) {
	db := integrationDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	beforeRollback, err := Status(db, migrations.Files, "post")
	if err != nil {
		t.Fatalf("Status before rollback: %v", err)
	}
	if len(beforeRollback.Applied) == 0 {
		t.Fatal("expected post migrations before rollback")
	}

	// Rollback is deliberately constrained to newest-first. Derive the current
	// applied sequence instead of freezing this integration proof to a historic
	// tail: every future reversible post migration must participate in the real
	// rollback/reapply exercise.
	for index := len(beforeRollback.Applied) - 1; index >= 0; index-- {
		version := beforeRollback.Applied[index]
		if err := RollbackMigration(db, migrations.Files, "post", version); err != nil {
			t.Fatalf("RollbackMigration(%s): %v", version, err)
		}
	}
	if indexExists(t, db, "idx_ai_conversation_owner_identity") {
		t.Fatal("owner-scoped index should be gone after rollback")
	}
	applied, err := appliedVersions(db)
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}
	if applied["post/0001_conversation_owner_identity"] {
		t.Fatal("rolled-back migration should not remain recorded")
	}
	if applied["post/0002_durable_jobs_indexes"] {
		t.Fatal("later rolled-back migration should not remain recorded")
	}
	if applied["post/0003_durable_jobs_queue_index"] {
		t.Fatal("queue-index migration should not remain recorded")
	}
	// Re-apply cleanly.
	count, err := ApplyMigrations(db, migrations.Files, "post")
	if err != nil {
		t.Fatalf("re-apply post: %v", err)
	}
	if count != len(beforeRollback.Applied) {
		t.Fatalf("re-applied %d post migrations, want %d", count, len(beforeRollback.Applied))
	}
	if !indexExists(t, db, "idx_ai_conversation_owner_identity") {
		t.Fatal("index should be restored after re-apply")
	}
}
