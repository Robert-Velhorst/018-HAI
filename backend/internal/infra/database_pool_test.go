package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func clearPostgresPoolEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_IDLE_TIME", "DB_CONN_MAX_LIFETIME", "DB_CONNECT_TIMEOUT", "DB_OPEN_TIMEOUT", "DB_SSLMODE", "HAI_LOCAL_COMPOSE_DATABASE"} {
		t.Setenv(name, "")
	}
	t.Setenv("RUN_MODE", "demo")
}

func TestPostgresPoolSettingsDefaultsAndOverrides(t *testing.T) {
	clearPostgresPoolEnv(t)
	got, err := postgresPoolSettingsFromEnv()
	want := postgresPoolSettings{8, 2, 2 * time.Minute, 30 * time.Minute, 5 * time.Second, 10 * time.Second}
	if err != nil || got != want {
		t.Fatalf("defaults = %+v, %v; want %+v", got, err, want)
	}
	t.Setenv("DB_MAX_OPEN_CONNS", " 4 ")
	t.Setenv("DB_MAX_IDLE_CONNS", "0")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "15s")
	t.Setenv("DB_CONN_MAX_LIFETIME", "10m")
	t.Setenv("DB_CONNECT_TIMEOUT", "500ms")
	t.Setenv("DB_OPEN_TIMEOUT", "3s")
	got, err = postgresPoolSettingsFromEnv()
	want = postgresPoolSettings{4, 0, 15 * time.Second, 10 * time.Minute, 500 * time.Millisecond, 3 * time.Second}
	if err != nil || got != want {
		t.Fatalf("overrides = %+v, %v; want %+v", got, err, want)
	}
}

func TestPostgresPoolSettingsRejectUnsafeOrInvalidValues(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"DB_MAX_OPEN_CONNS", "0"}, {"DB_MAX_OPEN_CONNS", "1"}, {"DB_MAX_OPEN_CONNS", "-1"},
		{"DB_MAX_OPEN_CONNS", "9999999999999999999999999999"}, {"DB_MAX_OPEN_CONNS", "not-an-integer"},
		{"DB_MAX_IDLE_CONNS", "-1"}, {"DB_MAX_IDLE_CONNS", "9"}, {"DB_MAX_IDLE_CONNS", "private-value"},
		{"DB_CONN_MAX_IDLE_TIME", "0s"}, {"DB_CONN_MAX_LIFETIME", "-1m"},
		{"DB_CONNECT_TIMEOUT", "5"}, {"DB_OPEN_TIMEOUT", "secret-invalid-duration"},
		{"DB_CONN_MAX_LIFETIME", "999999999999999999999999h"},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			clearPostgresPoolEnv(t)
			t.Setenv(test.key, test.value)
			_, err := postgresPoolSettingsFromEnv()
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("invalid setting must identify its key: %v", err)
			}
			if strings.Contains(test.value, "secret") && strings.Contains(err.Error(), test.value) {
				t.Fatal("configuration error exposed raw input")
			}
		})
	}
}

func TestPostgresConnectionConfigPreservesCredentialFields(t *testing.T) {
	clearPostgresPoolEnv(t)
	settings, err := postgresPoolSettingsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ host, user, password, database string }{
		{"localhost", "tester", "with spaces ' \\ &?#% sslmode=require", "database &timezone=wrong"},
		{"::1", "tester@+ ?", "synthetic:/@'", "/database/name"},
		{"/synthetic/socket", "tester", "synthetic-password", "synthetic_database"},
	} {
		cfg, err := postgresConnectionConfig(test.user, test.password, test.database, test.host, 5432, settings)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.User != test.user || cfg.Password != test.password || cfg.Database != test.database || cfg.Host != test.host || cfg.Port != 5432 {
			t.Fatal("structured configuration did not preserve fields")
		}
		if cfg.TLSConfig != nil || len(cfg.Fallbacks) != 0 || cfg.RuntimeParams["timezone"] != "UTC" || cfg.ConnectTimeout != 5*time.Second {
			t.Fatal("credential content changed connection policy")
		}
	}
}

func TestPostgresSSLModeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, runMode, sslMode, host, localComposeMarker string
		wantErr, wantTLS                                 bool
	}{
		{"local default", "demo", "", "127.0.0.1", "", false, false},
		{"local explicit disable", "test", "disable", "127.0.0.1", "", false, false},
		{"production local compose database", "production", "disable", "postgres-automation", "true", false, false},
		{"production local host requires marker", "production", "disable", "postgres-automation", "", true, false},
		{"production local host rejects wrong marker", "production", "disable", "postgres-automation", "false", true, false},
		{"production marker rejects other internal database", "production", "disable", "postgres-idp", "true", true, false},
		{"production marker rejects remote host", "production", "disable", "db.example.net", "true", true, false},
		{"production requires mode", "production", "", "db.example.net", "", true, false},
		{"production rejects disable", "production", "disable", "db.example.net", "", true, false},
		{"production rejects unverified TLS", "production", "require", "db.example.net", "", true, false},
		{"production verify ca", "production", "verify-ca", "db.example.net", "", false, true},
		{"production verify full", "production", "verify-full", "db.example.net", "", false, true},
		{"unknown mode fails closed", "staging", "disable", "db.example.net", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RUN_MODE", tc.runMode)
			t.Setenv("DB_SSLMODE", tc.sslMode)
			t.Setenv("HAI_LOCAL_COMPOSE_DATABASE", tc.localComposeMarker)
			settings, err := postgresPoolSettingsFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := postgresConnectionConfig("tester", "synthetic", "db", tc.host, 5432, settings)
			if (err != nil) != tc.wantErr {
				t.Fatalf("connection config error = %v", err)
			}
			if err == nil && (cfg.TLSConfig != nil) != tc.wantTLS {
				t.Fatalf("TLS configured = %t, want %t", cfg.TLSConfig != nil, tc.wantTLS)
			}
		})
	}
}

func TestPostgresOpenRejectsCancelledOrInvalidInputWithoutConnection(t *testing.T) {
	clearPostgresPoolEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := NewPostgresDatabaseContext(ctx, "tester", "unused", "synthetic", "must-not-resolve.invalid", 5432); db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open = %v, %v", db, err)
	}
	if db, err := NewPostgresDatabaseContext(nil, "tester", "unused", "synthetic", "must-not-resolve.invalid", 5432); db != nil || err == nil {
		t.Fatal("nil context accepted")
	}
	for _, port := range []int{-1, 0, 65536} {
		if db, err := NewPostgresDatabaseContext(context.Background(), "tester", "unused", "synthetic", "must-not-resolve.invalid", port); db != nil || err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	t.Setenv("DB_MAX_OPEN_CONNS", "0")
	if db, err := NewPostgresDatabaseContext(context.Background(), "tester", "unused", "synthetic", "must-not-resolve.invalid", 5432); db != nil || err == nil {
		t.Fatal("unlimited pool accepted")
	}
}

// This driver exercises database/sql ownership and deadlines without networking.
type postgresPoolTestConnector struct {
	opened, closed atomic.Int64
	beforeConnect  func(context.Context) error
	ping           func(context.Context) error
	beforeClose    func() error
}

func (c *postgresPoolTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.beforeConnect != nil {
		if err := c.beforeConnect(ctx); err != nil {
			return nil, err
		}
	}
	c.opened.Add(1)
	return &postgresPoolTestConn{connector: c}, nil
}
func (c *postgresPoolTestConnector) Driver() driver.Driver { return postgresPoolTestDriver{} }

type postgresPoolTestDriver struct{}

func (postgresPoolTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector only")
}

type postgresPoolTestConn struct{ connector *postgresPoolTestConnector }

func (*postgresPoolTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("no SQL permitted")
}
func (*postgresPoolTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("no transactions permitted")
}
func (c *postgresPoolTestConn) Close() error {
	c.connector.closed.Add(1)
	if c.connector.beforeClose != nil {
		return c.connector.beforeClose()
	}
	return nil
}
func (c *postgresPoolTestConn) Ping(ctx context.Context) error {
	if c.connector.ping != nil {
		return c.connector.ping(ctx)
	}
	return ctx.Err()
}

func TestPostgresPoolCapsBeforeFirstPingAndReleasesExcessIdle(t *testing.T) {
	clearPostgresPoolEnv(t)
	settings, err := postgresPoolSettingsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	connector := &postgresPoolTestConnector{}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	connector.ping = func(ctx context.Context) error {
		if pool.Stats().MaxOpenConnections != settings.maxOpen {
			t.Fatal("first ping used an uncapped pool")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("first ping has no timeout")
		}
		return nil
	}
	db, err := initializePostgresPool(context.Background(), pool, settings)
	if err != nil {
		t.Fatal(err)
	}
	gotPool, err := db.DB()
	if err != nil || gotPool != pool {
		t.Fatal("gorm did not retain the owned pool")
	}
	var held []*sql.Conn
	for i := 0; i < settings.maxOpen; i++ {
		conn, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		held = append(held, conn)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	if conn != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted pool did not wait within caller deadline: %v", err)
	}
	if connector.opened.Load() != int64(settings.maxOpen) {
		t.Fatal("pool exceeded its connection cap")
	}
	for _, conn := range held {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	stats := pool.Stats()
	if stats.Idle != 2 || stats.OpenConnections != 2 || stats.MaxIdleClosed != 6 || connector.closed.Load() != 6 {
		t.Fatalf("idle cap not enforced: %+v", stats)
	}
}

func TestPostgresFutureConnectionAcquisitionIsBounded(t *testing.T) {
	for _, callerShorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "connector timeout", true: "caller timeout"}[callerShorter], func(t *testing.T) {
			connector := &postgresPoolTestConnector{}
			pool := sql.OpenDB(postgresBoundedConnector{Connector: connector, timeout: 30 * time.Millisecond})
			t.Cleanup(func() { _ = pool.Close() })
			pool.SetMaxOpenConns(2)
			pool.SetMaxIdleConns(0)
			if err := pool.Ping(); err != nil {
				t.Fatal(err)
			}
			connector.beforeConnect = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			ctx := context.Background()
			if callerShorter {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 15*time.Millisecond)
				defer cancel()
			}
			if err := pool.PingContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("future connect did not expire: %v", err)
			}
			if connector.opened.Load() != 1 || connector.closed.Load() != 1 || pool.Stats().OpenConnections != 0 {
				t.Fatal("timed-out acquisition left a connection open")
			}
		})
	}
}

func TestPostgresInitialPingErrorClosesOwnedPool(t *testing.T) {
	failure := errors.New("synthetic authentication error")
	connector := &postgresPoolTestConnector{ping: func(context.Context) error { return failure }}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	settings := postgresPoolSettings{2, 1, time.Minute, time.Minute, time.Second, time.Second}
	if db, err := initializePostgresPool(context.Background(), pool, settings); db != nil || !errors.Is(err, failure) {
		t.Fatalf("ping failure = %v, %v", db, err)
	}
	if connector.closed.Load() != 1 || pool.Ping() == nil {
		t.Fatal("failed owned pool remains usable")
	}
}

func TestPostgresPoolIdleLifetimeIsApplied(t *testing.T) {
	settings := postgresPoolSettings{2, 1, time.Millisecond, time.Hour, time.Second, time.Second}
	connector := &postgresPoolTestConnector{}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	if _, err := initializePostgresPool(context.Background(), pool, settings); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for pool.Stats().MaxIdleTimeClosed == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pool.Stats().MaxIdleTimeClosed == 0 || connector.closed.Load() == 0 {
		t.Fatal("idle lifetime was not applied")
	}
}

func TestPostgresInitialPingDeadlineAndFailureCloseOwnedPool(t *testing.T) {
	for _, callerShorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "open timeout", true: "caller timeout"}[callerShorter], func(t *testing.T) {
			settings := postgresPoolSettings{2, 1, time.Minute, time.Minute, time.Second, 30 * time.Millisecond}
			ctx := context.Background()
			if callerShorter {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 15*time.Millisecond)
				defer cancel()
				settings.openTimeout = time.Second
			}
			connector := &postgresPoolTestConnector{ping: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
			pool := sql.OpenDB(connector)
			t.Cleanup(func() { _ = pool.Close() })
			db, err := initializePostgresPool(ctx, pool, settings)
			if db != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timed-out open = %v, %v", db, err)
			}
			if connector.opened.Load() != 1 || connector.closed.Load() != 1 || pool.Stats().OpenConnections != 0 {
				t.Fatal("failed ping left an owned connection open")
			}
			if err := pool.Ping(); err == nil {
				t.Fatal("failed pool remains usable")
			}
		})
	}
}

func TestPostgresPoolConnectionLifetimeIsApplied(t *testing.T) {
	settings := postgresPoolSettings{2, 1, time.Minute, time.Millisecond, time.Second, time.Second}
	connector := &postgresPoolTestConnector{}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	if _, err := initializePostgresPool(context.Background(), pool, settings); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := pool.Ping(); err != nil {
		t.Fatal(err)
	}
	if pool.Stats().MaxLifetimeClosed == 0 || connector.closed.Load() == 0 {
		t.Fatal("connection lifetime was not applied")
	}
}

func TestGetDefaultDBMigrationFailureClosesOnlyFailedPool(t *testing.T) {
	resetDefaultDBForTest()
	defer resetDefaultDBForTest()
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	originalOpen, originalMigrate := openConfiguredDB, runDefaultMigrations
	defer func() { openConfiguredDB, runDefaultMigrations = originalOpen, originalMigrate }()
	var pools []*sql.DB
	var connectors []*postgresPoolTestConnector
	openConfiguredDB = func(context.Context) (*gorm.DB, error) {
		connector := &postgresPoolTestConnector{}
		pool := sql.OpenDB(connector)
		t.Cleanup(func() { _ = pool.Close() })
		if err := pool.Ping(); err != nil {
			t.Fatal(err)
		}
		pools, connectors = append(pools, pool), append(connectors, connector)
		return gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	}
	migrationFailure := errors.New("synthetic migration failure")
	runDefaultMigrations = func(context.Context, *gorm.DB) error {
		if len(pools) == 1 {
			return migrationFailure
		}
		return nil
	}
	if db, err := GetDefaultDB(); db != nil || !errors.Is(err, migrationFailure) {
		t.Fatalf("failed startup = %v, %v", db, err)
	}
	if connectors[0].closed.Load() != 1 || pools[0].Stats().OpenConnections != 0 || pools[0].Ping() == nil {
		t.Fatal("failed migration did not close its pool")
	}
	db, err := GetDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := GetDefaultDB(); err != nil || cached != db || len(pools) != 2 {
		t.Fatal("successful retry not cached")
	}
	if connectors[1].closed.Load() != 0 || pools[1].Ping() != nil {
		t.Fatal("successful cached pool was closed")
	}
}
