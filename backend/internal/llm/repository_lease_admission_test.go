package llm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMaintenanceLeaseAdmissionChecksCancellationAndStorage(t *testing.T) {
	for _, stage := range []string{"cancelled missing repository", "missing repository", "missing database", "cancelled after acquisition", "successful acquisition"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var repository *GormModelMaintenanceRepository
			var state *maintenanceLeaseDriverState
			var db *sql.DB
			switch stage {
			case "cancelled missing repository":
				cancel()
			case "missing repository":
			case "missing database":
				repository = &GormModelMaintenanceRepository{}
			default:
				state = &maintenanceLeaseDriverState{acquireResult: true, unlockResult: true}
				if stage == "cancelled after acquisition" {
					state.onAcquireRowsClose = cancel
				}
				name := fmt.Sprintf("llm-maintenance-admission-%d", maintenanceLeaseDriverSequence.Add(1))
				sql.Register(name, maintenanceLeaseDriver{state: state})
				var err error
				db, err = sql.Open(name, "")
				if err != nil {
					t.Fatal(err)
				}
				db.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = db.Close() })
				gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				repository = &GormModelMaintenanceRepository{DB: gormDB}
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("lease admission panicked: %v", recovered)
				}
			}()
			release, acquired, err := repository.AcquireModelMaintenanceLease(ctx, "ollama", "phi3:mini")
			if stage == "successful acquisition" {
				if err != nil || !acquired || release == nil {
					t.Fatalf("valid acquisition rejected: %v", err)
				}
				release()
				release()
				if state.closed.Load() != 0 {
					t.Fatal("successful unlock discarded a healthy connection")
				}
				return
			}
			if release != nil {
				release()
			}
			if err == nil || acquired {
				t.Fatalf("invalid acquisition accepted: acquired=%t err=%v", acquired, err)
			}
			if (stage == "cancelled missing repository" || stage == "cancelled after acquisition") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			if state != nil {
				if state.closed.Load() != 1 {
					t.Fatal("cancelled acquisition left its session in the pool")
				}
				next, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				_ = next.Close()
				if state.opened.Load() != 2 {
					t.Fatal("cancelled session was reused")
				}
			}
		})
	}
}
