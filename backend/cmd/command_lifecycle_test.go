package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type commandTestConnector struct {
	opened, closed, queried, executed, begun, committed, rolledBack atomic.Int32
	readOnly                                                        atomic.Bool
	queryFailure, closeFailure                                      error
	waitForDeadline                                                 bool
	ledgerPresent                                                   bool
	versions                                                        []string
	memoryRows                                                      func(context.Context) driver.Rows
	commitFailure                                                   error
	beforeFinalize                                                  func()
}

func (c *commandTestConnector) Driver() driver.Driver { return commandTestDriver{} }
func (c *commandTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("command connection has no deadline")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.opened.Add(1)
	return &commandTestConnection{connector: c}, nil
}

type commandTestDriver struct{}

func (commandTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected driver.Open")
}

type commandTestConnection struct{ connector *commandTestConnector }

func (*commandTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected statement preparation")
}
func (c *commandTestConnection) Close() error {
	c.connector.closed.Add(1)
	return c.connector.closeFailure
}
func (*commandTestConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected context-free transaction")
}
func (*commandTestConnection) Ping(ctx context.Context) error { return ctx.Err() }
func (c *commandTestConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.connector.begun.Add(1)
	c.connector.readOnly.Store(options.ReadOnly)
	return &commandTestTransaction{connector: c.connector}, nil
}
func (c *commandTestConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.connector.executed.Add(1)
	return nil, errors.New("unexpected database mutation")
}
func (c *commandTestConnection) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.connector.queried.Add(1)
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("command SQL has no deadline")
	}
	if c.connector.waitForDeadline {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.connector.queryFailure != nil {
		return nil, c.connector.queryFailure
	}
	if query == "SELECT to_regclass($1) IS NOT NULL" {
		return &commandTestRows{columns: []string{"exists"}, values: [][]driver.Value{{c.connector.ledgerPresent}}}, nil
	}
	if strings.Contains(query, "FROM pg_attribute") && strings.Contains(query, "attname = 'checksum'") {
		return &commandTestRows{columns: []string{"exists"}, values: [][]driver.Value{{true}}}, nil
	}
	if query == "SELECT version, checksum FROM schema_migrations" {
		rows := &commandTestRows{columns: []string{"version", "checksum"}}
		for _, version := range c.connector.versions {
			rows.values = append(rows.values, []driver.Value{version, ""})
		}
		return rows, nil
	}
	if query == `SELECT "id","content","kind","confidence","tags" FROM "context_memories" ORDER BY updated_at desc` {
		if c.connector.memoryRows != nil {
			return c.connector.memoryRows(ctx), nil
		}
		return &commandTestRows{columns: []string{"id"}}, nil
	}
	return nil, errors.New("unexpected read query: " + query)
}

type commandTestTransaction struct{ connector *commandTestConnector }

func (tx *commandTestTransaction) Commit() error {
	if tx.connector.beforeFinalize != nil {
		tx.connector.beforeFinalize()
	}
	tx.connector.committed.Add(1)
	return tx.connector.commitFailure
}
func (tx *commandTestTransaction) Rollback() error {
	if tx.connector.beforeFinalize != nil {
		tx.connector.beforeFinalize()
	}
	tx.connector.rolledBack.Add(1)
	return nil
}

type commandTestRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *commandTestRows) Columns() []string { return r.columns }
func (*commandTestRows) Close() error        { return nil }
func (r *commandTestRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func commandTestDatabase(t *testing.T, connector *commandTestConnector) (*gorm.DB, *sql.DB) {
	t.Helper()
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool
}

func assertCommandPoolClosed(t *testing.T, pool *sql.DB, connector *commandTestConnector) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	opened := connector.opened.Load()
	if err := pool.PingContext(ctx); err == nil || connector.opened.Load() != opened {
		t.Fatal("command retained a usable database pool")
	}
}

func TestMainRejectsInvalidArgumentsBeforeStartup(t *testing.T) {
	cases := [][]string{{"migrte", "status"}, {"migrate", "up", "--dry-run"}, {"reconcile", "--apply"}}
	if probe := os.Getenv("HAI_CLI_INVALID_ARGUMENT_PROBE"); probe != "" {
		for i, args := range cases {
			if probe == string(rune('a'+i)) {
				os.Args = append([]string{"backend"}, args...)
				main()
				t.Fatal("invalid command unexpectedly returned from main")
				return
			}
		}
		t.Fatal("unknown subprocess probe")
		return
	}
	for i, args := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainRejectsInvalidArgumentsBeforeStartup$")
		command.Env = append(os.Environ(), "HAI_CLI_INVALID_ARGUMENT_PROBE="+string(rune('a'+i)), "SERVER_PORT=65536")
		output, err := command.CombinedOutput()
		cancel()
		var exit *exec.ExitError
		want := validateCommandArguments(args)
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || strings.TrimSpace(string(output)) != want.Error() {
			t.Fatalf("invalid CLI %q: exit=%v output=%q", args, err, output)
		}
	}
}

func TestCommandArgumentsFailClosed(t *testing.T) {
	for _, args := range [][]string{
		{"migrte", "status"}, {"--help"}, {"doctor", "--extra"}, {"reconcile", "--apply"},
		{"migrate", "up", "--dry-run"}, {"migrate", "status", "--dry-run"},
		{"migrate", "down"}, {"migrate", "down", "0001_version", "--dry-run"},
		{"migrate", "down", "--dry-run"}, {"migrate", "down", "pre/--dry-run"},
		{"migrate", "down", "pre/../secret"}, {"migrate", "down", "post/"},
	} {
		if err := validateCommandArguments(args); err == nil {
			t.Fatalf("invalid command accepted: %q", args)
		}
	}
	for _, args := range [][]string{nil, {"doctor"}, {"reconcile"}, {"migrate"}, {"migrate", "status"}, {"migrate", "up"}, {"migrate", "down", "pre/0001_version"}} {
		if err := validateCommandArguments(args); err != nil {
			t.Fatalf("valid command %q refused: %v", args, err)
		}
	}
}

func TestInvalidMigrationArgumentsPrecedeConfiguration(t *testing.T) {
	t.Setenv("SERVER_PORT", "65536")
	for _, args := range [][]string{{"up", "--dry-run"}, {"down", "--dry-run"}, {"status", "extra"}, {"unknown"}} {
		if code := runMigrate(args); code == 0 {
			t.Fatalf("invalid migration request succeeded: %q", args)
		}
	}
}

func TestReconcileUsesOnlyReadOnlyTransactionAndClosesPool(t *testing.T) {
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	t.Setenv("HAI_COMMAND_TIMEOUT", "1s")
	for _, queryFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "query_failure"}[queryFails], func(t *testing.T) {
			connector := &commandTestConnector{}
			if queryFails {
				connector.queryFailure = errors.New("synthetic missing schema")
			}
			db, pool := commandTestDatabase(t, connector)
			code := runReconcileWithDatabase(func(context.Context) (*gorm.DB, error) { return db, nil })
			if (code != 0) != queryFails {
				t.Fatalf("reconcile exit = %d, query failure = %v", code, queryFails)
			}
			if !connector.readOnly.Load() || connector.begun.Load() != 1 || connector.queried.Load() != 1 || connector.executed.Load() != 0 {
				t.Fatal("reconcile did not remain a single read-only scan")
			}
			if !queryFails && connector.committed.Load() != 1 {
				t.Fatal("read-only scan was not committed")
			}
			if queryFails && connector.rolledBack.Load() != 1 {
				t.Fatal("failed read-only scan was not rolled back")
			}
			if connector.closed.Load() != 1 {
				t.Fatal("reconcile retained its owned pool")
			}
			assertCommandPoolClosed(t, pool, connector)
		})
	}
}

func TestMigrationCommandsReusePoolAndCloseOnEveryExit(t *testing.T) {
	for _, name := range []string{"status", "up", "down", "present_ledger", "unknown_version_failure", "apply_failure", "rollback_failure", "status_failure", "close_failure"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HAI_COMMAND_TIMEOUT", "1s")
			connector := &commandTestConnector{}
			if name == "present_ledger" || name == "unknown_version_failure" {
				connector.ledgerPresent = true
				if name == "unknown_version_failure" {
					connector.versions = []string{"pre/9999_unknown"}
				}
			}
			if name == "status_failure" {
				connector.queryFailure = errors.New("synthetic status failure")
			}
			if name == "close_failure" {
				connector.closeFailure = errors.New("synthetic close failure")
			}
			db, pool := commandTestDatabase(t, connector)
			opens, applies, rollbacks := 0, 0, 0
			commands := migrationCommands{
				open: func(ctx context.Context) (*gorm.DB, error) {
					opens++
					return db, pool.PingContext(ctx)
				},
				apply: func(bound *gorm.DB) error {
					applies++
					boundPool, err := bound.DB()
					if err != nil || boundPool != pool {
						t.Fatal("apply used another pool")
					}
					if name == "apply_failure" {
						return errors.New("synthetic apply failure")
					}
					return nil
				},
				rollback: func(bound *gorm.DB, dir, version string) error {
					rollbacks++
					boundPool, err := bound.DB()
					if err != nil || boundPool != pool || dir != "pre" || version != "pre/0001_version" {
						t.Fatal("rollback received wrong pool or target")
					}
					if name == "rollback_failure" {
						return errors.New("synthetic rollback failure")
					}
					return nil
				},
			}
			args := []string{"status"}
			if name == "up" || name == "apply_failure" || name == "status_failure" {
				args = []string{"up"}
			}
			if name == "down" || name == "rollback_failure" {
				args = []string{"down", "pre/0001_version"}
			}
			request, err := parseMigrationRequest(args)
			if err != nil {
				t.Fatal(err)
			}
			code := executeMigration(request, commands)
			wantFailure := strings.HasSuffix(name, "failure")
			if (code != 0) != wantFailure || opens != 1 || connector.opened.Load() != 1 || connector.closed.Load() != 1 {
				t.Fatalf("command lifecycle: code=%d opens=%d connections=%d closes=%d", code, opens, connector.opened.Load(), connector.closed.Load())
			}
			assertCommandPoolClosed(t, pool, connector)
			if (applies == 1) != (args[0] == "up") || (rollbacks == 1) != (args[0] == "down") {
				t.Fatal("incorrect mutation callback selection")
			}
			if (name == "up" || name == "status" || name == "close_failure") && connector.queried.Load() != 2 {
				t.Fatal("status did not read both phases using the same pool")
			}
			if name == "present_ledger" && connector.queried.Load() != 6 {
				t.Fatal("present ledger did not read applied versions for both phases")
			}
			if connector.executed.Load() != 0 {
				t.Fatal("status issued unexpected mutation SQL")
			}
		})
	}
}

func TestCommandTimeoutValidationAndCancellation(t *testing.T) {
	for _, raw := range []string{"0s", "-1m", "secret-invalid-duration", "1"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("HAI_COMMAND_TIMEOUT", raw)
			if code := withCommandDatabase("test", func(context.Context) (*gorm.DB, error) {
				t.Fatal("invalid timeout opened a database")
				return nil, nil
			}, func(*gorm.DB) int { t.Fatal("invalid timeout executed a command"); return 0 }); code == 0 {
				t.Fatal("invalid timeout accepted")
			}
		})
	}
	t.Run("query_deadline", func(t *testing.T) {
		t.Setenv("HAI_COMMAND_TIMEOUT", "1s")
		connector := &commandTestConnector{waitForDeadline: true}
		db, pool := commandTestDatabase(t, connector)
		request, _ := parseMigrationRequest([]string{"status"})
		if code := executeMigration(request, migrationCommands{open: func(context.Context) (*gorm.DB, error) { return db, nil }}); code == 0 {
			t.Fatal("cancelled query returned success")
		}
		if connector.queried.Load() != 1 || connector.closed.Load() != 1 {
			t.Fatal("deadline did not terminate query and close pool")
		}
		assertCommandPoolClosed(t, pool, connector)
	})
	t.Run("opener_gets_deadline_and_context_is_cancelled", func(t *testing.T) {
		t.Setenv("HAI_COMMAND_TIMEOUT", "")
		db, _ := commandTestDatabase(t, &commandTestConnector{})
		var openingContext context.Context
		if code := withCommandDatabase("test", func(ctx context.Context) (*gorm.DB, error) {
			openingContext = ctx
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 29*time.Minute || time.Until(deadline) > 30*time.Minute {
				t.Fatal("default command deadline is not 30 minutes")
			}
			return db, nil
		}, func(bound *gorm.DB) int {
			if bound.Statement.Context != openingContext {
				t.Fatal("command replaced the acquisition context")
			}
			return 0
		}); code != 0 || openingContext.Err() == nil {
			t.Fatal("successful command retained its live context")
		}
	})
}

func TestCommandOpeningFailureDoesNotRunOperation(t *testing.T) {
	t.Setenv("HAI_COMMAND_TIMEOUT", "1s")
	for _, malformed := range []bool{false, true} {
		called := false
		code := withCommandDatabase("test", func(context.Context) (*gorm.DB, error) {
			called = true
			if malformed {
				return &gorm.DB{}, nil
			}
			return nil, errors.New("synthetic opening failure")
		}, func(*gorm.DB) int { t.Fatal("failed open ran operation"); return 0 })
		if code == 0 || !called {
			t.Fatal("failed open returned success")
		}
	}
}
