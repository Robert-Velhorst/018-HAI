package durablejob

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/backoff"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repository so retry, lease, and scheduling logic is
// verifiable without a database.
type fakeRepo struct {
	jobs        map[uuid.UUID]*models.DurableJob
	extendCalls int
}

type leaseLosingRepo struct {
	*fakeRepo
}

func (r *leaseLosingRepo) ExtendLease(
	_ uuid.UUID,
	_ string,
	_ int64,
	_ time.Time,
) (bool, error) {
	r.extendCalls++
	return false, nil
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]*models.DurableJob{}} }

func (f *fakeRepo) Enqueue(job *models.DurableJob) (*models.DurableJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.Status == "" {
		job.Status = models.DurableJobPending
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	copyJob := *job
	f.jobs[job.ID] = &copyJob
	return &copyJob, nil
}

func (f *fakeRepo) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	for _, existing := range f.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind &&
			(existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return false, nil
		}
	}
	_, err := f.Enqueue(job)
	return err == nil, err
}

func (f *fakeRepo) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	if queue == "" {
		queue = "default"
	}
	claimed := []models.DurableJob{}
	for _, job := range f.jobs {
		if len(claimed) >= limit {
			break
		}
		if job.Queue != queue || job.Status != models.DurableJobPending || job.RunAt.After(now) {
			continue
		}
		job.Status = models.DurableJobRunning
		job.LockedBy = workerID
		job.LeaseGeneration++
		lockedAt := now
		job.LockedAt = &lockedAt
		claimed = append(claimed, *job)
	}
	return claimed, nil
}

func (f *fakeRepo) MarkSucceeded(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobSucceeded
	job.CompletedAt = &now
	job.LockedBy = ""
	job.LockedAt = nil
	return true, nil
}

func (f *fakeRepo) MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobPending
	job.RunAt = runAt
	job.Attempts = attempts
	job.LastError = lastErr
	job.LockedBy = ""
	job.LockedAt = nil
	return true, nil
}

func (f *fakeRepo) MarkDead(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int, lastErr string) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobDead
	job.Attempts = attempts
	job.LastError = lastErr
	job.CompletedAt = &now
	job.LockedBy = ""
	job.LockedAt = nil
	return true, nil
}

func (f *fakeRepo) ExtendLease(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.LockedAt = &now
	f.extendCalls++
	return true, nil
}

func fakeLeaseOwned(job *models.DurableJob, workerID string, leaseGeneration int64) bool {
	return job != nil &&
		job.Status == models.DurableJobRunning &&
		job.LockedBy == workerID &&
		job.LeaseGeneration == leaseGeneration
}

func (f *fakeRepo) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	cutoff := now.Add(-lease)
	reaped := 0
	for _, job := range f.jobs {
		if job.Status == models.DurableJobRunning && job.LockedAt != nil && job.LockedAt.Before(cutoff) {
			job.Status = models.DurableJobPending
			job.LockedBy = ""
			job.LockedAt = nil
			reaped++
		}
	}
	return reaped, nil
}

func (f *fakeRepo) CountActiveByKind(kind string) (int64, error) {
	var count int64
	for _, job := range f.jobs {
		if job.Kind == kind && (job.Status == models.DurableJobPending || job.Status == models.DurableJobRunning) {
			count++
		}
	}
	return count, nil
}

func (f *fakeRepo) Find(id uuid.UUID) (*models.DurableJob, error) {
	job, ok := f.jobs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return job, nil
}

// fixedClock returns a controllable clock for deterministic retry scheduling.
func fixedClock(t *time.Time) func() time.Time { return func() time.Time { return *t } }

func TestRunnerExecutesJobAndMarksSucceeded(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "w1", Now: fixedClock(&now)})

	ran := false
	runner.Register("demo", func(ctx context.Context, job models.DurableJob) error {
		ran = true
		return nil
	})
	job, _ := runner.Enqueue("demo", `{"x":1}`, now, 3)

	processed, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed != 1 || !ran {
		t.Fatalf("processed=%d ran=%v, want 1/true", processed, ran)
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("status = %q, want succeeded", stored.Status)
	}
}

func TestRunnerRetriesWithBackoffAndSurvivesRestart(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	policy := backoff.Policy{Base: time.Minute, Factor: 2, Max: time.Hour}
	runner := NewRunner(repo, Options{WorkerID: "w1", Policy: policy, Now: fixedClock(&now)})
	runner.Register("flaky", func(ctx context.Context, job models.DurableJob) error {
		return errors.New("boom")
	})
	job, _ := runner.Enqueue("flaky", "{}", now, 3)

	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("after failure: status=%q attempts=%d, want pending/1", stored.Status, stored.Attempts)
	}
	// Backoff must push RunAt into the future by the first delay (1m).
	if !stored.RunAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("RunAt = %v, want %v (backoff delay)", stored.RunAt, now.Add(time.Minute))
	}
	// Not yet due: a poll before RunAt must not claim it.
	if processed, _ := runner.RunOnce(context.Background()); processed != 0 {
		t.Fatalf("claimed a job before its RunAt; processed=%d", processed)
	}

	// Durability: a brand-new Runner (as if the process restarted) picks the job
	// up once it is due, because state lives in the repository, not in memory.
	now = now.Add(2 * time.Minute)
	restarted := NewRunner(repo, Options{WorkerID: "w2", Policy: policy, Now: fixedClock(&now)})
	restarted.Register("flaky", func(ctx context.Context, job models.DurableJob) error { return nil })
	if processed, _ := restarted.RunOnce(context.Background()); processed != 1 {
		t.Fatalf("restarted worker processed=%d, want 1", processed)
	}
	stored, _ = repo.Find(job.ID)
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("status after restart = %q, want succeeded", stored.Status)
	}
}

func TestRunnerDeadLettersAfterMaxAttempts(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "w1", Policy: backoff.Policy{Base: 0, Factor: 1}, Now: fixedClock(&now)})
	runner.Register("always-fails", func(ctx context.Context, job models.DurableJob) error {
		return errors.New("nope")
	})
	job, _ := runner.Enqueue("always-fails", "{}", now, 2)

	for i := 0; i < 2; i++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobDead {
		t.Fatalf("status = %q after exhausting attempts, want dead", stored.Status)
	}
	if stored.Attempts != 2 || stored.LastError == "" {
		t.Fatalf("attempts=%d lastErr=%q, want 2 and a recorded error", stored.Attempts, stored.LastError)
	}
}

func TestRunnerReclaimsJobAfterWorkerCrash(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	lease := 30 * time.Second

	// Simulate a crash: the job is claimed (running, leased) but never finished.
	job, _ := repo.Enqueue(&models.DurableJob{
		Kind: "demo", Payload: "{}", RunAt: now, MaxAttempts: 3, Status: models.DurableJobPending,
	})
	if _, err := repo.ClaimDue("dead-worker", "default", now, 10); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if stored, _ := repo.Find(job.ID); stored.Status != models.DurableJobRunning {
		t.Fatalf("precondition: job should be running/leased, got %q", stored.Status)
	}

	// After the lease expires, a healthy worker must reclaim and complete it.
	now = now.Add(lease + time.Second)
	survivor := NewRunner(repo, Options{WorkerID: "w2", Lease: lease, Now: fixedClock(&now)})
	survivor.Register("demo", func(ctx context.Context, job models.DurableJob) error { return nil })

	processed, err := survivor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1 (crashed worker's job reclaimed)", processed)
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("status = %q, want succeeded after recovery", stored.Status)
	}
}

func TestLeaseGenerationRejectsStaleWorkerCompletion(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	job, _ := repo.Enqueue(&models.DurableJob{
		Kind: "demo", Payload: "{}", RunAt: now, MaxAttempts: 3,
	})
	first, err := repo.ClaimDue("w1", "default", now, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim = %#v, %v", first, err)
	}
	now = now.Add(time.Minute)
	if reaped, err := repo.ReapExpiredLeases(now, 30*time.Second); err != nil || reaped != 1 {
		t.Fatalf("reap = %d, %v", reaped, err)
	}
	second, err := repo.ClaimDue("w2", "default", now, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("second claim = %#v, %v", second, err)
	}
	if second[0].LeaseGeneration <= first[0].LeaseGeneration {
		t.Fatalf("lease generation did not advance: first=%d second=%d", first[0].LeaseGeneration, second[0].LeaseGeneration)
	}

	updated, err := repo.MarkSucceeded(job.ID, "w1", first[0].LeaseGeneration, now)
	if err != nil {
		t.Fatalf("stale completion: %v", err)
	}
	if updated {
		t.Fatal("stale worker completion must be rejected")
	}
	stored, _ := repo.Find(job.ID)
	if stored.Status != models.DurableJobRunning || stored.LockedBy != "w2" {
		t.Fatalf("stale worker changed current lease: %#v", stored)
	}

	updated, err = repo.MarkSucceeded(job.ID, "w2", second[0].LeaseGeneration, now)
	if err != nil || !updated {
		t.Fatalf("current completion = %v, %v", updated, err)
	}
}

func TestRunnerOnlyClaimsItsConfiguredQueue(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	sourceRunner := NewRunner(repo, Options{WorkerID: "source-worker", Queue: "source", Now: fixedClock(&now)})
	workflowRunner := NewRunner(repo, Options{WorkerID: "workflow-worker", Queue: "workflow", Now: fixedClock(&now)})
	sourceRan := false
	workflowRan := false
	sourceRunner.Register("scan", func(context.Context, models.DurableJob) error {
		sourceRan = true
		return nil
	})
	workflowRunner.Register("sweep", func(context.Context, models.DurableJob) error {
		workflowRan = true
		return nil
	})
	if _, err := sourceRunner.Enqueue("scan", "{}", now, 2); err != nil {
		t.Fatalf("enqueue source: %v", err)
	}
	if _, err := workflowRunner.Enqueue("sweep", "{}", now, 2); err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}

	if processed, err := sourceRunner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("source run = %d, %v", processed, err)
	}
	if !sourceRan || workflowRan {
		t.Fatalf("queue isolation failed: source=%v workflow=%v", sourceRan, workflowRan)
	}
	if processed, err := workflowRunner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("workflow run = %d, %v", processed, err)
	}
	if !workflowRan {
		t.Fatal("workflow queue was not processed")
	}
}

func TestRunnerHeartbeatsLongRunningHandler(t *testing.T) {
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{
		WorkerID: "w1",
		Lease:    15 * time.Millisecond,
		Now:      func() time.Time { return time.Now().UTC() },
	})
	runner.Register("slow", func(context.Context, models.DurableJob) error {
		time.Sleep(45 * time.Millisecond)
		return nil
	})
	if _, err := runner.Enqueue("slow", "{}", time.Now().UTC(), 2); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("run = %d, %v", processed, err)
	}
	if repo.extendCalls == 0 {
		t.Fatal("long-running handler did not heartbeat its lease")
	}
}

func TestRunnerReturnsPromptlyWhenHandlerLosesLease(t *testing.T) {
	repo := &leaseLosingRepo{fakeRepo: newFakeRepo()}
	runner := NewRunner(repo, Options{
		WorkerID: "w1",
		Lease:    15 * time.Millisecond,
		Now:      func() time.Time { return time.Now().UTC() },
	})
	release := make(chan struct{})
	runner.Register("stuck", func(context.Context, models.DurableJob) error {
		<-release
		return nil
	})
	if _, err := runner.Enqueue("stuck", "{}", time.Now().UTC(), 2); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	type result struct {
		processed int
		err       error
	}
	done := make(chan result, 1)
	go func() {
		processed, err := runner.RunOnce(context.Background())
		done <- result{processed: processed, err: err}
	}()

	select {
	case run := <-done:
		close(release)
		if run.err != nil || run.processed != 1 {
			t.Fatalf("run = %d, %v", run.processed, run.err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("runner waited for a stale handler after lease ownership was lost")
	}
	if repo.extendCalls == 0 {
		t.Fatal("test did not reach lease renewal")
	}
}

func TestRegisterRecurringKeepsScheduleAliveAfterRepeatedFailures(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	// Zero backoff so the retry is immediately claimable in the next cycle.
	runner := NewRunner(repo, Options{WorkerID: "w1", Policy: backoff.Policy{Base: 0, Factor: 1}, Now: fixedClock(&now)})

	calls := 0
	if err := runner.RegisterRecurring("tick", time.Minute, 2, func(ctx context.Context) error {
		calls++
		return errors.New("always fails")
	}); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}

	// Burn through the first occurrence's attempts.
	for i := 0; i < 2; i++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	if calls != 2 {
		t.Fatalf("work invoked %d times, want 2 (attempt + retry)", calls)
	}

	dead, pending := 0, 0
	for _, job := range repo.jobs {
		if job.Kind != "tick" {
			continue
		}
		switch job.Status {
		case models.DurableJobDead:
			dead++
		case models.DurableJobPending:
			pending++
		}
	}
	// The exhausted occurrence dead-letters, but the schedule must continue:
	// rescheduling only on success would silently kill recurring work forever.
	if dead != 1 {
		t.Fatalf("dead occurrences = %d, want 1", dead)
	}
	if pending != 1 {
		t.Fatalf("pending occurrences = %d, want exactly 1 — the recurring schedule must survive failure", pending)
	}
}

func TestRunnerDeadLettersUnknownKindAndSurvivesPanic(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "w1", Now: fixedClock(&now)})

	unknown, _ := runner.Enqueue("not-registered", "{}", now, 3)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	stored, _ := repo.Find(unknown.ID)
	if stored.Status != models.DurableJobDead {
		t.Fatalf("unknown kind status = %q, want dead", stored.Status)
	}

	// A panicking handler must be contained and retried, not crash the worker.
	runner.Register("panics", func(ctx context.Context, job models.DurableJob) error {
		panic("kaboom")
	})
	panicking, _ := runner.Enqueue("panics", "{}", now, 3)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce after panic: %v", err)
	}
	stored, _ = repo.Find(panicking.ID)
	if stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("panicking job status=%q attempts=%d, want pending/1", stored.Status, stored.Attempts)
	}
	if stored.LastError == "" {
		t.Fatalf("expected the panic to be recorded as the job error")
	}
}

func TestAdaptiveIdleDelayBoundsDatabasePolling(t *testing.T) {
	base := 15 * time.Second
	tests := []struct {
		idleCycles int
		want       time.Duration
	}{
		{idleCycles: 0, want: base},
		{idleCycles: 1, want: base},
		{idleCycles: 2, want: 30 * time.Second},
		{idleCycles: 3, want: time.Minute},
		{idleCycles: 20, want: time.Minute},
	}
	for _, test := range tests {
		if got := adaptiveIdleDelay(base, test.idleCycles); got != test.want {
			t.Fatalf("idle cycles %d: delay=%s, want %s", test.idleCycles, got, test.want)
		}
	}
}

func TestRunnerStartRunsImmediatelyAndDrainsActiveBatches(t *testing.T) {
	repo := newFakeRepo()
	now := time.Now().UTC()
	runner := NewRunner(repo, Options{
		WorkerID: "w1",
		Batch:    1,
		Now:      func() time.Time { return now },
	})
	runs := make(chan struct{}, 2)
	runner.Register("ready", func(context.Context, models.DurableJob) error {
		runs <- struct{}{}
		return nil
	})
	for range 2 {
		if _, err := runner.Enqueue("ready", "{}", now, 2); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Start(ctx, time.Hour)
	}()

	for range 2 {
		select {
		case <-runs:
		case <-time.After(250 * time.Millisecond):
			cancel()
			<-done
			t.Fatal("runner did not immediately drain an active batch")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runner did not stop promptly after cancellation")
	}
}
