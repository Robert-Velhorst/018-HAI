package durablejob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

type failingReapRepo struct {
	*fakeRepo
	err error
}

type failingRecurringCompletionRepo struct {
	*fakeRepo
}

type queueScopedFakeRepo struct {
	*fakeRepo
	globalReapCalls int
}

type cancelAfterClaimRepo struct {
	*fakeRepo
	cancel context.CancelFunc
}

type claimLimitRecordingRepo struct {
	*fakeRepo
	limits []int
}

type retryDirectiveTestError struct {
	retryable  bool
	retryAfter time.Duration
}

func (e retryDirectiveTestError) Error() string             { return "provider sync failed safely" }
func (e retryDirectiveTestError) Retryable() bool           { return e.retryable }
func (e retryDirectiveTestError) RetryAfter() time.Duration { return e.retryAfter }

func (r *failingReapRepo) ReapExpiredLeases(time.Time, time.Duration) (int, error) {
	return 0, r.err
}

func (r *failingReapRepo) ReapExpiredLeasesForQueue(string, time.Time, time.Duration) (int, error) {
	return 0, r.err
}

func (r *failingRecurringCompletionRepo) CompleteRecurring(
	_ uuid.UUID,
	_ string,
	_ int64,
	_ time.Time,
	_ string,
	_ int,
	_ string,
	_ *models.DurableJob,
) (bool, bool, error) {
	return false, false, errors.New("simulated terminal transaction failure")
}

func (r *queueScopedFakeRepo) ReapExpiredLeases(time.Time, time.Duration) (int, error) {
	r.globalReapCalls++
	return 0, errors.New("runner used unscoped lease recovery")
}

func (r *queueScopedFakeRepo) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	return reapFakeExpiredLeases(r.fakeRepo, queue, now, lease)
}

func (r *cancelAfterClaimRepo) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	jobs, err := r.fakeRepo.ClaimDue(workerID, queue, now, limit)
	r.cancel()
	return jobs, err
}

func (r *claimLimitRecordingRepo) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	r.limits = append(r.limits, limit)
	return r.fakeRepo.ClaimDue(workerID, queue, now, limit)
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

func TestRunnerStartProcessesAlreadyDueWorkWithoutWaitingForPollInterval(t *testing.T) {
	repo := newFakeRepo()
	now := time.Now().UTC()
	if _, err := repo.Enqueue(&models.DurableJob{
		Queue: "startup", Kind: "due", RunAt: now, MaxAttempts: 1,
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(repo, Options{WorkerID: "startup-worker", Queue: "startup"})
	processed := make(chan struct{}, 1)
	runner.Register("due", func(context.Context, Job) error {
		processed <- struct{}{}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Start(ctx, time.Hour)

	select {
	case <-processed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("due work waited for the polling interval instead of running at worker startup")
	}
}

func TestRunnerStatusRecordsPollFailureAndRecovery(t *testing.T) {
	repository := &failingReapRepo{fakeRepo: newFakeRepo(), err: errors.New("database unavailable")}
	runner := NewRunner(repository, Options{WorkerID: "status-worker", Queue: "status"})
	runner.markStarted(time.Now().UTC())
	runner.runAndReport(context.Background())
	failed := runner.Status()
	if !failed.Running || failed.LastError == "" || failed.ConsecutiveErrors != 1 || failed.LastPollAt.IsZero() {
		t.Fatalf("failed worker status = %#v", failed)
	}

	repository.err = nil
	runner.runAndReport(context.Background())
	recovered := runner.Status()
	if recovered.LastError != "" || recovered.ConsecutiveErrors != 0 || recovered.LastSuccessfulAt.IsZero() {
		t.Fatalf("recovered worker status = %#v", recovered)
	}
	runner.markStopped()
	if runner.Status().Running {
		t.Fatal("runner status remained running after stop")
	}
}

func TestRunnerStatusRedactsCredentialsFromPollFailures(t *testing.T) {
	runner := NewRunner(newFakeRepo(), Options{WorkerID: "status-worker", Queue: "status"})
	runner.recordPollResult(time.Now().UTC(), errors.New("provider unavailable: token=must-not-leak"))
	if got := runner.Status().LastError; strings.Contains(got, "must-not-leak") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("runner status retained or lost redaction for credential-bearing error: %q", got)
	}
}

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

func (f *fakeRepo) EnqueueIfNoActiveMatchingPayload(job *models.DurableJob) (bool, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	for _, existing := range f.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind && existing.Payload == job.Payload &&
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

func (f *fakeRepo) MarkSucceededWithAttempts(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobSucceeded
	job.Attempts = attempts
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

func (f *fakeRepo) MarkDeferred(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, reason string) (bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobPending
	job.RunAt = runAt
	job.LastError = reason
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

func (f *fakeRepo) CompleteRecurring(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, terminalStatus string, attempts int, lastErr string, next *models.DurableJob) (bool, bool, error) {
	job := f.jobs[id]
	if !fakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, false, nil
	}
	if terminalStatus != models.DurableJobSucceeded && terminalStatus != models.DurableJobDead {
		return false, false, errors.New("invalid recurring terminal status")
	}
	job.Status = terminalStatus
	job.CompletedAt = &now
	job.LockedBy = ""
	job.LockedAt = nil
	job.LastError = lastErr
	job.Attempts = attempts
	for _, existing := range f.jobs {
		if existing.ID != id && existing.Queue == next.Queue && existing.Kind == next.Kind && (existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return true, false, nil
		}
	}
	_, err := f.Enqueue(next)
	return true, err == nil, err
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
	return reapFakeExpiredLeases(f, "", now, lease)
}

func (f *fakeRepo) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	return reapFakeExpiredLeases(f, queue, now, lease)
}

func reapFakeExpiredLeases(repo *fakeRepo, queue string, now time.Time, lease time.Duration) (int, error) {
	cutoff := now.Add(-lease)
	reaped := 0
	for _, job := range repo.jobs {
		if job.Queue == queue || queue == "" {
			if job.Status != models.DurableJobRunning || (job.LockedAt != nil && !job.LockedAt.Before(cutoff)) {
				continue
			}
			job.Attempts++
			job.LastError = "worker lease is missing or expired; execution outcome is unknown"
			if job.Attempts >= job.MaxAttempts {
				job.Status = models.DurableJobDead
				completedAt := now
				job.CompletedAt = &completedAt
				job.LastError = "worker lease is missing or expired on the final allowed attempt; execution outcome is unknown"
			} else {
				job.Status = models.DurableJobPending
			}
			job.LockedBy = ""
			job.LockedAt = nil
			reaped++
		}
	}
	return reaped, nil
}

func TestRunnerReapsRunningJobWithMissingLeaseTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		attempts     int
		maxAttempts  int
		wantStatus   string
		wantAttempts int
	}{
		{name: "retry remains", attempts: 0, maxAttempts: 3, wantStatus: models.DurableJobPending, wantAttempts: 1},
		{name: "final attempt dead-letters", attempts: 2, maxAttempts: 3, wantStatus: models.DurableJobDead, wantAttempts: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepo()
			job, err := repo.Enqueue(&models.DurableJob{
				Queue: "default", Kind: "malformed", RunAt: now, Status: models.DurableJobRunning,
				Attempts: test.attempts, MaxAttempts: test.maxAttempts, LockedBy: "orphaned-worker",
			})
			if err != nil {
				t.Fatalf("Enqueue malformed row: %v", err)
			}

			reaped, err := repo.ReapExpiredLeases(now, time.Minute)
			if err != nil || reaped != 1 {
				t.Fatalf("ReapExpiredLeases = %d, %v; want one recovered row", reaped, err)
			}
			stored, err := repo.Find(job.ID)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if stored.Status != test.wantStatus || stored.Attempts != test.wantAttempts || stored.LockedBy != "" || stored.LockedAt != nil {
				t.Fatalf("recovered malformed job = %#v; want %s/%d with cleared lease", stored, test.wantStatus, test.wantAttempts)
			}
			if test.wantStatus == models.DurableJobDead && stored.CompletedAt == nil {
				t.Fatal("dead-lettered malformed job has no terminal timestamp")
			}
			if !strings.Contains(stored.LastError, "lease is missing") || !strings.Contains(stored.LastError, "outcome is unknown") {
				t.Fatalf("recovery reason = %q; want missing-lease unknown-outcome explanation", stored.LastError)
			}
		})
	}
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

func (f *fakeRepo) FindLatestDeadByPayload(queue, kind, payload string) (*models.DurableJob, error) {
	var latest *models.DurableJob
	for _, job := range f.jobs {
		if job.Queue != queue || job.Kind != kind || job.Payload != payload || job.Status != models.DurableJobDead || job.CompletedAt == nil {
			continue
		}
		if latest == nil || job.CompletedAt.After(*latest.CompletedAt) {
			latest = job
		}
	}
	return latest, nil
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
	if stored.Attempts != 1 {
		t.Fatalf("attempts = %d after successful first run, want 1", stored.Attempts)
	}
}

func TestEnsureScheduledForPayloadDeduplicatesOnlyMatchingWork(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "w1", Queue: "source", Now: fixedClock(&now)})

	created, err := runner.EnsureScheduledForPayload("source.sync", `{"sourceId":"a"}`, now, 5)
	if err != nil || !created {
		t.Fatalf("first payload schedule = %v, %v; want true, nil", created, err)
	}
	created, err = runner.EnsureScheduledForPayload("source.sync", `{"sourceId":"a"}`, now, 5)
	if err != nil || created {
		t.Fatalf("duplicate payload schedule = %v, %v; want false, nil", created, err)
	}
	created, err = runner.EnsureScheduledForPayload("source.sync", `{"sourceId":"b"}`, now, 5)
	if err != nil || !created {
		t.Fatalf("distinct payload schedule = %v, %v; want true, nil", created, err)
	}

	if got := len(repo.jobs); got != 2 {
		t.Fatalf("active jobs = %d, want 2 distinct source syncs", got)
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
	if stored.Attempts != 2 {
		t.Fatalf("attempts after failure then success = %d, want 2", stored.Attempts)
	}
}

func TestRunnerPreservesFailureWhenZeroDelayRetryIsReclaimedInSamePass(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{
		WorkerID: "zero-delay-worker", Policy: backoff.Policy{Base: 0, Factor: 1}, Now: fixedClock(&now), Batch: 2,
	})
	const failure = "source timed out after the request was sent"
	calls := 0
	runner.Register("sync", func(context.Context, Job) error {
		calls++
		return errors.New(failure)
	})
	job, err := runner.Enqueue("sync", "{}", now, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one processed delivery", processed, err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if calls != 1 || stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("same-pass retry state = calls:%d job:%#v; want one invocation and pending/1", calls, stored)
	}
	if stored.LastError != failure {
		t.Fatalf("same-pass retry replaced the failure evidence: got %q, want %q", stored.LastError, failure)
	}
}

func TestRunnerKeepsStableJobIDForIdempotentSideEffectRetry(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{
		WorkerID: "idempotent-worker", Policy: backoff.Policy{Base: time.Minute, Factor: 1}, Now: fixedClock(&now),
	})
	var effectKey uuid.UUID
	effectCount := 0
	var invocationKeys []uuid.UUID
	runner.Register("side-effect", func(_ context.Context, job Job) error {
		invocationKeys = append(invocationKeys, job.ID)
		if effectKey == uuid.Nil {
			effectKey = job.ID
			effectCount++
			return errors.New("side effect applied but acknowledgement was lost")
		}
		if job.ID != effectKey {
			return errors.New("retry used a different idempotency key")
		}
		return nil
	})
	job, err := runner.Enqueue("side-effect", "{}", now, 2)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("first RunOnce = %d, %v; want one attempt", processed, err)
	}
	now = now.Add(time.Minute)
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("retry RunOnce = %d, %v; want one retry", processed, err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(invocationKeys) != 2 || invocationKeys[0] != job.ID || invocationKeys[1] != job.ID {
		t.Fatalf("handler idempotency keys = %v; want the stable job ID %s on both deliveries", invocationKeys, job.ID)
	}
	if effectCount != 1 {
		t.Fatalf("simulated external effect count = %d; idempotency key should prevent duplicate effect", effectCount)
	}
	if stored.Status != models.DurableJobSucceeded || stored.Attempts != 2 {
		t.Fatalf("retried job = status %q attempts %d; want succeeded/2", stored.Status, stored.Attempts)
	}
}

func TestRunnerHonorsProviderRetryDirectiveAndMinimumDelay(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantStatus   string
		wantAttempts int
		wantRunAt    time.Time
	}{
		{
			name:       "provider minimum delay wins over backoff",
			err:        retryDirectiveTestError{retryable: true, retryAfter: 2 * time.Minute},
			wantStatus: models.DurableJobPending, wantAttempts: 1,
		},
		{
			name:       "permanent provider error stops retries immediately",
			err:        retryDirectiveTestError{retryable: false},
			wantStatus: models.DurableJobDead, wantAttempts: 1,
		},
		{
			name:       "unbounded provider delay stops automatic retries",
			err:        retryDirectiveTestError{retryable: true, retryAfter: MaxRetryAfter + time.Second},
			wantStatus: models.DurableJobDead, wantAttempts: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
			repo := newFakeRepo()
			runner := NewRunner(repo, Options{
				WorkerID: "provider-worker", Queue: "source", Now: fixedClock(&now),
				Policy: backoff.Policy{Base: time.Minute, Factor: 1, Max: time.Minute},
			})
			runner.Register("sync", func(context.Context, Job) error { return test.err })
			job, err := runner.Enqueue("sync", "{}", now, 5)
			if err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
				t.Fatalf("RunOnce = %d, %v; want one processed job", processed, err)
			}
			stored, err := repo.Find(job.ID)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if stored.Status != test.wantStatus || stored.Attempts != test.wantAttempts {
				t.Fatalf("status/attempts = %q/%d, want %q/%d", stored.Status, stored.Attempts, test.wantStatus, test.wantAttempts)
			}
			if test.wantStatus == models.DurableJobPending {
				wantRunAt := now.Add(2 * time.Minute)
				if !stored.RunAt.Equal(wantRunAt) {
					t.Fatalf("retry RunAt = %s, want provider minimum %s", stored.RunAt, wantRunAt)
				}
			}
		})
	}
}

func TestRunnerKeepsRecurringScheduleAfterPermanentFailure(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "recurring-worker", Queue: "source", Now: fixedClock(&now)})
	calls := 0
	if err := runner.RegisterRecurring("sync-scan", time.Hour, 5, func(context.Context) error {
		calls++
		return retryDirectiveTestError{retryable: false}
	}); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one processed job", processed, err)
	}
	if calls != 1 {
		t.Fatalf("recurring handler calls = %d, want one (permanent failure must not retry)", calls)
	}
	dead, pending := 0, 0
	for _, job := range repo.jobs {
		if job.Kind != "sync-scan" {
			continue
		}
		if job.Status == models.DurableJobDead {
			dead++
		}
		if job.Status == models.DurableJobPending {
			pending++
			if !job.RunAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("next recurring RunAt = %s, want %s", job.RunAt, now.Add(time.Hour))
			}
		}
	}
	if dead != 1 || pending != 1 {
		t.Fatalf("recurring dead/pending counts = %d/%d, want 1/1", dead, pending)
	}
}

func TestRunnerPersistsAttemptsForSuccessfulRecurringOccurrence(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "recurring-worker", Queue: "source", Now: fixedClock(&now)})
	if err := runner.RegisterRecurring("sync-scan", time.Hour, 5, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one processed occurrence", processed, err)
	}

	var completed, pending *models.DurableJob
	for _, job := range repo.jobs {
		if job.Kind != "sync-scan" {
			continue
		}
		switch job.Status {
		case models.DurableJobSucceeded:
			completed = job
		case models.DurableJobPending:
			pending = job
		}
	}
	if completed == nil || completed.Attempts != 1 {
		t.Fatalf("completed occurrence = %#v, want succeeded with one recorded attempt", completed)
	}
	if pending == nil || pending.Attempts != 0 {
		t.Fatalf("next occurrence = %#v, want fresh pending occurrence with zero attempts", pending)
	}
}

func TestRecurringDeferredWorkDoesNotDeadLetterAtAttemptLimit(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "recurring-worker", Queue: "source", Now: fixedClock(&now)})
	if err := runner.RegisterRecurring("sync-scan", time.Hour, 1, func(context.Context) error {
		return Defer("provider is temporarily paused")
	}); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}

	var occurrence *models.DurableJob
	for _, job := range repo.jobs {
		if job.Kind == "sync-scan" {
			occurrence = job
			break
		}
	}
	if occurrence == nil {
		t.Fatal("recurring occurrence was not enqueued")
	}

	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one deferred occurrence", processed, err)
	}
	if occurrence.Status != models.DurableJobPending || occurrence.Attempts != 0 {
		t.Fatalf("deferred occurrence status/attempts = %q/%d, want pending/0", occurrence.Status, occurrence.Attempts)
	}
	if !occurrence.RunAt.Equal(now.Add(DefaultDeferDelay)) {
		t.Fatalf("deferred occurrence RunAt = %s, want %s", occurrence.RunAt, now.Add(DefaultDeferDelay))
	}
	for _, job := range repo.jobs {
		if job.ID != occurrence.ID && job.Kind == "sync-scan" && (job.Status == models.DurableJobPending || job.Status == models.DurableJobRunning) {
			t.Fatalf("deferred occurrence unexpectedly created replacement job %s", job.ID)
		}
	}
}

func TestRunnerDefersPausedWorkWithoutConsumingRetryBudget(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "w1", Now: fixedClock(&now)})
	runner.Register("paused", func(context.Context, models.DurableJob) error {
		return Defer("background processing is paused by safety policy")
	})
	job, err := runner.Enqueue("paused", "{}", now, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want 1, nil", processed, err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if stored.Status != models.DurableJobPending || stored.Attempts != 0 {
		t.Fatalf("deferred state = %q attempts=%d, want pending/0", stored.Status, stored.Attempts)
	}
	if !stored.RunAt.Equal(now.Add(DefaultDeferDelay)) {
		t.Fatalf("deferred RunAt = %v, want %v", stored.RunAt, now.Add(DefaultDeferDelay))
	}
	if stored.LastError != "job deferred: background processing is paused by safety policy" {
		t.Fatalf("deferred reason = %q", stored.LastError)
	}
}

func TestRunnerCancellationOnFinalAttemptRetainsLeaseForRecovery(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	const lease = 30 * time.Second
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{
		WorkerID: "stopping-worker", Lease: lease, Now: fixedClock(&now),
	})
	started := make(chan struct{})
	handlerReturned := make(chan struct{})
	runner.Register("shutdown", func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		close(handlerReturned)
		return ctx.Err()
	})
	job, err := runner.Enqueue("shutdown", "{}", now, 2)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		_, runErr := runner.RunOnce(ctx)
		runDone <- runErr
	}()
	<-started
	cancel()
	<-handlerReturned
	if runErr := <-runDone; !errors.Is(runErr, context.Canceled) {
		t.Fatalf("RunOnce error = %v, want context.Canceled", runErr)
	}

	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find after shutdown: %v", err)
	}
	if stored.Status != models.DurableJobRunning || stored.LockedBy != "stopping-worker" {
		t.Fatalf("job after shutdown = status %q locked_by %q, want running lease retained by stopping worker", stored.Status, stored.LockedBy)
	}
	if stored.Attempts != 0 || stored.CompletedAt != nil {
		t.Fatalf("job after shutdown consumed/closed attempt: attempts=%d completed_at=%v, want 0 and open", stored.Attempts, stored.CompletedAt)
	}

	// A later worker recovers the expired lease and can complete the same work;
	// shutdown did not consume the final permitted attempt or dead-letter it.
	now = now.Add(lease + time.Second)
	survivor := NewRunner(repo, Options{WorkerID: "survivor", Lease: lease, Now: fixedClock(&now)})
	survivor.Register("shutdown", func(context.Context, Job) error { return nil })
	if processed, runErr := survivor.RunOnce(context.Background()); runErr != nil || processed != 1 {
		t.Fatalf("survivor RunOnce = %d, %v; want one recovered job", processed, runErr)
	}
	stored, err = repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find after recovery: %v", err)
	}
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("recovered job status = %q, want succeeded", stored.Status)
	}
}

func TestRunnerHonorsCancellationWhenLeaseHeartbeatIntervalRoundsToZero(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "tiny-lease-worker", Lease: time.Nanosecond, Now: fixedClock(&now)})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	runner.Register("slow", func(context.Context, Job) error {
		close(started)
		<-release // Simulates a dependency that cannot stop immediately on cancellation.
		return nil
	})
	job, err := runner.Enqueue("slow", "{}", now, 2)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, runErr := runner.RunOnce(ctx)
		done <- runErr
	}()
	<-started
	cancel()

	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("RunOnce error = %v; want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		releaseOnce.Do(func() { close(release) })
		<-done
		t.Fatal("RunOnce did not observe cancellation while the handler was blocked")
	}
	releaseOnce.Do(func() { close(release) })

	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find after cancellation: %v", err)
	}
	if stored.Status != models.DurableJobRunning || stored.LockedBy != "tiny-lease-worker" || stored.Attempts != 0 {
		t.Fatalf("in-flight job after cancellation = %#v; want owned lease retained and attempts unchanged", stored)
	}
}

func TestRunnerDoesNotInvokeClaimedJobWithCanceledContext(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "stopping-worker", Now: fixedClock(&now)})
	handlerCalled := make(chan struct{}, 1)
	runner.Register("shutdown", func(context.Context, Job) error {
		handlerCalled <- struct{}{}
		return errors.New("must not run")
	})
	job, err := runner.Enqueue("shutdown", "{}", now, 1)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if processed, runErr := runner.RunOnce(ctx); processed != 0 || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("RunOnce = %d, %v; want no claim and context.Canceled", processed, runErr)
	}
	select {
	case <-handlerCalled:
		t.Fatal("handler ran after worker context was already canceled")
	default:
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if stored.Status != models.DurableJobPending || stored.Attempts != 0 || stored.CompletedAt != nil {
		t.Fatalf("job state after canceled startup = %#v, want unchanged pending job with no consumed attempt", stored)
	}
}

func TestRunnerReleasesClaimedButUnstartedJobOnCancellation(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	fake := newFakeRepo()
	ctx, cancel := context.WithCancel(context.Background())
	repo := &cancelAfterClaimRepo{fakeRepo: fake, cancel: cancel}
	runner := NewRunner(repo, Options{WorkerID: "stopping-worker", Queue: "source", Now: fixedClock(&now)})
	handlerCalled := false
	runner.Register("sync", func(context.Context, Job) error {
		handlerCalled = true
		return nil
	})
	job, err := runner.Enqueue("sync", "{}", now, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	secondJob, err := runner.Enqueue("sync", "{\"second\":true}", now, 3)
	if err != nil {
		t.Fatalf("Enqueue second: %v", err)
	}

	if processed, runErr := runner.RunOnce(ctx); processed != 1 || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("RunOnce = %d, %v; want one released claim and context.Canceled", processed, runErr)
	}
	if handlerCalled {
		t.Fatal("handler ran after cancellation between claim and invocation")
	}
	for _, id := range []uuid.UUID{job.ID, secondJob.ID} {
		stored, err := fake.Find(id)
		if err != nil {
			t.Fatalf("Find %s: %v", id, err)
		}
		if stored.Status != models.DurableJobPending || stored.Attempts != 0 || stored.LockedBy != "" || stored.LockedAt != nil {
			t.Fatalf("unstarted canceled job = %#v, want pending, unlocked, and attempt-neutral", stored)
		}
		if !stored.RunAt.Equal(now) {
			t.Fatalf("unstarted canceled job RunAt = %s, want immediately due at %s", stored.RunAt, now)
		}
	}
}

func TestRunnerUsesQueueScopedLeaseRecovery(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	fake := &queueScopedFakeRepo{fakeRepo: newFakeRepo()}
	shortQueueJob, err := fake.Enqueue(&models.DurableJob{Queue: "short", Kind: "short-job", RunAt: now, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue short queue job: %v", err)
	}
	longQueueJob, err := fake.Enqueue(&models.DurableJob{Queue: "long", Kind: "long-job", RunAt: now, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue long queue job: %v", err)
	}
	shortClaim, err := fake.ClaimDue("short-worker", "short", now, 1)
	if err != nil || len(shortClaim) != 1 {
		t.Fatalf("claim short queue job: jobs=%d err=%v", len(shortClaim), err)
	}
	longClaim, err := fake.ClaimDue("long-worker", "long", now, 1)
	if err != nil || len(longClaim) != 1 {
		t.Fatalf("claim long queue job: jobs=%d err=%v", len(longClaim), err)
	}

	now = now.Add(10 * time.Second)
	shortRunner := NewRunner(fake, Options{WorkerID: "short-worker", Queue: "short", Lease: time.Second, Now: fixedClock(&now)})
	shortRunner.Register("short-job", func(context.Context, Job) error { return nil })
	if processed, err := shortRunner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("short queue RunOnce = %d, %v; want reclaimed short job", processed, err)
	}
	if fake.globalReapCalls != 0 {
		t.Fatalf("runner used global reaper %d times, want queue-scoped reaper", fake.globalReapCalls)
	}
	shortStored, _ := fake.Find(shortQueueJob.ID)
	longStored, _ := fake.Find(longQueueJob.ID)
	if shortStored.Status != models.DurableJobSucceeded {
		t.Fatalf("short queue status = %q, want succeeded", shortStored.Status)
	}
	if longStored.Status != models.DurableJobRunning || longStored.LockedBy != "long-worker" {
		t.Fatalf("long queue job was reaped by another queue: %#v", longStored)
	}
}

type globalOnlyReaperRepo struct {
	Repository
	globalReapCalls int
}

func (r *globalOnlyReaperRepo) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	r.globalReapCalls++
	return r.Repository.ReapExpiredLeases(now, lease)
}

func TestRunnerFailsClosedWithoutQueueScopedLeaseRecovery(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	fake := newFakeRepo()
	repo := &globalOnlyReaperRepo{Repository: fake}
	job, err := repo.Enqueue(&models.DurableJob{Queue: "short", Kind: "work", RunAt: now, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if claimed, err := repo.ClaimDue("live-worker", "short", now, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDue = %d, %v; want one running lease", len(claimed), err)
	}

	runner := NewRunner(repo, Options{WorkerID: "short-lease-worker", Queue: "short", Lease: time.Second, Now: fixedClock(&now)})
	runner.Register("work", func(context.Context, Job) error { return nil })
	if processed, err := runner.RunOnce(context.Background()); processed != 0 || !errors.Is(err, ErrQueueScopedLeaseRecoveryUnavailable) {
		t.Fatalf("RunOnce = %d, %v; want fail-closed queue recovery error", processed, err)
	}
	if repo.globalReapCalls != 0 {
		t.Fatalf("global reaper called %d times; it must never receive a queue-specific lease", repo.globalReapCalls)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if stored.Status != models.DurableJobRunning || stored.LockedBy != "live-worker" {
		t.Fatalf("job changed after unsupported recovery: status=%q owner=%q", stored.Status, stored.LockedBy)
	}
}

func TestRunnerShutdownDoesNotReportCancellationAsWorkerFailure(t *testing.T) {
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "shutdown-worker"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner.Start(ctx, time.Hour)
	status := runner.Status()
	if status.Running {
		t.Fatalf("runner remained marked running after shutdown: %#v", status)
	}
	if status.LastError != "" || status.ConsecutiveErrors != 0 {
		t.Fatalf("normal shutdown recorded a poll failure: %#v", status)
	}
}

func TestRunnerDeadLettersWhenFinalAttemptFailsNormally(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	runner := NewRunner(repo, Options{WorkerID: "failing-worker", Now: fixedClock(&now)})
	runner.Register("failure", func(context.Context, Job) error {
		return errors.New("permanent execution failure")
	})
	job, err := runner.Enqueue("failure", "{}", now, 1)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if processed, runErr := runner.RunOnce(context.Background()); runErr != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one processed job", processed, runErr)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if stored.Status != models.DurableJobDead || stored.Attempts != 1 || stored.LastError != "permanent execution failure" {
		t.Fatalf("job after normal final-attempt failure = status %q attempts=%d error=%q, want dead/1/recorded failure", stored.Status, stored.Attempts, stored.LastError)
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
	if stored.Attempts != 2 {
		t.Fatalf("attempts = %d after one crashed delivery and one successful retry, want 2", stored.Attempts)
	}
}

func TestRunnerCountsExpiredFinalDeliveryAgainstRetryLimit(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	const lease = 30 * time.Second
	repo := newFakeRepo()
	job, err := repo.Enqueue(&models.DurableJob{
		Queue: "default", Kind: "crashed-final", Payload: "{}", RunAt: now,
		MaxAttempts: 1, Status: models.DurableJobPending,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if claimed, err := repo.ClaimDue("dead-worker", "default", now, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDue = %d, %v; want one claimed delivery", len(claimed), err)
	}
	claimedAt := now
	repo.jobs[job.ID].LockedAt = &claimedAt

	now = now.Add(lease + time.Second)
	runner := NewRunner(repo, Options{WorkerID: "survivor", Lease: lease, Now: fixedClock(&now)})
	handlerCalled := false
	runner.Register("crashed-final", func(context.Context, Job) error {
		handlerCalled = true
		return nil
	})
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 {
		t.Fatalf("RunOnce = %d, %v; expired final delivery should dead-letter without re-execution", processed, err)
	}
	stored, err := repo.Find(job.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if handlerCalled || stored.Status != models.DurableJobDead || stored.Attempts != 1 || stored.CompletedAt == nil {
		t.Fatalf("recovered job = %#v handlerCalled=%t; want dead/1/completed without rerunning", stored, handlerCalled)
	}
	if !strings.Contains(stored.LastError, "outcome is unknown") {
		t.Fatalf("last error = %q, want unknown-outcome recovery note", stored.LastError)
	}
}

func TestRunnerClaimsBatchOneAtATime(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	repo := &claimLimitRecordingRepo{fakeRepo: newFakeRepo()}
	runner := NewRunner(repo, Options{WorkerID: "batch-worker", Queue: "source", Batch: 2, Now: fixedClock(&now)})
	runCount := 0
	runner.Register("batch", func(context.Context, Job) error {
		runCount++
		return nil
	})
	for i := 0; i < 2; i++ {
		if _, err := runner.Enqueue("batch", fmt.Sprintf("{\"n\":%d}", i), now, 3); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 2 {
		t.Fatalf("RunOnce = %d, %v; want two jobs within batch", processed, err)
	}
	if runCount != 2 {
		t.Fatalf("handler ran %d times, want 2", runCount)
	}
	if len(repo.limits) != 2 {
		t.Fatalf("ClaimDue calls = %v, want one single-job claim per batch item", repo.limits)
	}
	for _, limit := range repo.limits {
		if limit != 1 {
			t.Fatalf("ClaimDue limits = %v, want one job per claim to avoid aging unstarted leases", repo.limits)
		}
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

func TestRegisterRecurringDoesNotScheduleFutureWorkWhenTerminalTransactionFails(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	repo := &failingRecurringCompletionRepo{fakeRepo: newFakeRepo()}
	runner := NewRunner(repo, Options{WorkerID: "w1", Now: fixedClock(&now)})
	if err := runner.RegisterRecurring("tick", time.Minute, 1, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("RegisterRecurring: %v", err)
	}

	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce succeeded despite a failed terminal recurring transaction")
	}

	pending := 0
	for _, job := range repo.jobs {
		if job.Kind == "tick" && job.Status == models.DurableJobPending {
			pending++
		}
	}
	if pending != 0 {
		t.Fatalf("pending recurring jobs = %d, want 0 when terminal transaction failed", pending)
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
