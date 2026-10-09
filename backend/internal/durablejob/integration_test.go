//go:build integration

// Real-Postgres proof for the durable worker. Runs only under
// `-tags integration` with an explicitly enabled dedicated test database.
package durablejob

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/backoff"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func integrationRepo(t *testing.T) (Repository, *gorm.DB) {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_DURABLEJOB_TEST_DATABASE_DSN", "hai_durablejob_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != "hai_durablejob_test" {
		t.Fatalf("refusing test database %q: %v", database, err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatalf("extension: %v", err)
	}
	if err := db.Migrator().DropTable(&models.DurableJob{}); err != nil {
		t.Fatalf("drop dedicated test table: %v", err)
	}
	if err := db.AutoMigrate(&models.DurableJob{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return NewGormRepository(db), db
}

func TestDurableJobSurvivesProcessRestart(t *testing.T) {
	repo, _ := integrationRepo(t)
	now := time.Now().UTC()

	first := NewRunner(repo, Options{
		WorkerID: "w1",
		Policy:   backoff.Policy{Base: time.Millisecond, Factor: 1},
		Now:      func() time.Time { return now },
	})
	first.Register("job", func(ctx context.Context, j models.DurableJob) error { return errors.New("transient") })
	job, err := first.Enqueue("job", `{"n":1}`, now, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := first.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("after failure status=%q attempts=%d, want pending/1", stored.Status, stored.Attempts)
	}

	// A completely new Runner (simulating a restarted process) must pick up the
	// persisted job and finish it — nothing lived in memory.
	later := now.Add(time.Minute)
	second := NewRunner(repo, Options{WorkerID: "w2", Now: func() time.Time { return later }})
	second.Register("job", func(ctx context.Context, j models.DurableJob) error { return nil })
	processed, err := second.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce (restarted): %v", err)
	}
	if processed != 1 {
		t.Fatalf("restarted worker processed=%d, want 1", processed)
	}
	stored, _ = repo.Find(job.ID)
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("status=%q, want succeeded after restart", stored.Status)
	}
	if stored.Attempts != 2 {
		t.Fatalf("attempts=%d after one failure and one success, want 2", stored.Attempts)
	}
}

func TestRecurringSuccessPersistsAttemptsAndFreshReplacement(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	runner := NewRunner(repo, Options{
		WorkerID: "recurring-worker", Queue: "recurring", Now: func() time.Time { return now },
	})
	if err := runner.RegisterRecurring("scan", time.Hour, 3, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one completed occurrence", processed, err)
	}

	var completed, pending models.DurableJob
	if err := db.Where("queue = ? AND kind = ? AND status = ?", "recurring", "scan", models.DurableJobSucceeded).First(&completed).Error; err != nil {
		t.Fatalf("find completed occurrence: %v", err)
	}
	if err := db.Where("queue = ? AND kind = ? AND status = ?", "recurring", "scan", models.DurableJobPending).First(&pending).Error; err != nil {
		t.Fatalf("find replacement occurrence: %v", err)
	}
	if completed.Attempts != 1 {
		t.Fatalf("completed occurrence attempts = %d, want 1", completed.Attempts)
	}
	if pending.Attempts != 0 {
		t.Fatalf("replacement occurrence attempts = %d, want 0", pending.Attempts)
	}
}

func TestConcurrentWorkersNeverDoubleClaim(t *testing.T) {
	repo, _ := integrationRepo(t)
	now := time.Now().UTC()
	const jobCount = 25

	seeder := NewRunner(repo, Options{WorkerID: "seed", Now: func() time.Time { return now }})
	for i := 0; i < jobCount; i++ {
		if _, err := seeder.Enqueue("shared", "{}", now, 3); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	executions := map[string]int{}
	makeRunner := func(id string) *Runner {
		r := NewRunner(repo, Options{WorkerID: id, Batch: 5, Now: func() time.Time { return now }})
		r.Register("shared", func(ctx context.Context, j models.DurableJob) error {
			mu.Lock()
			executions[j.ID.String()]++
			mu.Unlock()
			return nil
		})
		return r
	}

	// Two workers hammer the same queue simultaneously.
	var wg sync.WaitGroup
	for _, id := range []string{"wA", "wB"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			runner := makeRunner(id)
			for i := 0; i < 10; i++ {
				if _, err := runner.RunOnce(context.Background()); err != nil {
					t.Errorf("%s RunOnce: %v", id, err)
					return
				}
			}
		}(id)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(executions) != jobCount {
		t.Fatalf("executed %d distinct jobs, want %d", len(executions), jobCount)
	}
	for id, count := range executions {
		if count != 1 {
			t.Fatalf("job %s executed %d times; FOR UPDATE SKIP LOCKED must prevent double claiming", id, count)
		}
	}
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	lease := 30 * time.Second

	seeder := NewRunner(repo, Options{WorkerID: "seed", Now: func() time.Time { return now }})
	job, err := seeder.Enqueue("orphan", "{}", now, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Claim it, then abandon it (as if the worker crashed).
	if _, err := repo.ClaimDue("dead-worker", "default", now, 10); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	// Backdate the lease so it is expired.
	if err := db.Model(&models.DurableJob{}).Where("id = ?", job.ID).
		Update("locked_at", now.Add(-2*lease)).Error; err != nil {
		t.Fatalf("backdate lease: %v", err)
	}

	survivor := NewRunner(repo, Options{WorkerID: "w2", Lease: lease, Now: func() time.Time { return now }})
	survivor.Register("orphan", func(ctx context.Context, j models.DurableJob) error { return nil })
	processed, err := survivor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1 (orphaned job reclaimed)", processed)
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("status=%q, want succeeded after lease recovery", stored.Status)
	}
	if stored.Attempts != 2 {
		t.Fatalf("attempts=%d after crash recovery and successful execution, want 2", stored.Attempts)
	}
}

func TestMalformedRunningRowsWithoutLeaseTimestampAreRecovered(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	rows := []*models.DurableJob{
		{Queue: "default", Kind: "malformed-retry", RunAt: now, Status: models.DurableJobRunning, Attempts: 0, MaxAttempts: 3, LockedBy: "orphaned-worker"},
		{Queue: "default", Kind: "malformed-final", RunAt: now, Status: models.DurableJobRunning, Attempts: 2, MaxAttempts: 3, LockedBy: "orphaned-worker"},
	}
	for _, row := range rows {
		if _, err := repo.Enqueue(row); err != nil {
			t.Fatalf("enqueue malformed running row %q: %v", row.Kind, err)
		}
	}

	count, err := repo.ReapExpiredLeases(now, time.Minute)
	if err != nil || count != 2 {
		t.Fatalf("ReapExpiredLeases = %d, %v; want both malformed rows", count, err)
	}
	var retry, final models.DurableJob
	if err := db.Where("kind = ?", "malformed-retry").First(&retry).Error; err != nil {
		t.Fatalf("load retried row: %v", err)
	}
	if retry.Status != models.DurableJobPending || retry.Attempts != 1 || retry.LockedAt != nil || retry.LockedBy != "" ||
		!strings.Contains(retry.LastError, "lease is missing") {
		t.Fatalf("retried malformed row = %#v; want pending/1 with cleared lease and recovery reason", retry)
	}
	if err := db.Where("kind = ?", "malformed-final").First(&final).Error; err != nil {
		t.Fatalf("load terminal row: %v", err)
	}
	if final.Status != models.DurableJobDead || final.Attempts != 3 || final.CompletedAt == nil || final.LockedAt != nil || final.LockedBy != "" ||
		!strings.Contains(final.LastError, "final allowed attempt") {
		t.Fatalf("terminal malformed row = %#v; want dead/3 with completion time and cleared lease", final)
	}
}

func TestExpiredLeaseOnFinalDeliveryDeadLettersWithoutReexecution(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	lease := 30 * time.Second
	job, err := repo.Enqueue(&models.DurableJob{
		Queue: "recovery", Kind: "final-crash", Payload: "{}", Status: models.DurableJobPending,
		RunAt: now, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if claimed, err := repo.ClaimDue("dead-worker", "recovery", now, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDue = %d, %v; want one claim", len(claimed), err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", job.ID).
		Update("locked_at", now.Add(-2*lease)).Error; err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	runner := NewRunner(repo, Options{WorkerID: "survivor", Queue: "recovery", Lease: lease})
	handlerCalled := false
	runner.Register("final-crash", func(context.Context, Job) error {
		handlerCalled = true
		return nil
	})
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 {
		t.Fatalf("RunOnce = %d, %v; final crashed delivery must not run again", processed, err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if handlerCalled || stored.Status != models.DurableJobDead || stored.Attempts != 1 || stored.CompletedAt == nil {
		t.Fatalf("recovered final job = %#v handlerCalled=%t; want dead/1/completed without reexecution", stored, handlerCalled)
	}
}

func TestUnstartedBatchJobsCannotExpireWhileEarlierJobRuns(t *testing.T) {
	repo, _ := integrationRepo(t)
	now := time.Now().UTC()
	lease := 500 * time.Millisecond
	if _, err := repo.Enqueue(&models.DurableJob{
		Queue: "batch", Kind: "first", Payload: "{}", Status: models.DurableJobPending,
		RunAt: now.Add(-2 * time.Second), MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}
	secondJob, err := repo.Enqueue(&models.DurableJob{
		Queue: "batch", Kind: "second", Payload: "{}", Status: models.DurableJobPending,
		RunAt: now.Add(-time.Second), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue second job: %v", err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()
	firstDone := make(chan error, 1)
	var secondRuns int32
	first := NewRunner(repo, Options{WorkerID: "first-worker", Queue: "batch", Lease: lease, Batch: 2})
	first.Register("first", func(context.Context, Job) error {
		close(firstStarted)
		<-releaseFirst
		return nil
	})
	first.Register("second", func(context.Context, Job) error {
		atomic.AddInt32(&secondRuns, 1)
		return nil
	})
	go func() {
		_, err := first.RunOnce(context.Background())
		firstDone <- err
	}()
	<-firstStarted
	time.Sleep(2 * lease)
	storedSecond, err := repo.Find(secondJob.ID)
	if err != nil {
		t.Fatalf("find second job while first is running: %v", err)
	}
	if storedSecond.Status != models.DurableJobPending || storedSecond.LockedBy != "" || storedSecond.LockedAt != nil {
		t.Fatalf("second job while first is blocked = %#v; it must remain unclaimed until execution starts", storedSecond)
	}

	second := NewRunner(repo, Options{WorkerID: "second-worker", Queue: "batch", Lease: lease, Batch: 1})
	second.Register("first", func(context.Context, Job) error { return nil })
	second.Register("second", func(context.Context, Job) error {
		atomic.AddInt32(&secondRuns, 1)
		return nil
	})
	if processed, err := second.RunOnce(context.Background()); err != nil || processed != 1 {
		release()
		t.Fatalf("second worker RunOnce = %d, %v; want the still-pending second job", processed, err)
	}
	release()
	if err := <-firstDone; err != nil {
		t.Fatalf("first worker RunOnce: %v", err)
	}
	if got := atomic.LoadInt32(&secondRuns); got != 1 {
		t.Fatalf("second handler ran %d times; an unstarted batch lease must not be reclaimed and executed twice", got)
	}
}

func TestLeaseRecoveryDoesNotReapAnotherQueueWithLongerLease(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	shortQueueJob, err := repo.Enqueue(&models.DurableJob{
		Queue: "short", Kind: "short-lease", Payload: "{}", Status: models.DurableJobPending,
		RunAt: now, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue short queue: %v", err)
	}
	longQueueJob, err := repo.Enqueue(&models.DurableJob{
		Queue: "long", Kind: "long-lease", Payload: "{}", Status: models.DurableJobPending,
		RunAt: now, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue long queue: %v", err)
	}
	if claimed, err := repo.ClaimDue("short-worker", "short", now, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("claim short queue: jobs=%d err=%v", len(claimed), err)
	}
	if claimed, err := repo.ClaimDue("long-worker", "long", now, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("claim long queue: jobs=%d err=%v", len(claimed), err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", shortQueueJob.ID).
		Update("locked_at", now.Add(-2*time.Second)).Error; err != nil {
		t.Fatalf("expire short lease: %v", err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", longQueueJob.ID).
		Update("locked_at", now.Add(-2*time.Second)).Error; err != nil {
		t.Fatalf("age long lease: %v", err)
	}

	reaper, ok := repo.(interface {
		ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error)
	})
	if !ok {
		t.Fatal("Gorm repository does not support queue-scoped lease recovery")
	}
	reaped, err := reaper.ReapExpiredLeasesForQueue("short", now, time.Second)
	if err != nil || reaped != 1 {
		t.Fatalf("short queue reaping = %d, %v; want exactly one", reaped, err)
	}
	shortStored, err := repo.Find(shortQueueJob.ID)
	if err != nil {
		t.Fatalf("find short queue job: %v", err)
	}
	longStored, err := repo.Find(longQueueJob.ID)
	if err != nil {
		t.Fatalf("find long queue job: %v", err)
	}
	if shortStored.Status != models.DurableJobPending || shortStored.LockedBy != "" {
		t.Fatalf("short job after reaping = status %q locked_by %q, want pending/unlocked", shortStored.Status, shortStored.LockedBy)
	}
	if longStored.Status != models.DurableJobRunning || longStored.LockedBy != "long-worker" {
		t.Fatalf("long queue job was reaped by short-lease worker: status %q locked_by %q", longStored.Status, longStored.LockedBy)
	}
}

func TestQueuesAreClaimedIndependently(t *testing.T) {
	repo, _ := integrationRepo(t)
	now := time.Now().UTC()

	for _, queue := range []string{"source", "workflow"} {
		job := &models.DurableJob{
			Queue:       queue,
			Kind:        "shared-kind",
			Payload:     "{}",
			Status:      models.DurableJobPending,
			RunAt:       now,
			MaxAttempts: 3,
		}
		if _, err := repo.Enqueue(job); err != nil {
			t.Fatalf("enqueue %s job: %v", queue, err)
		}
	}

	claimed, err := repo.ClaimDue("source-worker", "source", now, 10)
	if err != nil {
		t.Fatalf("claim source queue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Queue != "source" {
		t.Fatalf("claimed %#v, want only source queue", claimed)
	}

	workflowClaimed, err := repo.ClaimDue("workflow-worker", "workflow", now, 10)
	if err != nil {
		t.Fatalf("claim workflow queue: %v", err)
	}
	if len(workflowClaimed) != 1 || workflowClaimed[0].Queue != "workflow" {
		t.Fatalf("claimed %#v, want only workflow queue", workflowClaimed)
	}
}

func TestReclaimedLeaseFencesStaleWorkerCompletion(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	lease := 30 * time.Second

	job, err := repo.Enqueue(&models.DurableJob{
		Queue:       "workflow",
		Kind:        "fenced",
		Payload:     "{}",
		Status:      models.DurableJobPending,
		RunAt:       now,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	firstLease, err := repo.ClaimDue("worker-one", "workflow", now, 1)
	if err != nil || len(firstLease) != 1 {
		t.Fatalf("first claim: jobs=%d err=%v", len(firstLease), err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", job.ID).
		Update("locked_at", now.Add(-2*lease)).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	if reaped, err := repo.ReapExpiredLeases(now, lease); err != nil || reaped != 1 {
		t.Fatalf("reap: count=%d err=%v", reaped, err)
	}

	secondLease, err := repo.ClaimDue("worker-two", "workflow", now, 1)
	if err != nil || len(secondLease) != 1 {
		t.Fatalf("second claim: jobs=%d err=%v", len(secondLease), err)
	}
	if secondLease[0].LeaseGeneration <= firstLease[0].LeaseGeneration {
		t.Fatalf(
			"lease generation did not advance: first=%d second=%d",
			firstLease[0].LeaseGeneration,
			secondLease[0].LeaseGeneration,
		)
	}

	updated, err := repo.MarkSucceeded(
		job.ID,
		"worker-one",
		firstLease[0].LeaseGeneration,
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("stale completion returned error: %v", err)
	}
	if updated {
		t.Fatal("stale worker completed a reclaimed job")
	}

	updated, err = repo.MarkSucceeded(
		job.ID,
		"worker-two",
		secondLease[0].LeaseGeneration,
		now.Add(2*time.Second),
	)
	if err != nil || !updated {
		t.Fatalf("current worker completion: updated=%t err=%v", updated, err)
	}
}

func TestSingletonSchedulingIsAtomicPerQueueAndKind(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	const contenderCount = 16

	var wg sync.WaitGroup
	results := make(chan bool, contenderCount)
	errorsCh := make(chan error, contenderCount)
	for i := 0; i < contenderCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := repo.EnqueueIfNoActive(&models.DurableJob{
				Queue:       "ambient",
				Kind:        "singleton-scan",
				Payload:     "{}",
				Status:      models.DurableJobPending,
				RunAt:       now,
				MaxAttempts: 3,
			})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- created
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		t.Fatalf("singleton scheduling: %v", err)
	}
	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created %d singleton jobs, want exactly 1", createdCount)
	}

	var storedCount int64
	if err := db.Model(&models.DurableJob{}).
		Where(
			"queue = ? AND kind = ? AND status IN ?",
			"ambient",
			"singleton-scan",
			[]string{models.DurableJobPending, models.DurableJobRunning},
		).
		Count(&storedCount).Error; err != nil {
		t.Fatalf("count singleton jobs: %v", err)
	}
	if storedCount != 1 {
		t.Fatalf("stored %d singleton jobs, want 1", storedCount)
	}
}
