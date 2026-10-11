package infra

import "testing"

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
