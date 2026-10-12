package hostruntime

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stop at actual transaction entry: no SQL server, commit or rollback proof.
type cancellationEntryPool struct {
	ctx    context.Context
	calls  int
	err    error
	cancel context.CancelFunc
}

func (*cancellationEntryPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*cancellationEntryPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}
func (*cancellationEntryPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*cancellationEntryPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	panic("unexpected row query")
}
func (p *cancellationEntryPool) BeginTx(ctx context.Context, _ *sql.TxOptions) (gorm.ConnPool, error) {
	p.ctx, p.calls = ctx, p.calls+1
	if p.cancel != nil {
		p.cancel()
	}
	return nil, p.err
}

func TestHostCancellationRepositoryContext(t *testing.T) {
	for _, mode := range []string{"bounded", "nil_context", "cancel_before", "cancel_begin", "short_deadline", "dry_run", "nil_repository", "nil_database"} {
		t.Run(mode, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("invalid cancellation panicked: %v", p)
				}
			}()
			pool := &cancellationEntryPool{err: errors.New("offline begin refused")}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			root := db.Statement.Context
			parent := context.WithValue(context.Background(), cancelScopeKey{}, "operator")
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			var request context.Context = ctx
			repo := &gormRepository{db: db}
			switch mode {
			case "nil_context":
				request = nil
			case "cancel_before":
				cancel()
			case "cancel_begin":
				pool.cancel = cancel
			case "short_deadline":
				var stop context.CancelFunc
				request, stop = context.WithTimeout(ctx, time.Second)
				defer stop()
			case "dry_run":
				repo.db = db.Session(&gorm.Session{DryRun: true})
			case "nil_repository":
				repo = nil
			case "nil_database":
				repo.db = nil
			}
			job, revoked, err := repo.CancelTask(request, "robert", "task", uuid.New(), time.Now().UTC())
			if err == nil || revoked || job != nil {
				t.Fatalf("offline entry acknowledged cancellation: job=%#v revoked=%v err=%v", job, revoked, err)
			}
			if db.Statement.Context != root {
				t.Fatal("shared database context changed")
			}
			switch mode {
			case "bounded", "cancel_begin", "short_deadline":
				if pool.calls != 1 || !errors.Is(err, pool.err) {
					t.Fatalf("lost transaction entry: calls=%d err=%v", pool.calls, err)
				}
				deadline, bounded := pool.ctx.Deadline()
				if !bounded || time.Until(deadline) > AdmissionTimeout || pool.ctx.Value(cancelScopeKey{}) != "operator" {
					t.Fatal("lost bounded context at transaction entry")
				}
				if mode == "short_deadline" {
					earlier, _ := request.Deadline()
					if deadline.After(earlier) {
						t.Fatal("parent deadline extended")
					}
				}
				if mode == "cancel_begin" && !errors.Is(err, context.Canceled) {
					t.Fatal("late cancellation cause lost")
				}
			default:
				if pool.calls != 0 {
					t.Fatal("invalid request reached transaction entry")
				}
			}
		})
	}
}
