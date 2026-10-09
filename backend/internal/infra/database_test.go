package infra

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestOpenDefaultDBContextRejectsCancellationBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := OpenDefaultDBContext(ctx); db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open = %v, %v", db, err)
	}
	if db, err := OpenDefaultDBContext(nil); db != nil || err == nil {
		t.Fatalf("context-free open = %v, %v", db, err)
	}
}

func TestGetDefaultDBCachesOnlySuccessfulMigration(t *testing.T) {
	resetDefaultDBForTest()
	defer resetDefaultDBForTest()
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")

	originalOpen := openConfiguredDB
	originalMigrate := runDefaultMigrations
	defer func() {
		openConfiguredDB = originalOpen
		runDefaultMigrations = originalMigrate
	}()

	db := &gorm.DB{}
	opens := 0
	migrations := 0
	openConfiguredDB = func(context.Context) (*gorm.DB, error) {
		opens++
		return db, nil
	}
	runDefaultMigrations = func(_ context.Context, got *gorm.DB) error {
		migrations++
		if got != db {
			t.Fatal("migration received a different database connection")
		}
		return nil
	}

	first, err := GetDefaultDB()
	if err != nil {
		t.Fatalf("first GetDefaultDB: %v", err)
	}
	second, err := GetDefaultDB()
	if err != nil {
		t.Fatalf("second GetDefaultDB: %v", err)
	}
	if first != db || second != db {
		t.Fatalf("GetDefaultDB = %p, %p; want cached %p", first, second, db)
	}
	if opens != 1 || migrations != 1 {
		t.Fatalf("open/migration calls = %d/%d, want 1/1", opens, migrations)
	}
}

func TestGetDefaultDBRetriesAfterMigrationFailure(t *testing.T) {
	resetDefaultDBForTest()
	defer resetDefaultDBForTest()
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")

	originalOpen := openConfiguredDB
	originalMigrate := runDefaultMigrations
	defer func() {
		openConfiguredDB = originalOpen
		runDefaultMigrations = originalMigrate
	}()

	opens := 0
	migrations := 0
	openConfiguredDB = func(context.Context) (*gorm.DB, error) {
		opens++
		return &gorm.DB{}, nil
	}
	runDefaultMigrations = func(context.Context, *gorm.DB) error {
		migrations++
		if migrations == 1 {
			return errors.New("temporary migration failure")
		}
		return nil
	}

	if _, err := GetDefaultDB(); err == nil {
		t.Fatal("first GetDefaultDB should surface migration failure")
	}
	if _, err := GetDefaultDB(); err != nil {
		t.Fatalf("GetDefaultDB should retry after a failed migration: %v", err)
	}
	if opens != 2 || migrations != 2 {
		t.Fatalf("open/migration calls = %d/%d, want 2/2", opens, migrations)
	}
}

func TestGetDefaultDBSkipsStartupMigrationsWhenDisabled(t *testing.T) {
	resetDefaultDBForTest()
	defer resetDefaultDBForTest()
	t.Setenv("DB_MIGRATIONS_ENABLED", "false")

	originalOpen := openConfiguredDB
	originalMigrate := runDefaultMigrations
	defer func() {
		openConfiguredDB = originalOpen
		runDefaultMigrations = originalMigrate
	}()

	openConfiguredDB = func(context.Context) (*gorm.DB, error) { return &gorm.DB{}, nil }
	runDefaultMigrations = func(context.Context, *gorm.DB) error {
		t.Fatal("startup migrations must be skipped for a runtime-only database role")
		return nil
	}

	if _, err := GetDefaultDB(); err != nil {
		t.Fatalf("GetDefaultDB with startup migrations disabled: %v", err)
	}
}

func TestMigrationsEnabledAtStartupDefaultsTrueAndFailsClosed(t *testing.T) {
	t.Setenv("DB_MIGRATIONS_ENABLED", "")
	if !migrationsEnabledAtStartup() {
		t.Fatal("empty migration setting should preserve compatible startup behavior")
	}
	t.Setenv("DB_MIGRATIONS_ENABLED", "not-a-bool")
	if migrationsEnabledAtStartup() {
		t.Fatal("invalid migration setting must not grant schema-changing startup behavior")
	}
}
