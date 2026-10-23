package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLaunchStateRequiresBoundedAvailableStorage(t *testing.T) {
	for _, mode := range []string{"bounded", "cancel_before", "cancel_during", "nil_context", "nil_repository", "nil_storage", "zero_storage", "dry_run"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), stopSummaryContextKey{}, "launch-owner"))
			defer cancel()
			pool := &stopFailureWritePool{configurationReadPool: &configurationReadPool{cancel: func() {}}}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			db = db.WithContext(ctx)
			repo := &GormUserRepository{DB: db}
			switch mode {
			case "cancel_before":
				cancel()
			case "cancel_during":
				pool.cancelWrite = cancel
			case "nil_context":
				db.Statement.Context = nil
			case "nil_repository":
				repo = nil
			case "nil_storage":
				repo.DB = nil
			case "zero_storage":
				repo.DB = &gorm.DB{}
			case "dry_run":
				repo.DB = db.Session(&gorm.Session{DryRun: true})
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("unavailable launch storage panicked: %v", recovered)
				}
			}()
			rootContext := db.Statement.Context
			err = repo.UpdateLaunchState(uuid.New(), time.Now().UTC(), nil)
			if db.Statement.Context != rootContext {
				t.Fatal("launch projection changed the shared context")
			}
			if mode == "bounded" {
				if err != nil || pool.writes != 1 || pool.writeContext.Value(stopSummaryContextKey{}) != "launch-owner" {
					t.Fatalf("launch projection lost storage context: %v", err)
				}
				deadline, ok := pool.writeContext.Deadline()
				if !ok || time.Until(deadline) > approvalRegistrationTimeout {
					t.Fatal("launch projection has no bounded deadline")
				}
				return
			}
			if err == nil {
				t.Fatal("unavailable or cancelled storage acknowledged launch projection")
			}
			if (mode == "cancel_before" || mode == "cancel_during") && !errors.Is(err, context.Canceled) {
				t.Fatal("launch projection lost cancellation cause")
			}
			if mode != "cancel_during" && pool.writes != 0 {
				t.Fatal("invalid launch storage reached a write")
			}
		})
	}
}
