package automation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stopFailureWritePool struct {
	*configurationReadPool
	query        string
	args         []interface{}
	writes       int
	writeContext context.Context
	cancelWrite  context.CancelFunc
}

func (p *stopFailureWritePool) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	p.writeContext = ctx
	p.query, p.args = query, args
	p.writes++
	if p.cancelWrite != nil {
		p.cancelWrite()
	}
	return driver.RowsAffected(1), nil
}

type stopSummaryContextKey struct{}

func TestStopFailureSummaryKeepsBoundedStorageContext(t *testing.T) {
	for _, mode := range []string{"bounded", "cancel_before", "cancel_during", "nil_context"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), stopSummaryContextKey{}, "owner-snapshot"))
			defer cancel()
			pool := &stopFailureWritePool{configurationReadPool: &configurationReadPool{cancel: func() {}}}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			db = db.WithContext(ctx)
			if mode == "cancel_before" {
				cancel()
			}
			if mode == "cancel_during" {
				pool.cancelWrite = cancel
			}
			if mode == "nil_context" {
				db.Statement.Context = nil
			}
			rootContext := db.Statement.Context
			err = (&GormUserRepository{DB: db}).UpdateRuntimeStopFailure(uuid.New(), time.Now().UTC(), "summary")
			if db.Statement.Context != rootContext {
				t.Fatal("summary changed shared repository context")
			}
			if mode == "bounded" {
				if err != nil || pool.writes != 1 || pool.writeContext.Value(stopSummaryContextKey{}) != "owner-snapshot" {
					t.Fatal("summary lost context values")
				}
				deadline, bounded := pool.writeContext.Deadline()
				if !bounded || time.Until(deadline) > approvalRegistrationTimeout {
					t.Fatal("summary write has no bounded storage deadline")
				}
			} else {
				if err == nil {
					t.Fatal("cancelled or missing context acknowledged summary storage")
				}
				if mode != "nil_context" && !errors.Is(err, context.Canceled) {
					t.Fatal("summary lost cancellation cause")
				}
				if mode != "cancel_during" && pool.writes != 0 {
					t.Fatal("invalid context reached storage")
				}
			}
		})
	}
}

func TestRuntimeStopFailureRepositoryWritesOnlyTheSummary(t *testing.T) {
	for _, mode := range []string{"valid", "nil_id", "zero_time", "nil_storage", "dry_run"} {
		t.Run(mode, func(t *testing.T) {
			pool := &stopFailureWritePool{configurationReadPool: &configurationReadPool{cancel: func() {}}}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			repo := &GormUserRepository{DB: db}
			id, at := uuid.New(), time.Now().UTC()
			switch mode {
			case "nil_id":
				id = uuid.Nil
			case "zero_time":
				at = time.Time{}
			case "nil_storage":
				repo.DB = nil
			case "dry_run":
				repo.DB = db.Session(&gorm.Session{DryRun: true})
			}
			err = repo.UpdateRuntimeStopFailure(id, at, "stop failure summary")
			if mode != "valid" {
				if err == nil || pool.writes != 0 {
					t.Fatal("invalid summary entered storage or claimed success")
				}
				return
			}
			if err != nil || pool.writes != 1 || len(pool.args) != 3 {
				t.Fatalf("summary write failed: %v SQL=%s", err, pool.query)
			}
			parts := strings.Split(pool.query, " WHERE ")
			if len(parts) != 2 || parts[0] != `UPDATE "automations" SET "last_failure_reason"=$1` || !strings.Contains(parts[1], "last_launch_at <=") {
				t.Fatal("summary SQL changed configuration columns or lost the newer-launch fence")
			}
			if pool.args[0] != "stop failure summary" || pool.args[1] != id || pool.args[2] != at {
				t.Fatal("summary lost its exact arguments")
			}
		})
	}
}
