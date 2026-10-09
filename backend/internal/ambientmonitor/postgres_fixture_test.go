package ambientmonitor

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const ambientMonitorPostgresTestDSNEnv = "HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN"
const ambientMonitorPostgresTestDatabase = "hai_ambient_monitor_test"

func openAmbientMonitorPostgresTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	// Validate before even constructing a connection or applying migrations.
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, ambientMonitorPostgresTestDSNEnv, ambientMonitorPostgresTestDatabase)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get postgres connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close postgres fixture: %v", err)
		}
	})
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations to ambient monitor test database: %v", err)
	}
	return db
}

func TestAmbientMonitorPostgresFixtureGuard(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, dsn, optIn, message string
		fails                     bool
	}{
		{"missing DSN", "", "true", "skipping destructive PostgreSQL test", false},
		{"missing opt-in", "host=127.0.0.1 dbname=hai_ambient_monitor_test", "", "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required", false},
		{"false opt-in", "host=127.0.0.1 dbname=hai_ambient_monitor_test", "false", "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required", false},
		{"not explicit opt-in", "host=127.0.0.1 dbname=hai_ambient_monitor_test", "1", "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required", false},
		{"wrong database", "host=127.0.0.1 dbname=postgres", "true", "does not exactly match", true},
		{"database suffix", "host=127.0.0.1 dbname=hai_ambient_monitor_test_shadow", "true", "does not exactly match", true},
		{"hostname", "host=localhost dbname=hai_ambient_monitor_test", "true", "literal loopback", true},
		{"remote host", "host=192.0.2.10 dbname=hai_ambient_monitor_test", "true", "literal loopback", true},
		{"socket", "host=/tmp dbname=hai_ambient_monitor_test", "true", "literal loopback", true},
		{"remote fallback", "host=127.0.0.1,192.0.2.10 dbname=hai_ambient_monitor_test", "true", "multi-host", true},
		{"malformed", "not a postgres dsn", "true", "DSN is invalid", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestAmbientMonitorPostgresFixtureGuardProbe$", "-test.v")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if key != ambientMonitorPostgresTestDSNEnv && key != "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS" && key != "HAI_AMBIENT_MONITOR_GUARD_PROBE" {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, ambientMonitorPostgresTestDSNEnv+"="+test.dsn,
				"HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS="+test.optIn, "HAI_AMBIENT_MONITOR_GUARD_PROBE=true")
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err != nil) != test.fails || !strings.Contains(string(output), test.message) ||
				strings.Contains(string(output), "open postgres:") || strings.Contains(string(output), "apply migrations") {
				t.Fatalf("fixture guard: error=%v context=%v output=%s", err, ctx.Err(), output)
			}
		})
	}
}

func TestAmbientMonitorPostgresFixtureGuardProbe(t *testing.T) {
	if os.Getenv("HAI_AMBIENT_MONITOR_GUARD_PROBE") != "true" {
		t.Skip("fixture guard subprocess only")
	}
	openAmbientMonitorPostgresTestDatabase(t)
	t.Fatal("unsafe fixture reached database setup")
}

func TestAmbientMonitorPostgresFixtureAcceptsLiteralLoopback(t *testing.T) {
	for _, dsn := range []string{
		"postgres://tester@127.0.0.1/hai_ambient_monitor_test?sslmode=disable",
		"postgres://tester@[::1]/hai_ambient_monitor_test?sslmode=prefer",
	} {
		t.Setenv(ambientMonitorPostgresTestDSNEnv, dsn)
		t.Setenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS", "true")
		if got := pgtestguard.RequireDedicatedPostgresTestDSN(t, ambientMonitorPostgresTestDSNEnv, ambientMonitorPostgresTestDatabase); got != dsn {
			t.Fatalf("accepted DSN = %q, want %q", got, dsn)
		}
	}
}
