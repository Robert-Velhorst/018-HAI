//go:build integration

package durablejob

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

// Acceptance definitions only until explicitly run against the dedicated
// database. integrationRepo enforces the destructive-test opt-in/DSN guard.
func TestReviewPolicyPostgresCrashRecoveryAndRestart(t *testing.T) {
	repo, db := integrationRepo(t)
	now := time.Now().UTC()
	locked := now.Add(-time.Hour)
	for _, scenario := range []struct {
		kind     string
		attempts int
		lockedAt *time.Time
	}{
		{"review-first", 0, &locked}, {"review-final", 2, &locked}, {"review-missing-lease", 0, nil},
	} {
		job, err := repo.Enqueue(&models.DurableJob{Queue: "review", Kind: scenario.kind, ReplayPolicy: models.DurableJobReviewUnknown,
			Status: models.DurableJobRunning, RunAt: locked, Attempts: scenario.attempts, MaxAttempts: 3, LockedAt: scenario.lockedAt,
			LockedBy: "crashed", LeaseGeneration: 7})
		if err != nil {
			t.Fatal(err)
		}
		runner := NewRunner(repo, Options{Queue: "review", Now: func() time.Time { return now }})
		if err := runner.RegisterReviewRecurring(scenario.kind, time.Minute, 3, func(context.Context) error { t.Fatal("uncertain occurrence replayed"); return nil }); err != nil {
			t.Fatal(err)
		}
		if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 {
			t.Fatalf("processed=%d err=%v", processed, err)
		}
		stored, err := repo.Find(job.ID)
		if err != nil || stored.Status != models.DurableJobNeedsReview || stored.Attempts != scenario.attempts+1 || stored.CompletedAt != nil || stored.LockedAt != nil || stored.LockedBy != "" {
			t.Fatalf("invalid held occurrence: %+v err=%v", stored, err)
		}
		var count int64
		if err := db.Model(&models.DurableJob{}).Where("queue = ? AND kind = ?", "review", scenario.kind).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("review was replaced: count=%d err=%v", count, err)
		}
		if err := runner.RegisterReviewRecurring(scenario.kind, time.Minute, 3, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if owned, err := repo.MarkSucceeded(job.ID, "crashed", 7, now); err != nil || owned {
			t.Fatal("stale worker overwrote review hold")
		}
	}
}

func TestReviewPolicyPostgresUpgradeAndClaimHold(t *testing.T) {
	repo, _ := integrationRepo(t)
	now := time.Now().UTC()
	locked := now.Add(-time.Hour)
	legacy, err := repo.Enqueue(&models.DurableJob{Queue: "review", Kind: "scan", Status: models.DurableJobRunning, LockedBy: "old-worker", LockedAt: &locked, LeaseGeneration: 4, RunAt: locked, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(repo, Options{Queue: "review", Now: func() time.Time { return now }})
	if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { t.Fatal("legacy uncertain work ran"); return nil }); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Find(legacy.ID)
	if err != nil || stored.ReplayPolicy != models.DurableJobReviewUnknown || stored.LeaseGeneration != 4 {
		t.Fatal("startup failed to upgrade existing active work without rewriting its lease")
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 {
		t.Fatalf("legacy work replayed: %d %v", processed, err)
	}
	duplicate, err := repo.Enqueue(&models.DurableJob{Queue: "review", Kind: "scan", RunAt: locked})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.Enqueue(&models.DurableJob{Queue: "different-queue", Kind: "scan", RunAt: locked})
	if err != nil {
		t.Fatal(err)
	}
	if jobs, err := repo.ClaimDue("new", "review", now, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("pending duplicate bypassed hold: %v %v", jobs, err)
	}
	if jobs, err := repo.ClaimDue("new", "different-queue", now, 10); err != nil || len(jobs) != 1 || jobs[0].ID != other.ID {
		t.Fatal("review hold incorrectly blocked another queue")
	}
	stored, err = repo.Find(duplicate.ID)
	if err != nil || stored.Status != models.DurableJobPending {
		t.Fatal("blocked duplicate was rewritten")
	}
}
