//go:build integration

// These tests run only under `-tags integration` against a real Postgres
// pointed to by HAI_TEST_DATABASE_DSN. They prove the versioned migration
// runner (and the full schema) apply against the actual configured engine, not
// a mock. Normal `go test ./...` never compiles or runs them.
package infra

import (
	"os"
	"strings"
	"sync"
	"testing"

	"automation-hub-backend/migrations"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// Start from a clean schema so the run is reproducible. Extensions must be
	// removed before their schema; otherwise PostgreSQL retains an extension
	// catalog entry after CASCADE drops its functions, and IF NOT EXISTS cannot
	// recreate those functions on the next migration pass.
	if err := db.Exec(`
		DROP EXTENSION IF EXISTS "uuid-ossp" CASCADE;
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
	`).Error; err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return db
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
	postMigrations, err := loadMigrations(migrations.Files, "post")
	if err != nil {
		t.Fatalf("load post migrations: %v", err)
	}
	if len(postMigrations) < 4 {
		t.Fatalf("loaded %d post migrations, want at least 4", len(postMigrations))
	}

	// The rollback API must continue to reject a stale caller that attempts to
	// remove an earlier migration while a newer dependency remains applied.
	if err := RollbackMigration(
		db,
		migrations.Files,
		"post",
		"post/0003_durable_jobs_queue_index",
	); err == nil || !strings.Contains(err.Error(), "later migration") {
		t.Fatalf("out-of-order rollback error = %v, want later-migration refusal", err)
	}

	for i := len(postMigrations) - 1; i >= 0; i-- {
		version := postMigrations[i].Version
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
	if count != len(postMigrations) {
		t.Fatalf("re-applied %d post migrations, want %d", count, len(postMigrations))
	}
	if !indexExists(t, db, "idx_ai_conversation_owner_identity") {
		t.Fatal("index should be restored after re-apply")
	}
}
