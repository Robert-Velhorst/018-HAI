package migrations_test

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func migrationFilesThrough(t *testing.T, version string) fs.FS {
	t.Helper()
	target := strings.TrimPrefix(strings.TrimSpace(version), "pre/")
	if target == "" || target == version {
		t.Fatalf("invalid pre-migration version %q", version)
	}
	files := fstest.MapFS{
		"pre": &fstest.MapFile{Mode: fs.ModeDir},
	}
	entries, err := fs.ReadDir(migrations.Files, "pre")
	if err != nil {
		t.Fatalf("read embedded pre migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".up.sql") && !strings.HasSuffix(name, ".down.sql")) {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql")
		if stem > target {
			continue
		}
		data, err := fs.ReadFile(migrations.Files, "pre/"+name)
		if err != nil {
			t.Fatalf("read embedded migration %s: %v", name, err)
		}
		files["pre/"+name] = &fstest.MapFile{Data: data, Mode: 0o600}
	}
	return files
}

// openIsolatedMigrationDatabase creates a database for one destructive test.
// The configured database is used only as a connection template; its schema
// and data are never modified by migration lifecycle tests.
func openIsolatedMigrationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	adminConfig, err := isolatedMigrationDatabaseConfig(dsn)
	if err != nil {
		t.Fatalf("refusing unsafe isolated migration database configuration: %s", isolatedMigrationFailure(err))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	adminSQL := stdlib.OpenDB(*adminConfig)
	adminSQL.SetMaxOpenConns(1)
	adminSQL.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := adminSQL.Close(); err != nil {
			t.Errorf("cannot close isolated migration administration pool: %s", isolatedMigrationFailure(err))
		}
	})
	var administrationDatabase string
	if err := adminSQL.QueryRowContext(ctx, "SELECT pg_catalog.current_database()").Scan(&administrationDatabase); err != nil || administrationDatabase != adminConfig.Database {
		t.Fatalf("cannot verify dedicated migration administration database: %s", isolatedMigrationFailure(err))
	}

	databaseName := "hai_migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedDatabase := pgx.Identifier{databaseName}.Sanitize()
	// Never reuse a colliding name or derive cleanup ownership from existence.
	if _, err := adminSQL.ExecContext(ctx, "CREATE DATABASE "+quotedDatabase+" TEMPLATE template0"); err != nil {
		t.Fatalf("cannot create a new owned migration test database: %s", isolatedMigrationFailure(err))
	}
	var ownedDatabaseOID int64
	t.Cleanup(func() {
		// Test pools and connections close first. Never kill other sessions.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		var currentOID int64
		if ownedDatabaseOID == 0 {
			t.Error("cannot prove isolated migration database ownership; refusing cleanup")
			return
		}
		if err := adminSQL.QueryRowContext(cleanupCtx, "SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = $1", databaseName).Scan(&currentOID); err != nil || currentOID != ownedDatabaseOID {
			t.Errorf("isolated migration database identity changed or unavailable; refusing cleanup: %s", isolatedMigrationFailure(err))
			return
		}
		if _, err := adminSQL.ExecContext(cleanupCtx, "DROP DATABASE "+quotedDatabase); err != nil {
			t.Errorf("cannot drop owned migration test database %s: %s", databaseName, isolatedMigrationFailure(err))
		}
	})
	if err := adminSQL.QueryRowContext(ctx, "SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = $1", databaseName).Scan(&ownedDatabaseOID); err != nil {
		t.Fatalf("cannot record newly created migration test database identity: %s", isolatedMigrationFailure(err))
	}

	testConfig := adminConfig.Copy()
	testConfig.Database = databaseName
	testSQL := stdlib.OpenDB(*testConfig)
	testSQL.SetMaxOpenConns(4)
	testSQL.SetMaxIdleConns(4)
	t.Cleanup(func() {
		if err := testSQL.Close(); err != nil {
			t.Errorf("cannot close isolated migration test pool: %s", isolatedMigrationFailure(err))
		}
	})
	testDB, err := gorm.Open(postgres.New(postgres.Config{Conn: testSQL}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("cannot open isolated migration database: %s", isolatedMigrationFailure(err))
	}
	var connectedDatabase, searchPath string
	if err := testSQL.QueryRowContext(ctx, "SELECT pg_catalog.current_database(), pg_catalog.current_setting('search_path')").Scan(&connectedDatabase, &searchPath); err != nil ||
		connectedDatabase != databaseName || searchPath != testConfig.RuntimeParams["search_path"] {
		t.Fatalf("isolated migration database identity or namespace differs from its owned target: %s", isolatedMigrationFailure(err))
	}
	return testDB.WithContext(ctx)
}

func isolatedMigrationDatabaseConfig(dsn string) (*pgx.ConnConfig, error) {
	if err := pgtestguard.ValidateDedicatedPostgresTestDSN(dsn, "hai_migration_runner_test"); err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
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
	return config, nil
}

// SQLSTATE/deadline diagnostics preserve the failure class without echoing DSNs.
func isolatedMigrationFailure(err error) string {
	if err == nil {
		return "unexpected database identity"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return "SQLSTATE " + state.SQLState()
	}
	return "connection or database I/O failure; private diagnostics withheld"
}

func isLoopbackPostgresConfig(config *pgx.ConnConfig) bool {
	if config == nil || !isLoopbackPostgresHost(config.Host) {
		return false
	}
	for _, fallback := range config.Fallbacks {
		if fallback == nil || !isLoopbackPostgresHost(fallback.Host) {
			return false
		}
	}
	return true
}

func TestIsLoopbackPostgresConfig(t *testing.T) {
	for _, test := range []struct {
		name  string
		hosts string
		local bool
	}{
		{name: "loopback", hosts: "127.0.0.1", local: true},
		{name: "localhost", hosts: "localhost", local: true},
		{name: "all loopback fallbacks", hosts: "127.0.0.1,::1,localhost", local: true},
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
			if got := isLoopbackPostgresConfig(config); got != test.local {
				t.Fatalf("isLoopbackPostgresConfig = %t, want %t", got, test.local)
			}
		})
	}
	if isLoopbackPostgresConfig(nil) {
		t.Fatal("nil config must be rejected")
	}
}

func isLoopbackPostgresHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func TestIsLoopbackPostgresHost(t *testing.T) {
	for _, test := range []struct {
		host  string
		local bool
	}{
		{host: "localhost", local: true},
		{host: "LOCALHOST", local: true},
		{host: "127.0.0.1", local: true},
		{host: "::1", local: true},
		{host: "[::1]", local: true},
		{host: "db.internal", local: false},
		{host: "8.8.8.8", local: false},
		{host: "localhost.example", local: false},
	} {
		t.Run(test.host, func(t *testing.T) {
			if got := isLoopbackPostgresHost(test.host); got != test.local {
				t.Fatalf("isLoopbackPostgresHost(%q) = %t, want %t", test.host, got, test.local)
			}
		})
	}
}
