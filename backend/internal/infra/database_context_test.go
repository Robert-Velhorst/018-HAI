package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func receiveStartup[T any](t *testing.T, results <-chan T) T {
	t.Helper()
	select {
	case value := <-results:
		return value
	case <-time.After(time.Second):
		t.Fatal("database startup did not finish the expected step")
		var zero T
		return zero
	}
}

func TestSharedPoolCallerContextValidationPrecedesAcquisition(t *testing.T) {
	restoreSharedPoolHooks(t)
	openConfiguredDB = func(context.Context) (*gorm.DB, error) { t.Error("invalid caller opened a pool"); return nil, nil }
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	for _, ctx := range []context.Context{nil, cancelled, expired} {
		if db, err := GetDefaultDBContext(ctx); db != nil || err == nil {
			t.Fatalf("invalid caller accepted: %v, %v", db, err)
		}
	}
}

func TestSharedPoolWaiterCancellationDoesNotCancelInitializer(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "false")
	t.Setenv("DB_STARTUP_TIMEOUT", "5s")
	db, c := sharedPoolFixture(t)
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var once sync.Once
	var calls sync.WaitGroup
	defer func() { once.Do(func() { close(release) }); calls.Wait() }()
	openConfiguredDB = func(ctx context.Context) (*gorm.DB, error) {
		entered <- ctx
		select {
		case <-release:
			return db, nil
		case <-ctx.Done():
			return db, ctx.Err()
		}
	}
	opened := make(chan error, 1)
	calls.Add(1)
	go func() { defer calls.Done(); _, err := GetDefaultDB(); opened <- err }()
	startupCtx := receiveStartup(t, entered)
	// Cancelling a waiter must not abandon the shared initialization it did not own.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if value, err := GetDefaultDBContext(ctx); value != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter outcome = %v, %v", value, err)
	}
	if startupCtx.Err() != nil {
		t.Fatal("waiter cancelled another caller's initialization")
	}
	once.Do(func() { close(release) })
	if err := receiveStartup(t, opened); err != nil {
		t.Fatal(err)
	}
	if value, err := GetDefaultDB(); value != db || err != nil || c.closed.Load() != 0 {
		t.Fatal("initializer did not retain its successful pool")
	}
	if err := CloseDefaultDB(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedPoolStopCancelsOpenAndMigration(t *testing.T) {
	for _, phase := range []string{"open", "migration"} {
		t.Run(phase, func(t *testing.T) {
			restoreSharedPoolHooks(t)
			t.Setenv("DB_MIGRATIONS_ENABLED", "true")
			t.Setenv("DB_STARTUP_TIMEOUT", "5s")
			db, c := sharedPoolFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var calls sync.WaitGroup
			defer func() { once.Do(func() { close(release) }); calls.Wait() }()
			hold := func(ctx context.Context) error {
				close(entered)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
					return errors.New("fixture released without cancellation")
				}
			}
			openConfiguredDB = func(ctx context.Context) (*gorm.DB, error) {
				if phase == "open" {
					return db, hold(ctx)
				}
				return db, nil
			}
			runDefaultMigrations = func(ctx context.Context, _ *gorm.DB) error {
				if phase == "open" {
					t.Error("migration ran after cancelled open")
					return nil
				}
				return hold(ctx)
			}
			opened, closed := make(chan error, 1), make(chan error, 1)
			calls.Add(1)
			go func() {
				defer calls.Done()
				value, err := GetDefaultDB()
				if value != nil {
					t.Error("cancelled candidate published")
				}
				opened <- err
			}()
			receiveStartup(t, entered)
			calls.Add(1)
			go func() { defer calls.Done(); closed <- CloseDefaultDB() }()
			if err := receiveStartup(t, opened); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrDefaultDBClosed) {
				t.Fatalf("cancelled startup outcome = %v", err)
			}
			if err := receiveStartup(t, closed); err != nil || c.closed.Load() != 1 {
				t.Fatalf("shutdown = %v, pool closes=%d", err, c.closed.Load())
			}
		})
	}
}

func TestSharedPoolDeadlineRejectsLateSuccessAndDoesNotPinRetry(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	t.Setenv("DB_STARTUP_TIMEOUT", "10ms")
	failed, failedConnector := sharedPoolFixture(t)
	accepted, acceptedConnector := sharedPoolFixture(t)
	opens := 0
	openConfiguredDB = func(context.Context) (*gorm.DB, error) {
		opens++
		if opens == 1 {
			return failed, nil
		}
		return accepted, nil
	}
	runDefaultMigrations = func(ctx context.Context, _ *gorm.DB) error {
		if opens == 1 {
			<-ctx.Done()
		}
		return nil // A callback's success cannot override the caller's expired authority.
	}
	if db, err := GetDefaultDB(); db != nil || !errors.Is(err, context.DeadlineExceeded) || failedConnector.closed.Load() != 1 {
		t.Fatalf("late success accepted: %v, %v", db, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	value, err := GetDefaultDBContext(ctx)
	cancel()
	if err != nil || value != accepted {
		t.Fatalf("retry = %v, %v", value, err)
	}
	if value.Statement.Context.Err() != nil {
		t.Fatal("successful runtime pool retained a cancelled startup context")
	}
	if cached, err := GetDefaultDB(); cached != accepted || err != nil || opens != 2 || acceptedConnector.closed.Load() != 0 {
		t.Fatal("neutral cached pool was lost after its startup caller cancelled")
	}
	if err := CloseDefaultDB(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseStartupTimeoutValidation(t *testing.T) {
	for _, value := range []string{"", " 2s ", "0s", "-1s", "2", "private-invalid-value", "999999999999999999999h"} {
		t.Run(value, func(t *testing.T) {
			restoreSharedPoolHooks(t)
			t.Setenv("DB_STARTUP_TIMEOUT", value)
			timeout, err := databaseStartupTimeout()
			switch strings.TrimSpace(value) {
			case "":
				if err != nil || timeout != 5*time.Minute {
					t.Fatalf("default timeout = %v, %v", timeout, err)
				}
			case "2s":
				if err != nil || timeout != 2*time.Second {
					t.Fatalf("override timeout = %v, %v", timeout, err)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "DB_STARTUP_TIMEOUT") || strings.Contains(err.Error(), value) {
					t.Fatalf("unsafe timeout = %v", err)
				}
				openConfiguredDB = func(context.Context) (*gorm.DB, error) {
					t.Error("invalid deadline opened a database")
					return nil, nil
				}
				if db, err := GetDefaultDB(); db != nil || err == nil {
					t.Fatal("invalid timeout accepted")
				}
			}
		})
	}
}

type startupMigrationConnector struct {
	postgresPoolTestConnector
	executionContext context.Context
}

func (c *startupMigrationConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.postgresPoolTestConnector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &startupMigrationConn{postgresPoolTestConn: conn.(*postgresPoolTestConn), connector: c}, nil
}

type startupMigrationConn struct {
	*postgresPoolTestConn
	connector *startupMigrationConnector
}

func (*startupMigrationConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return startupMigrationTx{}, nil
}
func (c *startupMigrationConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	c.connector.executionContext = ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

type startupMigrationTx struct{}

func (startupMigrationTx) Commit() error   { return nil }
func (startupMigrationTx) Rollback() error { return nil }

func TestRunMigrationsContextReachesAdvisoryLockAndKeepsBaseNeutral(t *testing.T) {
	c := &startupMigrationConnector{}
	pool := sql.OpenDB(c)
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := RunMigrationsContext(ctx, db); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("migration SQL did not honor caller deadline: %v", err)
	}
	if c.executionContext != ctx || db.Statement.Context.Err() != nil {
		t.Fatal("migration context did not reach SQL or contaminated the neutral pool")
	}
	if err := pool.PingContext(context.Background()); err != nil {
		t.Fatalf("base pool unusable: %v", err)
	}
	if err := RunMigrationsContext(nil, db); err == nil {
		t.Fatal("nil migration context accepted")
	}
	if err := RunMigrationsContext(context.Background(), nil); err == nil {
		t.Fatal("nil migration database accepted")
	}
}
