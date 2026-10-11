package infra

import "testing"

func TestSafePostgresIdentifier(t *testing.T) {
	for _, value := range []string{"hai_runtime", "runtime2", "Automation_DB"} {
		if !safePostgresIdentifier(value) {
			t.Fatalf("safe identifier %q was rejected", value)
		}
	}
	for _, value := range []string{"", "2runtime", "runtime-user", `runtime";DROP ROLE postgres;--`, "runtime user"} {
		if safePostgresIdentifier(value) {
			t.Fatalf("unsafe identifier %q was accepted", value)
		}
	}
}

func TestQuotePostgresLiteralEscapesPassword(t *testing.T) {
	if got, want := quotePostgresLiteral("one'two"), `'one''two'`; got != want {
		t.Fatalf("quotePostgresLiteral() = %q, want %q", got, want)
	}
}

func TestDatabaseMigrationsCanBeDisabledExplicitly(t *testing.T) {
	t.Setenv("DB_RUN_MIGRATIONS", "false")
	if databaseMigrationsEnabled() {
		t.Fatal("DB_RUN_MIGRATIONS=false did not disable runtime migrations")
	}
	t.Setenv("DB_RUN_MIGRATIONS", "unexpected")
	if !databaseMigrationsEnabled() {
		t.Fatal("unknown DB_RUN_MIGRATIONS value did not fail safe to migrations enabled")
	}
}

func TestConfiguredRuntimeRoleRejectsPlaceholderBeforeDatabaseAccess(t *testing.T) {
	t.Setenv(runtimeDBUserEnv, "hai_runtime")
	t.Setenv(runtimeDBPasswordEnv, "change-this-runtime-database-password")
	if err := ProvisionConfiguredRuntimeRole(nil, "automation_hub"); err == nil {
		t.Fatal("placeholder runtime database password was accepted")
	}
}
