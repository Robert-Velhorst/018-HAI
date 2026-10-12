package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type executionTestPool struct {
	opened             atomic.Int64
	begun              atomic.Int64
	queries            atomic.Int64
	prepared           atomic.Int64
	mu                 sync.Mutex
	commits, rollbacks []int64
	connectHook        func(context.Context, int64) error
}
type executionTestConnector struct{ state *executionTestPool }
type executionTestDriver struct{}

func (executionTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector only")
}
func (c executionTestConnector) Driver() driver.Driver { return executionTestDriver{} }
func (c executionTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := c.state.opened.Add(1)
	if c.state.connectHook != nil {
		if err := c.state.connectHook(ctx, id); err != nil {
			return nil, err
		}
	}
	return &executionTestConn{id: id, state: c.state}, nil
}

type executionTestConn struct {
	id    int64
	state *executionTestPool
}

func (*executionTestConn) Close() error { return nil }
func (c *executionTestConn) Prepare(string) (driver.Stmt, error) {
	c.state.prepared.Add(1)
	return &executionTestStmt{connection: c}, nil
}
func (c *executionTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *executionTestConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.begun.Add(1)
	return &executionTestTx{conn: c}, nil
}
func (c *executionTestConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries.Add(1)
	return driver.RowsAffected(1), nil
}
func (c *executionTestConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries.Add(1)
	return &executionTestRows{id: c.id}, nil
}

type executionTestStmt struct{ connection *executionTestConn }

func (*executionTestStmt) Close() error  { return nil }
func (*executionTestStmt) NumInput() int { return -1 }
func (s *executionTestStmt) Exec([]driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), nil)
}
func (s *executionTestStmt) Query([]driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), nil)
}
func (s *executionTestStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.connection.ExecContext(ctx, "", args)
}
func (s *executionTestStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.connection.QueryContext(ctx, "", args)
}

type executionTestTx struct{ conn *executionTestConn }

func (tx *executionTestTx) Commit() error {
	tx.conn.state.mu.Lock()
	defer tx.conn.state.mu.Unlock()
	tx.conn.state.commits = append(tx.conn.state.commits, tx.conn.id)
	return nil
}
func (tx *executionTestTx) Rollback() error {
	tx.conn.state.mu.Lock()
	defer tx.conn.state.mu.Unlock()
	tx.conn.state.rollbacks = append(tx.conn.state.rollbacks, tx.conn.id)
	return nil
}

type executionTestRows struct {
	id   int64
	read bool
}

func (*executionTestRows) Columns() []string { return []string{"connection_id"} }
func (*executionTestRows) Close() error      { return nil }
func (rows *executionTestRows) Next(values []driver.Value) error {
	if rows.read {
		return io.EOF
	}
	rows.read = true
	values[0] = rows.id
	return nil
}

func executionPoolFixture(t *testing.T, capacity int) (*gorm.DB, *sql.DB, *executionTestPool) {
	t.Helper()
	state := &executionTestPool{}
	pool := sql.OpenDB(executionTestConnector{state: state})
	pool.SetMaxOpenConns(capacity)
	pool.SetMaxIdleConns(capacity)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool, state
}

func readExecutionConnection(db *gorm.DB) (int64, error) {
	var id int64
	err := db.Raw("SELECT connection_id").Row().Scan(&id)
	return id, err
}

func TestReservedAuthorizationCommitsIndependentlyAtMinimumPoolSize(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	failure := errors.New("synthetic effect failure")
	var lockID, authID int64
	err := WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
		if pool.Stats().InUse != 2 {
			t.Fatal("both connections were not reserved before transaction entry")
		}
		return lockDB.Transaction(func(lockTx *gorm.DB) error {
			var err error
			lockID, err = readExecutionConnection(lockTx)
			if err != nil {
				return err
			}
			if _, finish, err := PostgresExecutionDB(scope, lockTx); err == nil {
				finish()
				t.Fatal("lock transaction was accepted as authorization")
			}
			auth, finish, err := PostgresExecutionDB(scope, db)
			if err != nil {
				return err
			}
			defer finish()
			returnErr := auth.Transaction(func(authTx *gorm.DB) error {
				bound, finish, err := PostgresExecutionDB(scope, authTx)
				if err != nil {
					return err
				}
				defer finish()
				authID, err = readExecutionConnection(bound)
				if err != nil {
					return err
				}
				if got, err := bound.DB(); err == nil || got != nil {
					t.Fatal("wrapped transaction exposed its unrestricted pool")
				}
				if !PostgresExecutionPoolMatches(db, bound) {
					t.Fatal("opaque comparison lost original pool identity")
				}
				return bound.Exec("synthetic authorization write").Error
			})
			if returnErr != nil {
				return returnErr
			}
			return failure
		})
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if lockID == 0 || authID == 0 || authID == lockID {
		t.Fatal("authorization shared the lock connection")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.commits) != 1 || state.commits[0] != authID || len(state.rollbacks) != 1 || state.rollbacks[0] != lockID {
		t.Fatalf("independent commit/rollback lost: %+v / %+v", state.commits, state.rollbacks)
	}
	if pool.Stats().InUse != 0 {
		t.Fatal("reservation leaked connections")
	}
}

func TestReservationAcquiresBothBeforeLocksAndCleansPartialAdmission(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	other, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	called := false
	err = WithPostgresExecutionConnections(ctx, db, func(*gorm.DB, context.Context) error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || called || state.begun.Load() != 0 || pool.Stats().InUse != 1 {
		t.Fatalf("partial admission entered locks or leaked: %v / %t / %+v", err, called, pool.Stats())
	}
	executionAdmissions.Lock()
	defer executionAdmissions.Unlock()
	if _, exists := executionAdmissions.pools[pool]; exists {
		t.Fatal("cancelled admission leaked a pool gate")
	}
}

func TestConcurrentReservationsMakeProgressWithoutSplitPairDeadlock(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	partial := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var workers sync.WaitGroup
	defer func() {
		cancel()
		once.Do(func() { close(release) })
		workers.Wait()
	}()
	state.connectHook = func(ctx context.Context, id int64) error {
		if id != 2 {
			return nil
		}
		close(partial)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	results := make(chan error, 2)
	workers.Add(1)
	go func() {
		defer workers.Done()
		results <- WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
			return lockDB.Transaction(func(*gorm.DB) error {
				auth, finish, err := PostgresExecutionDB(scope, db)
				if err != nil {
					return err
				}
				defer finish()
				_, err = readExecutionConnection(auth)
				return err
			})
		})
	}()
	select {
	case <-partial:
	case <-ctx.Done():
		t.Fatal("first reservation did not reach partial acquisition")
	}
	started := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		close(started)
		results <- WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
			return lockDB.Transaction(func(*gorm.DB) error {
				auth, finish, err := PostgresExecutionDB(scope, db)
				if err != nil {
					return err
				}
				defer finish()
				_, err = readExecutionConnection(auth)
				return err
			})
		})
	}()
	<-started
	for {
		executionAdmissions.Lock()
		gate := executionAdmissions.pools[pool]
		queued := gate != nil && gate.users == 2
		executionAdmissions.Unlock()
		if queued {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("competing reservation bypassed the pair acquisition gate")
		case <-time.After(time.Millisecond):
		}
	}
	if pool.Stats().WaitCount != 0 || state.begun.Load() != 0 {
		t.Fatal("competing worker acquired pool capacity or locks before pair admission")
	}
	once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("reservation progress timed out")
		}
	}
	if pool.Stats().InUse != 0 {
		t.Fatal("completed reservations retained connections")
	}
}

func TestDeadlineTighteningCancelsAlreadyBoundDetachedHandle(t *testing.T) {
	db, _, _ := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		bound, finish, err := PostgresExecutionDB(context.WithoutCancel(scope), db)
		if err != nil {
			return err
		}
		defer finish()
		if err := TightenPostgresExecutionDeadline(scope, time.Now().Add(20*time.Millisecond)); err != nil {
			return err
		}
		select {
		case <-bound.Statement.Context.Done():
		case <-time.After(300 * time.Millisecond):
			t.Fatal("previously issued handle outlived tightened authority")
		}
		if _, err := readExecutionConnection(bound); !errors.Is(err, context.Canceled) {
			t.Fatalf("expired handle executed: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReservedScopeRejectsWrongPoolNestedAndRetainedContexts(t *testing.T) {
	db, _, _ := executionPoolFixture(t, 2)
	wrong, _, wrongState := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var retained context.Context
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		retained = context.WithoutCancel(scope)
		auth, finish, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		defer finish()
		if _, cleanup, err := PostgresExecutionDB(context.Background(), auth); !errors.Is(err, ErrExecutionConnectionScope) {
			cleanup()
			t.Fatal("private handle accepted without its authority context")
		}
		if _, finish, err := PostgresExecutionDB(scope, wrong); !errors.Is(err, ErrExecutionConnectionScope) {
			finish()
			t.Fatal("wrong pool accepted")
		}
		if err := WithPostgresExecutionConnections(scope, db, func(*gorm.DB, context.Context) error { t.Fatal("nested callback entered"); return nil }); !errors.Is(err, ErrExecutionConnectionScope) {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if wrongState.opened.Load() != 0 {
		t.Fatal("mismatched pool was touched")
	}
	if _, finish, err := PostgresExecutionDB(retained, db); !errors.Is(err, ErrExecutionConnectionScope) {
		finish()
		t.Fatal("retained context revived capacity")
	}
}

func TestReservationPrivateDeadlineCannotBeExtendedByDetachment(t *testing.T) {
	db, _, _ := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		deadline := time.Now().Add(20 * time.Millisecond)
		if err := TightenPostgresExecutionDeadline(scope, deadline); err != nil {
			return err
		}
		if err := TightenPostgresExecutionDeadline(scope, time.Now().Add(time.Hour)); err != nil {
			return err
		}
		bound, finish, err := PostgresExecutionDB(context.WithoutCancel(scope), db)
		if err != nil {
			return err
		}
		defer finish()
		if got, ok := bound.Statement.Context.Deadline(); !ok || !got.Equal(deadline) {
			t.Fatal("detached or tightened private deadline was extended")
		}
		<-bound.Statement.Context.Done()
		if _, finish, err := PostgresExecutionDB(context.WithoutCancel(scope), db); !errors.Is(err, ErrExecutionConnectionScope) {
			finish()
			t.Fatal("expired reservation accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReservationRejectsUnboundedAndSingleConnectionAdmission(t *testing.T) {
	db, _, state := executionPoolFixture(t, 1)
	callback := func(*gorm.DB, context.Context) error { t.Fatal("unsupported admission entered"); return nil }
	if err := WithPostgresExecutionConnections(context.Background(), db, callback); !errors.Is(err, ErrExecutionConnectionScope) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := WithPostgresExecutionConnections(ctx, db, callback); !errors.Is(err, ErrExecutionConnectionCapacity) {
		t.Fatal(err)
	}
	if state.opened.Load() != 0 {
		t.Fatal("unsupported admission opened a connection")
	}
}

func TestReservedSQLRejectsReplacedAndDetachedContextsBeforeDriverEntry(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var retained *gorm.DB
	err := WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
		auth, finish, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		defer finish()
		retained = auth
		reattached, stop := context.WithTimeout(context.WithoutCancel(auth.Statement.Context), time.Second)
		defer stop()
		for _, handle := range []*gorm.DB{lockDB, auth} {
			if _, err := readExecutionConnection(handle); err != nil {
				return err
			}
			before := state.queries.Load()
			for _, replacement := range []context.Context{
				context.Background(), context.WithoutCancel(handle.Statement.Context), reattached,
			} {
				changed := handle.WithContext(replacement)
				if err := changed.Exec("synthetic write").Error; !errors.Is(err, ErrExecutionConnectionScope) {
					t.Fatalf("replaced context executed: %v", err)
				}
				rows, err := changed.Raw("SELECT connection_id").Rows()
				if rows != nil {
					rows.Close()
				}
				if !errors.Is(err, ErrExecutionConnectionScope) {
					t.Fatalf("replaced context queried: %v", err)
				}
				if _, err := readExecutionConnection(changed); err == nil {
					t.Fatal("replaced context returned a row")
				}
				if err := changed.Begin().Error; !errors.Is(err, ErrExecutionConnectionScope) {
					t.Fatalf("replaced context began a transaction: %v", err)
				}
			}
			if state.queries.Load() != before || state.begun.Load() != 0 {
				t.Fatal("rejection reached the driver")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readExecutionConnection(retained.WithContext(context.Background())); err == nil {
		t.Fatal("retained handle executed outside reservation")
	}
	if raw, err := retained.DB(); err == nil || raw != nil || pool.Stats().InUse != 0 {
		t.Fatal("retained handle exposed pool or retained capacity")
	}
}

func TestReservedPreparedSessionsCannotBorrowOrdinaryStatementCache(t *testing.T) {
	db, _, state := executionPoolFixture(t, 2)
	if err := db.Session(&gorm.Session{PrepareStmt: true}).Exec("synthetic write").Error; err != nil {
		t.Fatal(err)
	}
	if state.prepared.Load() != 1 || state.queries.Load() != 1 {
		t.Fatal("ordinary statement cache was not populated")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
		auth, finish, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		defer finish()
		for _, handle := range []*gorm.DB{lockDB, auth} {
			if err := handle.Session(&gorm.Session{PrepareStmt: true}).Exec("synthetic write").Error; !errors.Is(err, ErrExecutionConnectionScope) {
				t.Fatalf("prepared session borrowed ordinary authority: %v", err)
			}
			if _, err := readExecutionConnection(handle.Session(&gorm.Session{PrepareStmt: true})); err == nil {
				t.Fatal("prepared row returned ordinary authority")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.prepared.Load() != 1 || state.queries.Load() != 1 {
		t.Fatal("reserved prepared session issued SQL")
	}
}

func TestReservedCollaboratorsReuseAuthorizationTransaction(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(lockDB *gorm.DB, scope context.Context) error {
		return lockDB.Transaction(func(lockTx *gorm.DB) error {
			if _, err := PostgresExecutionTransactionContext(scope, lockTx); !errors.Is(err, ErrExecutionConnectionScope) {
				t.Fatal("effect-lock transaction was offered to collaborators")
			}
			auth, finish, err := PostgresExecutionDB(scope, db)
			if err != nil {
				return err
			}
			defer finish()
			return auth.Transaction(func(authTx *gorm.DB) error {
				transactionContext, err := PostgresExecutionTransactionContext(auth.Statement.Context, authTx)
				if err != nil {
					return err
				}
				collaborator, cleanup, err := PostgresExecutionDB(transactionContext, db)
				if err != nil {
					return err
				}
				defer cleanup()
				if collaborator.Statement.ConnPool != authTx.Statement.ConnPool {
					t.Fatal("collaborator lost the active authorization transaction")
				}
				if _, err := readExecutionConnection(collaborator); err != nil {
					return err
				}
				return collaborator.Exec("synthetic collaborator write").Error
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.begun.Load() != 2 || len(state.commits) != 2 || state.commits[0] == state.commits[1] || pool.Stats().InUse != 0 {
		t.Fatal("collaborator created a third transaction or lost separate commits")
	}
}

func TestReservedSamePoolDoesNotProveAnotherReservationsAuthority(t *testing.T) {
	db, pool, _ := executionPoolFixture(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, first context.Context) error {
		return WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, second context.Context) error {
			auth, finish, err := PostgresExecutionDB(second, db)
			if err != nil {
				return err
			}
			defer finish()
			return auth.Transaction(func(tx *gorm.DB) error {
				if !PostgresExecutionPoolMatches(db, tx) {
					t.Fatal("same-pool comparison lost its identity-only contract")
				}
				if _, cleanup, err := PostgresExecutionDB(first, tx); !errors.Is(err, ErrExecutionConnectionScope) {
					cleanup()
					t.Fatal("another reservation reused the transaction")
				}
				if _, err := PostgresExecutionTransactionContext(first, tx); !errors.Is(err, ErrExecutionConnectionScope) {
					t.Fatal("another reservation established transaction affinity")
				}
				return nil
			})
		})
	})
	if err != nil || pool.Stats().InUse != 0 {
		t.Fatalf("reservation comparison failed or retained capacity: %v", err)
	}
}

func TestReservedUnclosedRowsAreCancelledAtCallbackExit(t *testing.T) {
	db, pool, _ := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var rows *sql.Rows
	var finish func()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		auth, cleanup, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		finish = cleanup
		rows, err = auth.Raw("SELECT connection_id").Rows()
		return err
	})
	if finish != nil {
		finish()
	}
	if rows != nil {
		defer rows.Close()
	}
	if err != nil || rows == nil || rows.Next() || !errors.Is(rows.Err(), context.Canceled) || pool.Stats().InUse != 0 {
		t.Fatalf("retained rows escaped cancellation or cleanup: %v", err)
	}
}

func TestOpaqueComparisonPreservesOrdinaryTransactionsAndRejectsTypedNil(t *testing.T) {
	db, _, _ := executionPoolFixture(t, 2)
	other, _, _ := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !PostgresExecutionPoolMatches(db, tx) || PostgresExecutionPoolMatches(other, tx) {
			t.Fatal("ordinary transaction identity comparison changed")
		}
		invalid := tx.WithContext(ctx)
		invalid.Statement.ConnPool = (*sql.Tx)(nil)
		if PostgresExecutionPoolMatches(db, invalid) {
			t.Fatal("typed nil transaction passed pool comparison")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReservedExpiredCommitRollsBackAndReturnsCapacity(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		auth, finish, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		defer finish()
		return auth.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("synthetic write").Error; err != nil {
				return err
			}
			if err := TightenPostgresExecutionDeadline(scope, time.Now().Add(20*time.Millisecond)); err != nil {
				return err
			}
			<-scope.Done()
			return nil
		})
	})
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrExecutionConnectionScope) {
		t.Fatalf("expired transaction committed or lost refusal: %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.commits) != 0 || len(state.rollbacks) != 1 || pool.Stats().InUse != 0 {
		t.Fatal("expired transaction failed to roll back or release capacity")
	}
}

func TestReservedUnfinishedTransactionIsCancelledAtCallbackExit(t *testing.T) {
	db, pool, state := executionPoolFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var finish func()
	err := WithPostgresExecutionConnections(ctx, db, func(_ *gorm.DB, scope context.Context) error {
		auth, cleanup, err := PostgresExecutionDB(scope, db)
		if err != nil {
			return err
		}
		finish = cleanup
		// Deliberately omit transaction completion inside the callback.
		return auth.Begin().Error
	})
	if finish != nil {
		finish()
	}
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.commits) != 0 || len(state.rollbacks) != 1 || pool.Stats().InUse != 0 {
		t.Fatal("unfinished transaction escaped reservation cleanup")
	}
}
