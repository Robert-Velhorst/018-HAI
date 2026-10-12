package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type recoveryPGFaultKey struct{}
type recoveryPGFault struct {
	err    error
	cancel context.CancelFunc
}

// Acceptance definition only: the exact dedicated-database and explicit
// destructive-test guard must pass before any connection or migration.
func TestPostgresClaimRecoveryStateAndHistoryAtomic(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open guarded recovery acceptance database failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := infra.RunMigrations(db); err != nil {
		t.Fatal("canonical acceptance migrations failed")
	}
	if err := db.Callback().Create().After("gorm:create").Register("recovery_acceptance:after_event_write", func(tx *gorm.DB) {
		if _, event := tx.Statement.Dest.(*models.WorkflowEvent); !event {
			return
		}
		fault, ok := tx.Statement.Context.Value(recoveryPGFaultKey{}).(*recoveryPGFault)
		if !ok || fault == nil {
			return
		}
		if fault.cancel != nil {
			fault.cancel()
		}
		if fault.err != nil {
			tx.AddError(fault.err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"workflow", "open-loop"} {
		for _, scenario := range []string{"confirmed", "history_failure", "cancel_after_history_write", "live_lease", "archived_parent"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				fixture := newHistoricalReminderFixtureForOwner(t, "recovery-acceptance-"+uuid.NewString(), time.Now().UTC().Add(-3*time.Minute))
				item := fixture.source.Workflow
				item.CurrentState = StateInProgress
				item.WorkerClaimID = uuid.NewString()
				item.Archived = scenario == "archived_parent"
				lease := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
				if scenario == "live_lease" {
					lease = time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
				}
				item.WorkerLeaseUntil = &lease
				item.UpdatedAt = time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
				if err := db.Create(&item).Error; err != nil {
					t.Fatal("seed recovery workflow failed")
				}
				loop := models.WorkflowOpenLoop{ID: uuid.New(), WorkflowID: item.ID, Status: "processing", ClaimID: uuid.NewString(), LeaseUntil: &lease, UpdatedAt: item.UpdatedAt, ResponsibleParty: "external", WaitingFor: "reply", NextAction: "review follow-up"}
				if kind == "open-loop" {
					if err := db.Create(&loop).Error; err != nil {
						t.Fatal("seed recovery loop failed")
					}
				}
				parent, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				fault := &recoveryPGFault{}
				if scenario == "history_failure" {
					fault.err = errors.New("injected acknowledgement failure after real event write")
				}
				if scenario == "cancel_after_history_write" {
					fault.cancel = cancel
				}
				ctx = context.WithValue(ctx, recoveryPGFaultKey{}, fault)
				repo := &GormRepository{DB: db.WithContext(ctx)}
				var changed bool
				if kind == "workflow" {
					_, changed, err = repo.RecoverExpiredWorkflowClaimAtomic(item, time.Now().UTC().Add(24*time.Hour))
				} else {
					_, changed, err = repo.RecoverExpiredOpenLoopClaimAtomic(item.OwnerIdentity, loop, time.Now().UTC().Add(24*time.Hour))
				}
				wantHistory := int64(0)
				if scenario == "confirmed" {
					if err != nil || !changed {
						t.Fatal("recovery did not acknowledge its atomic commit")
					}
					wantHistory = 1
				} else if scenario == "live_lease" || scenario == "archived_parent" {
					if err != nil || changed {
						t.Fatal("recovered a live database lease or archived parent")
					}
				} else if err == nil || changed {
					t.Fatal("failed history was reported as successful recovery")
				}
				if scenario == "cancel_after_history_write" && !errors.Is(err, context.Canceled) {
					t.Fatal("recovery lost cancellation identity")
				}
				for _, table := range []string{"workflow_decisions", "workflow_events", "workflow_transitions"} {
					wanted := wantHistory
					if kind == "open-loop" && table == "workflow_transitions" {
						wanted = 0
					}
					var count int64
					if err := db.Table(table).Where("workflow_id = ?", item.ID).Count(&count).Error; err != nil || count != wanted {
						t.Fatalf("%s committed %d history rows, want %d", table, count, wanted)
					}
				}
				if kind == "workflow" {
					var stored models.WorkflowItem
					if err := db.First(&stored, "id = ?", item.ID).Error; err != nil {
						t.Fatal(err)
					}
					wantState := StateInProgress
					if scenario == "confirmed" {
						wantState = StateBlocked
					}
					if stored.CurrentState != wantState || stored.WorkerClaimID != item.WorkerClaimID {
						t.Fatal("workflow state/history diverged or claim fence was cleared")
					}
				} else {
					var stored models.WorkflowOpenLoop
					if err := db.First(&stored, "id = ?", loop.ID).Error; err != nil {
						t.Fatal(err)
					}
					wantStatus, wantClaim := "processing", loop.ClaimID
					if scenario == "confirmed" {
						wantStatus, wantClaim = "open", ""
					}
					if stored.Status != wantStatus || stored.ClaimID != wantClaim {
						t.Fatal("open-loop state/history diverged")
					}
				}
			})
		}
	}
}
