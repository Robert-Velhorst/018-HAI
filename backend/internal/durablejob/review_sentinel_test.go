package durablejob

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func TestReviewSentinelClassificationSurvivesWrappingAndJoinedPause(t *testing.T) {
	for _, err := range []error{
		ErrManualReviewRequired,
		fmt.Errorf("nested review: %w", ErrManualReviewRequired),
		errors.Join(reviewFalseClassifier{}, ErrManualReviewRequired),
		errors.Join(Defer("paused"), ErrManualReviewRequired),
	} {
		if !RequiresManualReview(err) {
			t.Errorf("review requirement was lost: %v", err)
		}
	}
	for _, err := range []error{nil, errors.New("ordinary failure"), Defer("paused"), reviewFalseClassifier{}} {
		if RequiresManualReview(err) {
			t.Errorf("ordinary outcome incorrectly requires review: %v", err)
		}
	}
}

func TestRunnerHoldsExplicitReviewSentinelWithoutRetryOrSuccessor(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("paused=%v", paused), func(t *testing.T) {
			q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
			now := time.Now().UTC()
			runner := NewRunner(q, Options{Queue: "review", Now: func() time.Time { return now }})
			calls := 0
			if err := runner.RegisterRecurring("scan", time.Minute, 3, func(context.Context) error {
				calls++
				if paused {
					return errors.Join(Defer("policy pause"), ErrManualReviewRequired)
				}
				return ErrManualReviewRequired
			}); err != nil {
				t.Fatal(err)
			}
			processed, err := runner.RunOnce(context.Background())
			if processed != 1 || !errors.Is(err, ErrManualReviewRequired) || !RequiresManualReview(err) {
				t.Fatalf("explicit review not surfaced: processed=%d err=%v", processed, err)
			}
			if q.held != 1 || len(q.jobs) != 1 {
				t.Fatalf("review did not hold one occurrence: held=%d jobs=%d", q.held, len(q.jobs))
			}
			for _, job := range q.jobs {
				if job.Status != models.DurableJobNeedsReview || job.Attempts != 1 || job.CompletedAt != nil {
					t.Fatalf("review state lost: %+v", job)
				}
			}
			now = now.Add(time.Hour)
			if processed, err := runner.RunOnce(context.Background()); processed != 0 || err != nil || calls != 1 || len(q.jobs) != 1 {
				t.Fatalf("reviewed work ran again: processed=%d calls=%d jobs=%d err=%v", processed, calls, len(q.jobs), err)
			}
		})
	}
}
