package infra

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func sharedPoolFixture(t *testing.T) (*gorm.DB, *postgresPoolTestConnector) {
	t.Helper()
	c := &postgresPoolTestConnector{}
	pool := sql.OpenDB(c)
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Ping(); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, c
}

func restoreSharedPoolHooks(t *testing.T) {
	t.Helper()
	resetDefaultDBForTest()
	open, migrate := openConfiguredDB, runDefaultMigrations
	t.Cleanup(func() {
		openConfiguredDB, runDefaultMigrations = open, migrate
		resetDefaultDBForTest()
	})
}

func waitSharedPoolSealed(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		defaultDBMu.Lock()
		closed := defaultDBClosed
		defaultDBMu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("shared pool shutdown did not seal admission")
}

func TestSharedPoolCloseIsTerminalAndIdempotent(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	db, c := sharedPoolFixture(t)
	var opens, migrations int
	openConfiguredDB = func(context.Context) (*gorm.DB, error) { opens++; return db, nil }
	runDefaultMigrations = func(context.Context, *gorm.DB) error { migrations++; return nil }
	for i := 0; i < 2; i++ {
		if cached, err := GetDefaultDB(); err != nil || cached != db {
			t.Fatalf("cached pool = %v, %v", cached, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := CloseDefaultDB(); err != nil {
			t.Fatal(err)
		}
	}
	if cached, err := GetDefaultDB(); cached != nil || !errors.Is(err, ErrDefaultDBClosed) {
		t.Fatalf("reopened terminal pool: %v, %v", cached, err)
	}
	if opens != 1 || migrations != 1 || c.closed.Load() != 1 {
		t.Fatalf("opens=%d migrations=%d closes=%d", opens, migrations, c.closed.Load())
	}
}

func TestSharedPoolCloseBeforeInitializationDoesNotOpen(t *testing.T) {
	restoreSharedPoolHooks(t)
	openConfiguredDB = func(context.Context) (*gorm.DB, error) { t.Error("shutdown opened a pool"); return nil, nil }
	if err := CloseDefaultDB(); err != nil {
		t.Fatal(err)
	}
	if db, err := GetDefaultDB(); db != nil || !errors.Is(err, ErrDefaultDBClosed) {
		t.Fatalf("closed pool = %v, %v", db, err)
	}
}

func TestSharedPoolConcurrentInitializationIsSingleFlight(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	db, c := sharedPoolFixture(t)
	var opens, migrations atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls sync.WaitGroup
	defer func() { once.Do(func() { close(release) }); calls.Wait() }()
	openConfiguredDB = func(context.Context) (*gorm.DB, error) {
		opens.Add(1)
		close(entered)
		<-release
		return db, nil
	}
	runDefaultMigrations = func(context.Context, *gorm.DB) error { migrations.Add(1); return nil }
	type result struct {
		db  *gorm.DB
		err error
	}
	results := make(chan result, 8)
	for i := 0; i < cap(results); i++ {
		calls.Add(1)
		go func() { defer calls.Done(); db, err := GetDefaultDB(); results <- result{db, err} }()
	}
	<-entered
	once.Do(func() { close(release) })
	calls.Wait()
	for i := 0; i < cap(results); i++ {
		r := <-results
		if r.err != nil || r.db != db {
			t.Fatalf("concurrent result = %v, %v", r.db, r.err)
		}
	}
	if opens.Load() != 1 || migrations.Load() != 1 || c.closed.Load() != 0 {
		t.Fatal("concurrent startup duplicated initialization or closed the published pool")
	}
	if err := CloseDefaultDB(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedPoolShutdownRejectsLateCandidateAndJoinsStartup(t *testing.T) {
	for _, phase := range []string{"open", "migration"} {
		t.Run(phase, func(t *testing.T) {
			restoreSharedPoolHooks(t)
			t.Setenv("DB_MIGRATIONS_ENABLED", "true")
			db, c := sharedPoolFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var calls sync.WaitGroup
			defer func() { once.Do(func() { close(release) }); calls.Wait() }()
			failure := errors.New("synthetic migration failure")
			openConfiguredDB = func(context.Context) (*gorm.DB, error) {
				if phase == "open" {
					close(entered)
					<-release
				}
				return db, nil
			}
			runDefaultMigrations = func(context.Context, *gorm.DB) error {
				if phase == "open" {
					t.Error("migration began after startup was sealed")
					return nil
				}
				close(entered)
				<-release
				return failure
			}
			opened, closed := make(chan error, 1), make(chan error, 1)
			calls.Add(2)
			go func() {
				defer calls.Done()
				value, err := GetDefaultDB()
				if value != nil {
					t.Error("late candidate was published")
				}
				opened <- err
			}()
			<-entered
			go func() { defer calls.Done(); closed <- CloseDefaultDB() }()
			waitSharedPoolSealed(t)
			if value, err := GetDefaultDB(); value != nil || !errors.Is(err, ErrDefaultDBClosed) {
				t.Fatalf("startup reopened while cleanup pending: %v, %v", value, err)
			}
			select {
			case err := <-closed:
				t.Fatalf("shutdown returned before startup/cleanup finished: %v", err)
			default:
			}
			once.Do(func() { close(release) })
			calls.Wait()
			if err := <-opened; !errors.Is(err, ErrDefaultDBClosed) || phase == "migration" && !errors.Is(err, failure) {
				t.Fatalf("initialization outcome = %v", err)
			}
			if err := <-closed; err != nil || c.closed.Load() != 1 {
				t.Fatalf("shutdown outcome = %v, closes=%d", err, c.closed.Load())
			}
		})
	}
}

func TestSharedPoolDriverCloseOutsideMutexRetainsErrorForAllClosers(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "false")
	db, c := sharedPoolFixture(t)
	openConfiguredDB = func(context.Context) (*gorm.DB, error) { return db, nil }
	if _, err := GetDefaultDB(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls sync.WaitGroup
	defer func() { once.Do(func() { close(release) }); calls.Wait() }()
	failure := errors.New("synthetic driver close error")
	c.beforeClose = func() error { close(entered); <-release; return failure }
	results := make(chan error, 2)
	calls.Add(1)
	go func() { defer calls.Done(); results <- CloseDefaultDB() }()
	<-entered
	refused := make(chan error, 1)
	calls.Add(1)
	go func() {
		defer calls.Done()
		_, err := GetDefaultDB()
		refused <- err
	}()
	select {
	case err := <-refused:
		if !errors.Is(err, ErrDefaultDBClosed) {
			t.Fatalf("unsealed pool: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("driver close held the cache lock")
	}
	calls.Add(1)
	go func() { defer calls.Done(); results <- CloseDefaultDB() }()
	once.Do(func() { close(release) })
	calls.Wait()
	for i := 0; i < 2; i++ {
		if err := <-results; !errors.Is(err, failure) {
			t.Fatalf("close error lost: %v", err)
		}
	}
	if err := CloseDefaultDB(); !errors.Is(err, failure) || c.closed.Load() != 1 {
		t.Fatalf("repeat close = %v, driver closes=%d", err, c.closed.Load())
	}
}

func TestSharedPoolRejectsNilSuccessfulOpener(t *testing.T) {
	restoreSharedPoolHooks(t)
	t.Setenv("DB_MIGRATIONS_ENABLED", "true")
	openConfiguredDB = func(context.Context) (*gorm.DB, error) { return nil, nil }
	runDefaultMigrations = func(context.Context, *gorm.DB) error { t.Error("nil pool reached migrations"); return nil }
	if db, err := GetDefaultDB(); db != nil || err == nil {
		t.Fatal("nil successful pool accepted")
	}
	if err := CloseDefaultDB(); err != nil {
		t.Fatal(err)
	}
}
