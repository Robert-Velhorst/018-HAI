package durablejob

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

type claimFailureAfterReview struct {
	*reviewQueue
	claimCalls int
	claimErr   error
}

func (r *claimFailureAfterReview) ClaimDue(worker, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	r.claimCalls++
	if r.claimCalls > 1 {
		return nil, r.claimErr
	}
	return r.reviewQueue.ClaimDue(worker, queue, now, limit)
}

func TestLaterClaimFailurePreservesEarlierReviewOutcome(t *testing.T) {
	claimErr := errors.New("claim storage unavailable")
	q := &claimFailureAfterReview{reviewQueue: &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}, claimErr: claimErr}
	runner := NewRunner(q, Options{Queue: "review", Batch: 2})
	if err := runner.RegisterRecurring("scan", time.Minute, 3, func(context.Context) error { return ErrManualReviewRequired }); err != nil {
		t.Fatal(err)
	}
	processed, err := runner.RunOnce(context.Background())
	if processed != 1 || !errors.Is(err, claimErr) || !errors.Is(err, ErrManualReviewRequired) || !RequiresManualReview(err) {
		t.Fatalf("later claim failure lost review outcome: processed=%d err=%v", processed, err)
	}
	if q.held != 1 || len(q.jobs) != 1 {
		t.Fatal("review hold was changed by later claim failure")
	}
}

func TestRunnerPollLogRedactsRepositoryCredentials(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	repo := &failingReapRepo{fakeRepo: newFakeRepo(), err: errors.New("provider unavailable token=must-not-leak")}
	runner := NewRunner(repo, Options{Queue: "review"})
	runner.runAndReport(context.Background())
	if text := output.String(); strings.Contains(text, "must-not-leak") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("poll log did not redact repository credentials: %q", text)
	}
}
