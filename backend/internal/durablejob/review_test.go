package durablejob

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type reviewOutcomeError struct{ cause error }

func (e reviewOutcomeError) Error() string            { return "unconfirmed token=must-not-leak" }
func (e reviewOutcomeError) Unwrap() error            { return e.cause }
func (reviewOutcomeError) RequiresManualReview() bool { return true }

type reviewQueue struct {
	*fakeRepo
	held         int
	writeError   error
	acknowledged bool
}

func (q *reviewQueue) MarkForReview(ctx context.Context, id uuid.UUID, worker string, generation int64, _ time.Time, attempts int, reason string) (bool, error) {
	q.held++
	if q.writeError != nil || !q.acknowledged {
		return false, q.writeError
	}
	job := q.jobs[id]
	if ctx.Err() != nil || !fakeLeaseOwned(job, worker, generation) {
		return false, ctx.Err()
	}
	job.Status, job.Attempts, job.LastError = "needs_review", attempts, reason
	job.LockedBy, job.LockedAt, job.CompletedAt = "", nil, nil
	return true, nil
}

func (q *reviewQueue) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	for _, held := range q.jobs {
		if held.Queue == job.Queue && held.Kind == job.Kind && held.Status == "needs_review" {
			return false, nil
		}
	}
	return q.fakeRepo.EnqueueIfNoActive(job)
}

func (q *reviewQueue) EnqueueReviewIfNoActive(job *models.DurableJob) (bool, error) {
	for _, existing := range q.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind &&
			(existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning || existing.Status == models.DurableJobNeedsReview) {
			existing.ReplayPolicy = models.DurableJobReviewUnknown
		}
	}
	return q.EnqueueIfNoActive(job)
}

func (q *reviewQueue) CompleteReviewRecurring(ctx context.Context, id uuid.UUID, worker string, generation int64, now time.Time, terminal string, attempts int, reason string, next *models.DurableJob) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return q.fakeRepo.CompleteRecurring(id, worker, generation, now, terminal, attempts, reason, next)
}

// Controlled recovery fixture; SQL recovery is tested independently against
// the production GORM implementation, not inferred from this in-memory store.
func (q *reviewQueue) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	return q.ReapExpiredLeasesForQueue("", now, lease)
}

func (q *reviewQueue) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	held := 0
	for _, job := range q.jobs {
		if (queue == "" || job.Queue == queue) && job.Status == models.DurableJobRunning && job.ReplayPolicy == models.DurableJobReviewUnknown &&
			(job.LockedAt == nil || job.LockedAt.Before(now.Add(-lease))) {
			job.Status, job.LockedAt, job.LockedBy, job.CompletedAt = models.DurableJobNeedsReview, nil, "", nil
			job.Attempts++
			held++
		}
	}
	normal, err := q.fakeRepo.ReapExpiredLeasesForQueue(queue, now, lease)
	return normal + held, err
}

func TestRunnerReviewOutcomeOverridesDeferralAndDoesNotReschedule(t *testing.T) {
	for _, paused := range []bool{false, true} {
		q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
		now := time.Now().UTC()
		runner := NewRunner(q, Options{Queue: "review", Now: func() time.Time { return now }})
		calls := 0
		if err := runner.RegisterRecurring("scan", time.Minute, 3, func(context.Context) error {
			calls++
			var cause error
			if paused {
				cause = Defer("safety pause")
			}
			return reviewOutcomeError{cause: cause}
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.RunOnce(context.Background()); err == nil {
			t.Fatal("required review was reported as an ordinary successful poll")
		}
		if q.held != 1 || len(q.jobs) != 1 || calls != 1 {
			t.Fatalf("unknown outcome retried or rescheduled: held=%d jobs=%d calls=%d", q.held, len(q.jobs), calls)
		}
		for _, job := range q.jobs {
			if job.Status != "needs_review" || job.Attempts != 1 || job.CompletedAt != nil || strings.Contains(job.LastError, "must-not-leak") {
				t.Fatalf("review outcome not preserved safely: %+v", job)
			}
		}
		now = now.Add(time.Hour)
		if _, err := runner.RunOnce(context.Background()); err != nil || calls != 1 {
			t.Fatal("reviewed job was reclaimed by polling or recovery")
		}
		restarted := NewRunner(q, Options{Queue: "review", Now: func() time.Time { return now }})
		if err := restarted.RegisterRecurring("scan", time.Minute, 3, func(context.Context) error { calls++; return nil }); err != nil || len(q.jobs) != 1 {
			t.Fatal("restart created a successor around the review hold")
		}
	}
}

func TestReviewCrashPolicySurvivesFailedHoldAndRestart(t *testing.T) {
	for _, final := range []bool{false, true} {
		q := &reviewQueue{fakeRepo: newFakeRepo()}
		now := time.Now().UTC()
		calls := 0
		runner := NewRunner(q, Options{Queue: "review", Now: func() time.Time { return now }})
		if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { calls++; return reviewOutcomeError{} }); err != nil {
			t.Fatal(err)
		}
		var occurrence *models.DurableJob
		for _, job := range q.jobs {
			occurrence = job
		}
		if final {
			occurrence.Attempts = 2
		}
		if _, err := runner.RunOnce(context.Background()); !errors.Is(err, ErrReviewPersistenceUnavailable) || occurrence.Status != models.DurableJobRunning {
			t.Fatal("failed hold must remain an unconfirmed leased occurrence")
		}
		now = now.Add(time.Hour)
		restarted := NewRunner(q, Options{Queue: "review", Now: func() time.Time { return now }})
		if err := restarted.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { calls++; return nil }); err != nil {
			t.Fatal(err)
		}
		processed, err := restarted.RunOnce(context.Background())
		if err != nil || processed != 0 || calls != 1 || len(q.jobs) != 1 || occurrence.Status != models.DurableJobNeedsReview || occurrence.CompletedAt != nil {
			t.Fatalf("restart replayed uncertain work: jobs=%d calls=%d state=%s processed=%d err=%v", len(q.jobs), calls, occurrence.Status, processed, err)
		}
	}
}

func TestReviewPolicyPreservesCancellationHeartbeatAndPanicUncertainty(t *testing.T) {
	for _, scenario := range []string{"cancel", "deadline", "panic", "heartbeat"} {
		t.Run(scenario, func(t *testing.T) {
			q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
			var repo Repository = q
			if scenario == "heartbeat" {
				repo = &reviewHeartbeatFailure{reviewQueue: q}
			}
			g := lifecycle.New(context.Background())
			defer func() {
				ctx, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				if err := g.StopAndWait(ctx); err != nil {
					t.Error(err)
				}
			}()
			ctx, cancel := context.WithCancel(g.Context())
			defer cancel()
			runner := NewRunner(repo, Options{Queue: "review", Lease: 15 * time.Millisecond})
			if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(ctx context.Context) error {
				switch scenario {
				case "cancel":
					cancel()
					return context.Canceled
				case "deadline":
					return context.DeadlineExceeded
				case "panic":
					panic("token=must-not-leak")
				default:
					<-ctx.Done()
					return ctx.Err()
				}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := runner.RunOnce(ctx); !errors.Is(err, ErrManualReviewRequired) || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("unknown invocation not review-held: %v", err)
			}
			for _, job := range q.jobs {
				if job.Status != models.DurableJobNeedsReview || job.ReplayPolicy != models.DurableJobReviewUnknown || job.CompletedAt != nil {
					t.Fatal("lost durable review policy/hold")
				}
			}
		})
	}
}

type reviewHeartbeatFailure struct{ *reviewQueue }

func (*reviewHeartbeatFailure) ExtendLease(uuid.UUID, string, int64, time.Time) (bool, error) {
	return false, errors.New("heartbeat unavailable token=must-not-leak")
}

type reviewFalseClassifier struct{}

func (reviewFalseClassifier) Error() string              { return "ordinary pause" }
func (reviewFalseClassifier) RequiresManualReview() bool { return false }

func TestReviewClassificationCannotBeMaskedByJoinedPause(t *testing.T) {
	if !RequiresManualReview(errors.Join(reviewFalseClassifier{}, reviewOutcomeError{})) {
		t.Fatal("false classifier masked a required review on another joined branch")
	}
}

func TestReviewPolicyCannotBeDowngradedByOrdinaryRegistration(t *testing.T) {
	q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
	runner := NewRunner(q, Options{Queue: "review"})
	if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := runner.RegisterRecurring("scan", time.Minute, 3, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, job := range q.jobs {
		if job.ReplayPolicy != models.DurableJobReviewUnknown {
			t.Fatal("ordinary registration downgraded persisted review authority")
		}
	}
}

func TestReviewPolicyMissingHandlerCannotReleaseRecurrence(t *testing.T) {
	q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
	runner := NewRunner(q, Options{Queue: "review"})
	if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	delete(runner.handlers, "scan")
	if _, err := runner.RunOnce(context.Background()); !errors.Is(err, ErrManualReviewRequired) {
		t.Fatal("missing handler quietly dead-lettered review-sensitive work")
	}
	for _, job := range q.jobs {
		if job.Status != models.DurableJobNeedsReview || job.Attempts != 0 {
			t.Fatal("unstarted review-sensitive occurrence was consumed/released")
		}
	}
}

func TestReviewPolicyPublicProducersRetainRegisteredAuthority(t *testing.T) {
	for _, producer := range []string{"enqueue", "ensure"} {
		q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
		runner := NewRunner(q, Options{Queue: "review"})
		if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if producer == "enqueue" {
			if job, err := runner.Enqueue("scan", "{}", time.Time{}, 3); err != nil || job.ReplayPolicy != models.DurableJobReviewUnknown {
				t.Fatal("explicit enqueue omitted registered replay policy")
			}
		} else {
			clear(q.jobs) // Fixture simulates no remaining occurrence; no real data.
			if _, err := runner.EnsureScheduled("scan", "{}", time.Time{}, 3); err != nil {
				t.Fatal(err)
			}
			for _, job := range q.jobs {
				if job.ReplayPolicy != models.DurableJobReviewUnknown {
					t.Fatal("public scheduling omitted registered replay policy")
				}
			}
		}
	}
}

func TestRunnerReviewWriteMustBeAcknowledged(t *testing.T) {
	for _, failure := range []error{nil, errors.New("storage unavailable")} {
		q := &reviewQueue{fakeRepo: newFakeRepo(), writeError: failure}
		runner := NewRunner(q, Options{Queue: "review"})
		runner.Register("scan", func(context.Context, Job) error { return reviewOutcomeError{} })
		job, err := runner.Enqueue("scan", "{}", time.Time{}, 3)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runner.RunOnce(context.Background()); err == nil || q.held != 1 || q.jobs[job.ID].Status != models.DurableJobRunning {
			t.Fatal("unacknowledged review write was treated as safe terminal state")
		}
	}
}
