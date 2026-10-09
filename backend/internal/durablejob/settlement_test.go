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

// These acknowledgements are controlled; actual SQL/transaction behavior has
// separate transport probes and gated dedicated-PostgreSQL acceptance tests.
type rejectedSettlementRepository struct {
	*reviewQueue
	settlementError error
	settlements     int
}

func (r *rejectedSettlementRepository) reject() (bool, error) {
	r.settlements++
	return false, r.settlementError
}
func (r *rejectedSettlementRepository) MarkSucceeded(uuid.UUID, string, int64, time.Time) (bool, error) {
	return r.reject()
}
func (r *rejectedSettlementRepository) MarkSucceededWithAttempts(uuid.UUID, string, int64, time.Time, int) (bool, error) {
	return r.reject()
}
func (r *rejectedSettlementRepository) MarkForRetry(uuid.UUID, string, int64, time.Time, int, string) (bool, error) {
	return r.reject()
}
func (r *rejectedSettlementRepository) MarkDeferred(uuid.UUID, string, int64, time.Time, string) (bool, error) {
	return r.reject()
}
func (r *rejectedSettlementRepository) MarkDead(uuid.UUID, string, int64, time.Time, int, string) (bool, error) {
	return r.reject()
}
func (r *rejectedSettlementRepository) CompleteRecurring(uuid.UUID, string, int64, time.Time, string, int, string, *models.DurableJob) (bool, bool, error) {
	_, err := r.reject()
	return false, false, err
}

func (r *rejectedSettlementRepository) CompleteReviewRecurring(context.Context, uuid.UUID, string, int64, time.Time, string, int, string, *models.DurableJob) (bool, bool, error) {
	_, err := r.reject()
	return false, false, err
}

type settlementLegacyRepository struct {
	Repository
	q *reviewQueue
}

func (r *settlementLegacyRepository) MarkForReview(ctx context.Context, id uuid.UUID, worker string, generation int64, now time.Time, attempts int, reason string) (bool, error) {
	return r.q.MarkForReview(ctx, id, worker, generation, now, attempts, reason)
}

func (r *settlementLegacyRepository) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	return r.q.ReapExpiredLeasesForQueue(queue, now, lease)
}

func TestSettlementUnacknowledgedWritesCannotReportSuccess(t *testing.T) {
	for _, policy := range []string{models.DurableJobReplayAtLeastOnce, models.DurableJobReviewUnknown} {
		for _, operation := range []string{"success", "legacy-success", "retry", "dead", "defer", "recurring", "missing-handler"} {
			for _, fail := range []bool{false, true} {
				t.Run(policy+"/"+operation+map[bool]string{false: "/no-ack", true: "/error"}[fail], func(t *testing.T) {
					q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
					r := &rejectedSettlementRepository{reviewQueue: q}
					if fail {
						r.settlementError = errors.New("storage token=must-not-leak")
					}
					var repository Repository = r
					if operation == "legacy-success" {
						repository = &settlementLegacyRepository{Repository: r, q: q}
					}
					runner := NewRunner(repository, Options{Queue: "settlement"})
					result := error(nil)
					attempts := 3
					if operation == "retry" || operation == "dead" {
						result = errors.New("confirmed handler failure")
					}
					if operation == "dead" {
						attempts = 1
					}
					if operation == "defer" {
						result = Defer("known safety pause")
					}
					if operation == "recurring" {
						if err := runner.RegisterRecurring("scan", time.Minute, attempts, func(context.Context) error { return result }); err != nil {
							t.Fatal(err)
						}
					} else if operation != "missing-handler" {
						runner.Register("scan", func(context.Context, Job) error { return result })
					}
					if len(q.jobs) == 0 {
						if _, err := q.Enqueue(&models.DurableJob{Queue: "settlement", Kind: "scan", RunAt: time.Now().Add(-time.Minute), MaxAttempts: attempts, ReplayPolicy: policy}); err != nil {
							t.Fatal(err)
						}
					}
					for _, job := range q.jobs {
						job.ReplayPolicy = policy
					}
					_, err := runner.RunOnce(context.Background())
					if err == nil {
						t.Fatal("unacknowledged job write was an ordinary successful poll")
					}
					if strings.Contains(err.Error(), "must-not-leak") {
						t.Fatal("settlement error exposed a recognized secret")
					}
					// Missing review handlers are held before any ordinary write.
					if fail && !(operation == "missing-handler" && policy == models.DurableJobReviewUnknown) && !errors.Is(err, r.settlementError) {
						t.Fatal("original storage identity was lost")
					}
					for _, job := range q.jobs {
						if policy == models.DurableJobReviewUnknown && (job.Status != models.DurableJobNeedsReview || q.held != 1) {
							t.Fatal("unconfirmed settlement did not enter review")
						}
						if policy == models.DurableJobReplayAtLeastOnce && job.Status != models.DurableJobRunning {
							t.Fatal("failed normal settlement rewrote occurrence")
						}
					}
				})
			}
		}
	}
}

func TestSettlementRejectedHoldRetainsOriginalFailureIdentity(t *testing.T) {
	q := &reviewQueue{fakeRepo: newFakeRepo(), writeError: errors.New("hold storage unavailable")}
	runner := NewRunner(q, Options{Queue: "settlement"})
	original := errors.New("original token=must-not-leak")
	runner.Register("scan", func(context.Context, Job) error { return reviewOutcomeError{cause: original} })
	if _, err := runner.Enqueue("scan", "{}", time.Time{}, 3); err != nil {
		t.Fatal(err)
	}
	_, err := runner.RunOnce(context.Background())
	if !errors.Is(err, original) || !errors.Is(err, q.writeError) || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("failed hold lost original outcome or exposed a secret: %v", err)
	}
}

func TestSettlementRefusedHandlerAdmissionReleasesOnlyUnstartedClaim(t *testing.T) {
	q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
	runner := NewRunner(q, Options{Queue: "settlement"})
	calls := 0
	if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	g := lifecycle.New(context.Background())
	g.Stop()
	_, err := runner.RunOnce(context.WithoutCancel(g.Context()))
	if !errors.Is(err, context.Canceled) || calls != 0 || q.held != 0 {
		t.Fatal("unstarted invocation was treated as uncertain executed work")
	}
	for _, job := range q.jobs {
		if job.Status != models.DurableJobPending || job.Attempts != 0 {
			t.Fatal("never-started occurrence consumed an attempt or retained a lease")
		}
	}
}
