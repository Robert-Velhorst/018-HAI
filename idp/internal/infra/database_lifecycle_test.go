package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type startupTestConnector struct {
	closed                     atomic.Int32
	closeError                 error
	pingError                  error
	waitForPingCancellation    bool
	beforePingReturn           func()
	ignorePingCancellation     bool
	connectContext             context.Context
	waitForConnectCancellation bool
}

func (c *startupTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.connectContext = ctx
	if c.waitForConnectCancellation {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &startupTestConnection{connector: c}, nil
}
func (*startupTestConnector) Driver() driver.Driver { return startupTestDriver{} }

type startupTestDriver struct{}

func (startupTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected driver.Open")
}

type startupTestConnection struct{ connector *startupTestConnector }

func (*startupTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected SQL statement")
}
func (*startupTestConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected SQL transaction")
}
func (c *startupTestConnection) Close() error {
	c.connector.closed.Add(1)
	return c.connector.closeError
}
func (c *startupTestConnection) Ping(ctx context.Context) error {
	if c.connector.waitForPingCancellation {
		<-ctx.Done()
	}
	if c.connector.beforePingReturn != nil {
		c.connector.beforePingReturn()
	}
	if c.connector.pingError != nil {
		return c.connector.pingError
	}
	if c.connector.ignorePingCancellation {
		return nil
	}
	return ctx.Err()
}

func startupTestDatabase(t *testing.T, connector *startupTestConnector) (*gorm.DB, *sql.DB) {
	t.Helper()
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool
}

func TestDefaultDatabaseStartupClosesPoolOnFailure(t *testing.T) {
	for _, stage := range []string{"migration", "seed", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("synthetic startup failure")
			closeFailure := errors.New("synthetic cleanup failure")
			connector := &startupTestConnector{}
			if stage == "cleanup" {
				connector.closeError = closeFailure
			}
			db, pool := startupTestDatabase(t, connector)
			seedCalls := 0
			result, err := prepareDefaultDatabase(db, func(*gorm.DB) error {
				if stage != "seed" {
					return failure
				}
				return nil
			}, func(*gorm.DB) error { seedCalls++; return failure })
			if result != nil || !errors.Is(err, failure) || connector.closed.Load() != 1 {
				t.Fatalf("failed startup retained a pool: result=%v err=%v closes=%d", result, err, connector.closed.Load())
			}
			if stage == "cleanup" && !errors.Is(err, closeFailure) {
				t.Fatal("cleanup error lost")
			}
			if stage != "seed" && seedCalls != 0 {
				t.Fatal("migration failure still seeded accounts")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := pool.PingContext(ctx); err == nil {
				t.Fatal("failed startup pool remains usable")
			}
		})
	}
}

func TestDefaultDatabaseStartupPreservesSuccessfulPool(t *testing.T) {
	connector := &startupTestConnector{}
	db, pool := startupTestDatabase(t, connector)
	steps := ""
	result, err := prepareDefaultDatabase(db, func(*gorm.DB) error { steps += "migration;"; return nil }, func(*gorm.DB) error { steps += "seed;"; return nil })
	if result != db || err != nil || connector.closed.Load() != 0 || steps != "migration;seed;" {
		t.Fatal("successful startup lost pool or step ordering")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDatabaseStartupRejectsMissingPool(t *testing.T) {
	for _, db := range []*gorm.DB{nil, {}, {Config: &gorm.Config{}}} {
		called := false
		result, err := prepareDefaultDatabase(db, func(*gorm.DB) error { called = true; return nil }, func(*gorm.DB) error { called = true; return nil })
		if result != nil || err == nil || called {
			t.Fatal("missing pool was accepted or invoked startup steps")
		}
	}
}

func TestDefaultDatabaseStartupClosesPoolWithoutSwallowingPanic(t *testing.T) {
	for _, stage := range []string{"migration", "seed"} {
		t.Run(stage, func(t *testing.T) {
			connector := &startupTestConnector{}
			db, _ := startupTestDatabase(t, connector)
			marker := errors.New("synthetic startup panic")
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _ = prepareDefaultDatabase(db, func(*gorm.DB) error {
					if stage == "migration" {
						panic(marker)
					}
					return nil
				}, func(*gorm.DB) error { panic(marker) })
			}()
			if recovered != marker || connector.closed.Load() != 1 {
				t.Fatal("startup panic was swallowed or its pool leaked")
			}
		})
	}
}
