package llm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var maintenanceLeaseDriverSequence atomic.Uint64

type maintenanceLeaseDriverState struct {
	opened       atomic.Int64
	closed       atomic.Int64
	failUnlock   bool
	unlockResult bool
}

type maintenanceLeaseDriver struct{ state *maintenanceLeaseDriverState }

func (d maintenanceLeaseDriver) Open(string) (driver.Conn, error) {
	d.state.opened.Add(1)
	return maintenanceLeaseConn{state: d.state}, nil
}

type maintenanceLeaseConn struct{ state *maintenanceLeaseDriverState }

func (c maintenanceLeaseConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}

func (c maintenanceLeaseConn) Close() error {
	c.state.closed.Add(1)
	return nil
}

func (maintenanceLeaseConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

func (c maintenanceLeaseConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "pg_advisory_unlock") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if c.state.failUnlock {
		return nil, errors.New("simulated unlock failure")
	}
	return &maintenanceLeaseRows{released: c.state.unlockResult}, nil
}

type maintenanceLeaseRows struct {
	released bool
	done     bool
}

func (*maintenanceLeaseRows) Columns() []string { return []string{"pg_advisory_unlock"} }
func (*maintenanceLeaseRows) Close() error      { return nil }
func (r *maintenanceLeaseRows) Next(destination []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	destination[0] = r.released
	return nil
}

func TestReleaseModelMaintenanceLeaseDiscardsSessionWithUncertainUnlock(t *testing.T) {
	for _, test := range []struct {
		name         string
		failUnlock   bool
		unlockResult bool
		wantClosed   int64
		wantReopened int64
	}{
		{name: "successful unlock returns connection to pool", unlockResult: true, wantClosed: 0, wantReopened: 1},
		{name: "failed unlock discards connection", failUnlock: true, wantClosed: 1, wantReopened: 2},
		{name: "lock no longer owned discards connection", unlockResult: false, wantClosed: 1, wantReopened: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &maintenanceLeaseDriverState{failUnlock: test.failUnlock, unlockResult: test.unlockResult}
			name := fmt.Sprintf("llm-maintenance-lease-%d", maintenanceLeaseDriverSequence.Add(1))
			sql.Register(name, maintenanceLeaseDriver{state: state})
			db, err := sql.Open(name, "")
			if err != nil {
				t.Fatalf("open test database: %v", err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })

			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatalf("acquire test connection: %v", err)
			}
			releaseModelMaintenanceLease(conn, 17)
			if got := state.closed.Load(); got != test.wantClosed {
				t.Fatalf("closed driver connections = %d, want %d", got, test.wantClosed)
			}

			next, err := db.Conn(context.Background())
			if err != nil {
				t.Fatalf("acquire next connection: %v", err)
			}
			_ = next.Close()
			if got := state.opened.Load(); got != test.wantReopened {
				t.Fatalf("opened driver connections = %d, want %d", got, test.wantReopened)
			}
		})
	}
}

type cancellableHistoryDriver struct{ started chan string }

func (d cancellableHistoryDriver) Open(string) (driver.Conn, error) {
	return cancellableHistoryConn{started: d.started}, nil
}

type cancellableHistoryConn struct{ started chan string }

func (cancellableHistoryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}

func (cancellableHistoryConn) Close() error { return nil }

func (cancellableHistoryConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

func (c cancellableHistoryConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.started <- query
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestGormMaintenanceHistoryReadsUseCallerContext(t *testing.T) {
	for _, test := range []struct {
		name string
		read func(*GormModelMaintenanceRepository, context.Context) error
	}{
		{
			name: "latest",
			read: func(repository *GormModelMaintenanceRepository, ctx context.Context) error {
				_, err := repository.FindLatestModelMaintenanceWithContext(ctx, "ollama", "phi3:mini")
				return err
			},
		},
		{
			name: "recent",
			read: func(repository *GormModelMaintenanceRepository, ctx context.Context) error {
				_, err := repository.FindRecentModelMaintenanceWithContext(ctx, 10)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan string, 1)
			name := fmt.Sprintf("llm-maintenance-history-%d", maintenanceLeaseDriverSequence.Add(1))
			sql.Register(name, cancellableHistoryDriver{started: started})
			sqlDB, err := sql.Open(name, "")
			if err != nil {
				t.Fatalf("open test database: %v", err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open GORM test database: %v", err)
			}
			repository := &GormModelMaintenanceRepository{DB: gormDB}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- test.read(repository, ctx) }()

			select {
			case query := <-started:
				if !strings.Contains(strings.ToLower(query), "order by checked_at desc, created_at desc, id desc") {
					t.Fatalf("history query order = %q; want deterministic checked_at/created_at/id ordering", query)
				}
				cancel()
			case <-time.After(time.Second):
				t.Fatal("GORM history query did not reach the database driver")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("history query error = %v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("GORM history query did not stop after cancellation")
			}
		})
	}
}
