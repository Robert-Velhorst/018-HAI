package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var ErrExecutionConnectionCapacity = errors.New("execution requires two database connections")
var ErrExecutionConnectionScope = errors.New("invalid, expired or mismatched execution database reservation")

type executionConnectionKey struct{}
type executionQueryKey struct{}
type executionTransactionKey struct{}
type executionQueryAuthority struct {
	state *executionConnections
	done  <-chan struct{}
}
type executionConnections struct {
	pool            *sql.DB
	authority       context.Context
	active          atomic.Bool
	deadline        atomic.Int64
	authorization   *executionConnection
	authorizationDB *gorm.DB
	deadlineMu      sync.Mutex
	deadlineTimer   *time.Timer
}

type executionAdmission struct {
	held  chan struct{}
	users int
}

var executionAdmissions = struct {
	sync.Mutex
	pools map[*sql.DB]*executionAdmission
}{pools: make(map[*sql.DB]*executionAdmission)}

// Serialize only pair acquisition, not effects. Without this gate, two workers
// could each reserve their first connection and wait forever for their second.
func acquireExecutionAdmission(ctx context.Context, pool *sql.DB) (func(), error) {
	executionAdmissions.Lock()
	gate := executionAdmissions.pools[pool]
	if gate == nil {
		gate = &executionAdmission{held: make(chan struct{}, 1)}
		executionAdmissions.pools[pool] = gate
	}
	gate.users++
	executionAdmissions.Unlock()
	drop := func() {
		executionAdmissions.Lock()
		defer executionAdmissions.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(executionAdmissions.pools, pool)
		}
	}
	select {
	case gate.held <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-gate.held; drop() }) }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

// WithPostgresExecutionConnections reserves both connections before the caller
// acquires row locks. The synchronous callback receives a lock handle and a
// private context for independently committed authorization work. It must not
// retain database handles, open rows, transactions or asynchronous work.
func WithPostgresExecutionConnections(ctx context.Context, db *gorm.DB, callback func(*gorm.DB, context.Context) error) (err error) {
	if ctx == nil || db == nil || db.Config == nil || db.Statement == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" || db.PrepareStmt || callback == nil {
		return ErrExecutionConnectionScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || !time.Now().Before(deadline) || ctx.Value(executionConnectionKey{}) != nil {
		return ErrExecutionConnectionScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pool, ok := db.Statement.ConnPool.(*sql.DB)
	if !ok || pool == nil || db.Error != nil {
		return ErrExecutionConnectionScope
	}
	if maximum := pool.Stats().MaxOpenConnections; maximum > 0 && maximum < 2 {
		return ErrExecutionConnectionCapacity
	}
	release, err := acquireExecutionAdmission(ctx, pool)
	if err != nil {
		return err
	}
	lock, err := pool.Conn(ctx)
	if err != nil {
		release()
		return fmt.Errorf("reserve execution lock connection: %w", err)
	}
	authorization, err := pool.Conn(ctx)
	if err != nil {
		closeErr := lock.Close()
		release()
		return errors.Join(fmt.Errorf("reserve execution authorization connection: %w", err), closeErr)
	}
	release()
	ctx, cancel := context.WithCancel(ctx)
	state := &executionConnections{pool: pool, authority: ctx}
	state.deadline.Store(deadline.UnixNano())
	state.active.Store(true)
	state.deadlineTimer = time.AfterFunc(time.Until(deadline), cancel)
	state.authorization = &executionConnection{
		executionQueries: executionQueries{pool: authorization, state: state},
		connection:       authorization, authorization: true,
	}
	defer func() {
		state.active.Store(false)
		state.deadlineMu.Lock()
		state.deadlineTimer.Stop()
		state.deadlineMu.Unlock()
		cancel()
		for _, closeErr := range []error{authorization.Close(), lock.Close()} {
			if closeErr != nil && !errors.Is(closeErr, sql.ErrConnDone) {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	reservedCtx := context.WithValue(ctx, executionConnectionKey{}, state)
	lockHandle := &executionConnection{
		executionQueries: executionQueries{pool: lock, state: state}, connection: lock,
	}
	lockDB, err := newExecutionGormDB(db, lockHandle)
	if err != nil {
		return err
	}
	state.authorizationDB, err = newExecutionGormDB(db, state.authorization)
	if err != nil {
		return err
	}
	lockDB = lockDB.WithContext(reservedCtx)
	return callback(lockDB, reservedCtx)
}

// Fresh Gorm caches prevent a later PrepareStmt session from borrowing an
// unrestricted statement previously cached on the ordinary shared database.
func newExecutionGormDB(source *gorm.DB, pool gorm.ConnPool) (*gorm.DB, error) {
	dialect, ok := source.Dialector.(*postgres.Dialector)
	if !ok || dialect.Config == nil || len(source.Plugins) != 0 {
		return nil, ErrExecutionConnectionScope
	}
	config := *dialect.Config
	config.Conn = pool
	db, err := gorm.Open(postgres.New(config), &gorm.Config{
		SkipDefaultTransaction: source.SkipDefaultTransaction,
		NamingStrategy:         source.NamingStrategy,
		FullSaveAssociations:   source.FullSaveAssociations,
		Logger:                 source.Logger, NowFunc: source.NowFunc, DryRun: source.DryRun,
		DisableAutomaticPing:                     true,
		DisableForeignKeyConstraintWhenMigrating: source.DisableForeignKeyConstraintWhenMigrating,
		IgnoreRelationshipsWhenMigrating:         source.IgnoreRelationshipsWhenMigrating,
		DisableNestedTransaction:                 source.DisableNestedTransaction,
		AllowGlobalUpdate:                        source.AllowGlobalUpdate, QueryFields: source.QueryFields,
		CreateBatchSize: source.CreateBatchSize, TranslateError: source.TranslateError,
		ClauseBuilders: maps.Clone(source.ClauseBuilders),
	})
	if err != nil {
		return nil, err
	}
	err = db.Callback().Row().Before("gorm:row").Register("hai:reserved_prepared_row", func(queryDB *gorm.DB) {
		if queryDB.PrepareStmt {
			// Gorm's prepared Row error path returns an unscannable empty row.
			// Deny it through our safe Row path before preparation is attempted.
			queryDB.Statement.ConnPool = executionQueries{pool: pool}
		}
	})
	return db, err
}

// TightenPostgresExecutionDeadline can only shorten private authority. Call it
// after reading a locked claim's lease and before entering the effect callback.
func TightenPostgresExecutionDeadline(ctx context.Context, deadline time.Time) error {
	if ctx == nil {
		return ErrExecutionConnectionScope
	}
	state, ok := ctx.Value(executionConnectionKey{}).(*executionConnections)
	if !ok || state == nil || !state.valid() {
		return ErrExecutionConnectionScope
	}
	state.deadlineMu.Lock()
	defer state.deadlineMu.Unlock()
	if !state.valid() {
		return ErrExecutionConnectionScope
	}
	if deadline.UnixNano() < state.deadline.Load() {
		state.deadline.Store(deadline.UnixNano())
		// Cancel already-issued query contexts as well as future bindings.
		state.deadlineTimer.Reset(time.Until(deadline))
	}
	return nil
}

func (state *executionConnections) valid() bool {
	return state != nil && state.active.Load() && state.authority.Err() == nil && time.Now().UnixNano() < state.deadline.Load()
}

// PostgresExecutionDB preserves ordinary calls but routes a private managed
// callback to its reserved authorization connection. It never mutates shared
// handles, redirects a lock transaction, or falls back after scope failure.
func PostgresExecutionDB(ctx context.Context, db *gorm.DB) (*gorm.DB, func(), error) {
	noop := func() {}
	if ctx == nil || db == nil || db.Config == nil || db.Statement == nil {
		return nil, noop, ErrExecutionConnectionScope
	}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	state, reserved := ctx.Value(executionConnectionKey{}).(*executionConnections)
	if !reserved {
		if ctx.Value(executionConnectionKey{}) != nil {
			return nil, noop, ErrExecutionConnectionScope
		}
		switch db.Statement.ConnPool.(type) {
		case *executionConnection, *executionTransaction:
			return nil, noop, ErrExecutionConnectionScope
		}
		return db.WithContext(ctx), noop, nil
	}
	if !state.valid() || db.Error != nil || db.PrepareStmt {
		return nil, noop, ErrExecutionConnectionScope
	}
	active, activeSet := ctx.Value(executionTransactionKey{}).(*executionTransaction)
	if ctx.Value(executionTransactionKey{}) != nil && (!activeSet || active == nil || active.state != state || !active.authorization || executionQueryContextError(active.beginContext, state) != nil) {
		return nil, noop, ErrExecutionConnectionScope
	}
	var handle gorm.ConnPool
	switch current := db.Statement.ConnPool.(type) {
	case *sql.DB:
		if current != state.pool {
			return nil, noop, ErrExecutionConnectionScope
		}
		handle = state.authorization
	case *executionConnection:
		if current.state != state || !current.authorization {
			return nil, noop, ErrExecutionConnectionScope
		}
		handle = current
	case *executionTransaction:
		if current.state != state || !current.authorization {
			return nil, noop, ErrExecutionConnectionScope
		}
		handle = current
		if active != nil && active != current {
			return nil, noop, ErrExecutionConnectionScope
		}
	default:
		return nil, noop, ErrExecutionConnectionScope
	}
	if active != nil {
		handle = active
	}
	queryCtx, cancel := context.WithDeadline(ctx, time.Unix(0, state.deadline.Load()))
	stop := context.AfterFunc(state.authority, cancel)
	queryCtx = context.WithValue(queryCtx, executionQueryKey{}, executionQueryAuthority{state: state, done: queryCtx.Done()})
	bound := db.WithContext(queryCtx)
	config := *state.authorizationDB.Config
	bound.Config = &config
	bound.Config.ConnPool, bound.Statement.ConnPool = handle, handle
	return bound, func() { stop(); cancel() }, nil
}

// PostgresExecutionTransactionContext gives collaborators the independently
// owned authorization transaction, never the effect-lock transaction. Ordinary
// caller transactions retain their previous behavior outside managed scopes.
func PostgresExecutionTransactionContext(ctx context.Context, tx *gorm.DB) (context.Context, error) {
	if ctx == nil || tx == nil || tx.Statement == nil {
		return nil, ErrExecutionConnectionScope
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, reserved := ctx.Value(executionConnectionKey{}).(*executionConnections)
	if !reserved {
		if ctx.Value(executionConnectionKey{}) != nil {
			return nil, ErrExecutionConnectionScope
		}
		switch tx.Statement.ConnPool.(type) {
		case *executionConnection, *executionTransaction:
			return nil, ErrExecutionConnectionScope
		}
		return ctx, nil
	}
	transaction, ok := tx.Statement.ConnPool.(*executionTransaction)
	if !ok || transaction == nil || transaction.state != state || !transaction.authorization || executionQueryContextError(ctx, state) != nil {
		return nil, ErrExecutionConnectionScope
	}
	if err := executionQueryContextError(transaction.beginContext, state); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, executionTransactionKey{}, transaction), nil
}

// SQL entry points accept only issued, cancellation-linked contexts. Changing
// a Gorm handle's context must not detach it from private reservation authority.
func executionQueryContextError(ctx context.Context, state *executionConnections) error {
	if ctx == nil {
		return ErrExecutionConnectionScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !state.valid() || ctx.Value(executionConnectionKey{}) != state {
		return ErrExecutionConnectionScope
	}
	if ctx.Done() == state.authority.Done() {
		return nil
	}
	issued, ok := ctx.Value(executionQueryKey{}).(executionQueryAuthority)
	if !ok || issued.state != state || issued.done == nil || ctx.Done() != issued.done {
		return ErrExecutionConnectionScope
	}
	return nil
}

type executionQueries struct {
	pool  gorm.ConnPool
	state *executionConnections
}

func (queries executionQueries) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	// A prepared statement could bypass this guard after it leaves the scope.
	return nil, ErrExecutionConnectionScope
}

func (queries executionQueries) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if err := executionQueryContextError(ctx, queries.state); err != nil {
		return nil, err
	}
	return queries.pool.ExecContext(ctx, query, args...)
}

func (queries executionQueries) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if err := executionQueryContextError(ctx, queries.state); err != nil {
		return nil, err
	}
	return queries.pool.QueryContext(ctx, query, args...)
}

func (queries executionQueries) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if executionQueryContextError(ctx, queries.state) != nil {
		// sql.Row has no public error constructor. A pre-cancelled context
		// produces a Scan error without allowing the driver to issue SQL.
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		return queries.pool.QueryRowContext(cancelled, query, args...)
	}
	return queries.pool.QueryRowContext(ctx, query, args...)
}

// Scoped handles never expose an unrestricted pool through Gorm's DB accessor.
type executionConnection struct {
	executionQueries
	connection    *sql.Conn
	authorization bool
}

func (connection *executionConnection) GetDBConn() (*sql.DB, error) {
	return nil, ErrExecutionConnectionScope
}
func (connection *executionConnection) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	if err := executionQueryContextError(ctx, connection.state); err != nil {
		return nil, err
	}
	tx, err := connection.connection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &executionTransaction{
		executionQueries: executionQueries{pool: tx, state: connection.state},
		transaction:      tx, beginContext: ctx, authorization: connection.authorization,
	}, nil
}

type executionTransaction struct {
	executionQueries
	transaction   *sql.Tx
	beginContext  context.Context
	authorization bool
}

func (transaction *executionTransaction) GetDBConn() (*sql.DB, error) {
	return nil, ErrExecutionConnectionScope
}

// PostgresExecutionPoolMatches compares identity without handing scoped callers
// an unrestricted sql.DB. Callers must still require a real transaction and bind
// its authority before issuing SQL.
func PostgresExecutionPoolMatches(database, tx *gorm.DB) bool {
	identity := func(db *gorm.DB) (*sql.DB, error) {
		if db == nil || db.Config == nil || db.Statement == nil || db.Error != nil {
			return nil, ErrExecutionConnectionScope
		}
		value := reflect.ValueOf(db.Statement.ConnPool)
		if !value.IsValid() || ((value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && value.IsNil()) {
			return nil, ErrExecutionConnectionScope
		}
		switch pool := db.Statement.ConnPool.(type) {
		case *executionConnection:
			if pool == nil || !pool.state.valid() || !pool.authorization {
				return nil, ErrExecutionConnectionScope
			}
			return pool.state.pool, nil
		case *executionTransaction:
			if pool == nil || !pool.state.valid() || !pool.authorization {
				return nil, ErrExecutionConnectionScope
			}
			return pool.state.pool, nil
		default:
			return db.DB()
		}
	}
	original, err := identity(database)
	if err != nil || original == nil {
		return false
	}
	candidate, err := identity(tx)
	return err == nil && candidate != nil && candidate == original
}

func (transaction *executionTransaction) Commit() error {
	if err := executionQueryContextError(transaction.beginContext, transaction.state); err != nil {
		rollbackErr := transaction.transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return transaction.transaction.Commit()
}

func (transaction *executionTransaction) Rollback() error {
	// Rollback must remain available after revocation for unconditional cleanup.
	return transaction.transaction.Rollback()
}
