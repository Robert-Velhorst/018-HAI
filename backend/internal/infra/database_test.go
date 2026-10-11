package infra

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestMigrationsEnabledDefaultsToTrueAndCanBeDisabled(t *testing.T) {
	t.Setenv("DB_RUN_MIGRATIONS", "")
	if !databaseMigrationsEnabled() {
		t.Fatal("databaseMigrationsEnabled() = false, want legacy-compatible default true")
	}

	t.Setenv("DB_RUN_MIGRATIONS", "false")
	if databaseMigrationsEnabled() {
		t.Fatal("databaseMigrationsEnabled() = true, want false when explicitly disabled")
	}
}

func TestConfiguredRuntimeRoleRequiresBothValuesAndStrongSecret(t *testing.T) {
	t.Setenv("DB_RUNTIME_USER", "")
	t.Setenv("DB_RUNTIME_PASSWORD", "")
	if err := ProvisionConfiguredRuntimeRole(nil, "hai"); err != nil {
		t.Fatalf("empty runtime role configuration error = %v", err)
	}

	t.Setenv("DB_RUNTIME_USER", "hai_runtime")
	t.Setenv("DB_RUNTIME_PASSWORD", "")
	if err := ProvisionConfiguredRuntimeRole(nil, "hai"); err == nil {
		t.Fatal("runtime role configuration without password unexpectedly succeeded")
	}

	t.Setenv("DB_RUNTIME_USER", "hai_runtime")
	t.Setenv("DB_RUNTIME_PASSWORD", "short")
	if err := ProvisionConfiguredRuntimeRole(nil, "hai"); err == nil {
		t.Fatal("runtime role configuration with a weak password unexpectedly succeeded")
	}

	t.Setenv("DB_RUNTIME_PASSWORD", "safe-password-safe-password-safe")
	if err := ProvisionConfiguredRuntimeRole(nil, "hai"); err == nil {
		t.Fatal("runtime role configuration without a provisioning database unexpectedly succeeded")
	}
}

func TestGetDefaultDBCachesOnlySuccessfulMigration(t *testing.T) {
	resetDefaultDBForTest()
	defer resetDefaultDBForTest()

	originalOpen := openConfiguredDB
	originalMigrate := runDefaultMigrations
	defer func() {
		openConfiguredDB = originalOpen
		runDefaultMigrations = originalMigrate
	}()

	db := &gorm.DB{}
	opens := 0
	migrations := 0
	openConfiguredDB = func() (*gorm.DB, error) {
		opens++
		return db, nil
	}
	runDefaultMigrations = func(got *gorm.DB) error {
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

	originalOpen := openConfiguredDB
	originalMigrate := runDefaultMigrations
	defer func() {
		openConfiguredDB = originalOpen
		runDefaultMigrations = originalMigrate
	}()

	opens := 0
	migrations := 0
	openConfiguredDB = func() (*gorm.DB, error) {
		opens++
		return nil, nil
	}
	runDefaultMigrations = func(*gorm.DB) error {
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
