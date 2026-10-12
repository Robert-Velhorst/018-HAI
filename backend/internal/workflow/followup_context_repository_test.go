package workflow

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise GORM's real transaction entry without opening a database. Returning
// at BeginTx proves context transport, not server-side rollback or lock release.
type followUpContextTransactionPool struct {
	contexts []context.Context
	err      error
	cancel   context.CancelFunc
}

func (*followUpContextTransactionPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*followUpContextTransactionPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}
func (*followUpContextTransactionPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*followUpContextTransactionPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	panic("unexpected row query")
}
func (p *followUpContextTransactionPool) BeginTx(ctx context.Context, _ *sql.TxOptions) (gorm.ConnPool, error) {
	p.contexts = append(p.contexts, ctx)
	if p.cancel != nil {
		p.cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, p.err
}

func TestFollowUpGormTransactionAndReleaseReceiveScopedContext(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		pool := &followUpContextTransactionPool{err: errors.New("transaction entry refused by offline probe")}
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
			DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		scoped, err := (&GormRepository{DB: db}).withFollowUpContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if canceled {
			pool.cancel = cancel
		}
		repo := scoped.(*GormRepository)
		_, commitErr := repo.commitDueOpenLoop("alice", uuid.New(), uuid.New(), "context-worker")
		_, releaseErr := repo.releaseDueOpenLoopClaim("alice", uuid.New(), uuid.New(), "context-worker")
		wantErr := pool.err
		if canceled {
			wantErr = context.Canceled
		}
		if !errors.Is(commitErr, wantErr) || !errors.Is(releaseErr, wantErr) || len(pool.contexts) != 2 {
			t.Fatalf("transaction lost cancellation/error: commit=%v release=%v begins=%d", commitErr, releaseErr, len(pool.contexts))
		}
		for _, received := range pool.contexts {
			if received != ctx {
				t.Fatal("follow-up transaction used a context-free connection")
			}
		}
		if db.Statement.Context.Err() != nil {
			t.Fatal("scoped transaction canceled shared root")
		}
	}
}

func TestFollowUpGormContextRejectsInvalidRoot(t *testing.T) {
	for _, repo := range []*GormRepository{nil, {}, {DB: &gorm.DB{}}} {
		if _, err := repo.withFollowUpContext(context.Background()); err == nil {
			t.Fatal("invalid root database accepted")
		}
	}
	if _, err := (&GormRepository{DB: leaseExpiryDryRunDB(t)}).withFollowUpContext(nil); !errors.Is(err, ErrFollowUpContextUnavailable) {
		t.Fatal("nil context accepted")
	}
}
