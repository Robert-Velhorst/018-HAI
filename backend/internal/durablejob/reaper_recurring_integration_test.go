//go:build integration

package durablejob

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This exercises PostgreSQL locks and rollback in the production repository.
// No in-memory repository is used as proof of concurrent successor creation.
func TestRecurringReaperCreatesOneSuccessorAndRollsBackAtomically(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_DURABLEJOB_TEST_DATABASE_DSN", "hai_durablejob_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != "hai_durablejob_test" {
		t.Fatalf("refusing test database %q: %v", database, err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.DurableJob{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DurableJob{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Migrator().DropTable(&models.DurableJob{}); err != nil {
			t.Error(err)
		}
	})
	repo := &gormRepository{db: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	const queue = "recurring-recovery"
	const lease = time.Minute
	const interval = time.Hour

	for _, rollback := range []bool{false, true} {
		name := "concurrent reapers and startup"
		kind := "recurring-scan"
		if rollback {
			name = "successor insertion rollback"
			kind = "rollback-scan"
		}
		t.Run(name, func(t *testing.T) {
			lockedAt := now.Add(-2 * lease)
			job, err := repo.Enqueue(&models.DurableJob{Queue: queue, Kind: kind, Payload: "{}", Status: models.DurableJobRunning, RunAt: lockedAt, Attempts: 2, MaxAttempts: 3, LockedBy: "crashed-worker", LockedAt: &lockedAt, LeaseGeneration: 7})
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(repo, Options{Queue: queue, Lease: lease, Now: func() time.Time { return now }})
			if err := runner.RegisterRecurring(kind, interval, 3, func(context.Context) error { t.Error("unknown-outcome job was replayed"); return nil }); err != nil {
				t.Fatal(err)
			}
			schedules := map[string]recurringSchedule{kind: {interval: interval, maxAttempts: 3, payload: "{}"}}
			if rollback {
				injected := errors.New("injected successor insert failure")
				const callbackName = "test:fail-recurring-successor"
				if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
					if next, ok := tx.Statement.Dest.(*models.DurableJob); ok && next.Kind == kind && next.Status == models.DurableJobPending {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				recovered, recoverErr := repo.ReapExpiredLeasesWithRecurring(queue, now, lease, schedules)
				if err := db.Callback().Create().Remove(callbackName); err != nil {
					t.Fatal(err)
				}
				if !errors.Is(recoverErr, injected) || recovered != 0 {
					t.Fatalf("failed recovery = %d, %v", recovered, recoverErr)
				}
				stored, err := repo.Find(job.ID)
				if err != nil || stored.Status != models.DurableJobRunning || stored.Attempts != 2 || stored.LockedBy != "crashed-worker" || stored.CompletedAt != nil {
					t.Fatalf("rollback changed occurrence: %#v, %v", stored, err)
				}
			}

			var workers sync.WaitGroup
			errorsCh := make(chan error, 8)
			start := make(chan struct{})
			for i := 0; i < 8; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					processed, err := runner.RunOnce(context.Background())
					if err == nil && processed != 0 {
						err = errors.New("recovery replayed a terminal occurrence or ran its successor too early")
					}
					errorsCh <- err
				}()
			}
			close(start)
			workers.Wait()
			close(errorsCh)
			for err := range errorsCh {
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 3; i++ {
				if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 {
					t.Fatalf("repeat recovery = %d, %v", processed, err)
				}
				if created, err := runner.EnsureScheduled(kind, "{}", now, 3); err != nil || created {
					t.Fatalf("startup created duplicate = %t, %v", created, err)
				}
			}
			stored, err := repo.Find(job.ID)
			if err != nil || stored.Status != models.DurableJobDead || stored.Attempts != 3 || stored.CompletedAt == nil {
				t.Fatalf("exhausted occurrence = %#v, %v", stored, err)
			}
			var pending []models.DurableJob
			if err := db.Where("queue = ? AND kind = ? AND status IN ?", queue, kind, []string{models.DurableJobPending, models.DurableJobRunning}).Find(&pending).Error; err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].Attempts != 0 || pending[0].MaxAttempts != 3 || pending[0].ID == job.ID || !pending[0].RunAt.Equal(now.Add(interval)) {
				t.Fatalf("successors = %#v; want exactly one fresh scheduled occurrence", pending)
			}
			owned, created, err := repo.CompleteRecurring(job.ID, "crashed-worker", 7, now, models.DurableJobSucceeded, 3, "", &models.DurableJob{Queue: queue, Kind: kind, RunAt: now.Add(interval), MaxAttempts: 3})
			if err != nil || owned || created {
				t.Fatalf("stale completion = owned %t created %t err %v", owned, created, err)
			}
		})
	}
}
