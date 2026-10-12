package automation

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

type configurationContextKey struct{}

type configurationReadPool struct {
	cancel context.CancelFunc
	ctx    context.Context
	calls  int
}

func (*configurationReadPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*configurationReadPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("unexpected write")
}
func (*configurationReadPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}
func (p *configurationReadPool) QueryContext(ctx context.Context, _ string, _ ...interface{}) (*sql.Rows, error) {
	p.ctx = ctx
	p.calls++
	p.cancel()
	return nil, ctx.Err()
}

type borrowedConfigurationPool struct{ *configurationReadPool }

func (*borrowedConfigurationPool) Commit() error   { return nil }
func (*borrowedConfigurationPool) Rollback() error { return nil }

func TestConfigurationRepositoryScopesQueryWithoutMutatingRoot(t *testing.T) {
	for _, boundary := range []string{"query_cancel", "cancel_before", "dry_run", "borrowed", "nil_root"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "snapshot"))
			defer cancel()
			pool := &configurationReadPool{cancel: cancel}
			var conn gorm.ConnPool = pool
			if boundary == "borrowed" {
				conn = &borrowedConfigurationPool{pool}
			}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			rootCtx := db.Statement.Context
			repo := &GormUserRepository{DB: db}
			if boundary == "dry_run" {
				repo.DB = db.Session(&gorm.Session{DryRun: true})
			}
			if boundary == "nil_root" {
				repo.DB = nil
			}
			if boundary == "cancel_before" {
				cancel()
			}
			item, err := repo.FindByIDContext(ctx, uuid.New())
			if err == nil || item != nil {
				t.Fatal("unsafe query returned configuration")
			}
			if db.Statement.Context != rootCtx {
				t.Fatal("configuration query mutated shared root context")
			}
			if boundary == "query_cancel" {
				if pool.calls != 1 || !errors.Is(err, context.Canceled) || pool.ctx.Value(configurationContextKey{}) != "snapshot" {
					t.Fatalf("query lost cancellation: %v calls=%d", err, pool.calls)
				}
				if _, ok := pool.ctx.Deadline(); !ok {
					t.Fatal("configuration query has no deadline")
				}
			} else if pool.calls != 0 {
				t.Fatal("unsafe query entered transport")
			}
		})
	}
}
