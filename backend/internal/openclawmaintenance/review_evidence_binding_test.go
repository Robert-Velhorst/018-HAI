package openclawmaintenance

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/pgtestguard"
)

func TestMaintenanceReviewEvidenceBindsTargetReceiptAndObservation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	checkedAt, startedAt, createdAt := now.Add(-time.Minute), now.Add(-2*time.Minute), now.Add(-3*time.Minute)
	target := Target{ID: "companion", ReceiptID: "review-check", CheckedAt: &checkedAt, Installed: "2026.6.10", Available: "2026.9.1"}
	receipt := Job{ID: target.ReceiptID, Target: target.ID, Kind: "check", Status: "completed", Worker: "worker", LeaseDigest: digest("fixture-token"), CreatedAt: createdAt, StartedAt: &startedAt, FinishedAt: &checkedAt}
	report := checkReportAt(checkedAt)
	if !verifiedReviewEvidence(&target, &receipt, &report, now) {
		t.Fatal("valid review evidence was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Target, *Job, *Report)
	}{
		{"wrong target", func(_ *Target, j *Job, _ *Report) { j.Target = "gateway_core" }},
		{"wrong receipt", func(_ *Target, j *Job, _ *Report) { j.ID = "different-check" }},
		{"different observation time", func(_ *Target, j *Job, _ *Report) { t := checkedAt.Add(-time.Second); j.FinishedAt = &t }},
		{"different installed version", func(t *Target, _ *Job, _ *Report) { t.Installed = "2026.8.1" }},
		{"different available version", func(t *Target, _ *Job, _ *Report) { t.Available = "2026.10.1" }},
		{"missing worker", func(_ *Target, j *Job, _ *Report) { j.Worker = "" }},
		{"missing lease", func(_ *Target, j *Job, _ *Report) { j.LeaseDigest = "" }},
		{"missing start", func(_ *Target, j *Job, _ *Report) { j.StartedAt = nil }},
		{"start before creation", func(_ *Target, j *Job, _ *Report) { j.CreatedAt = checkedAt }},
		{"completion before start", func(_ *Target, j *Job, _ *Report) { t := checkedAt.Add(time.Second); j.StartedAt = &t }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidateTarget, candidateReceipt, candidateReport := target, receipt, report
			test.mutate(&candidateTarget, &candidateReceipt, &candidateReport)
			if verifiedReviewEvidence(&candidateTarget, &candidateReceipt, &candidateReport, now) {
				t.Fatal("mismatched review evidence was accepted")
			}
		})
	}
}

func TestMaintenanceReviewRejectsMismatchedReceiptPostgres(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "TEST_OPENCLAW_MAINTENANCE_DSN", maintenanceTestDatabase)
	for _, mutation := range []string{"other target receipt", "different installed version", "missing worker", "different completion time"} {
		t.Run(mutation, func(t *testing.T) {
			s := testService(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			s.now = func() time.Time { return now }
			ctx := context.Background()
			lease := applyLease(t, s)
			report := checkReportAt(now)
			report.Outcome = "update_failed"
			if err := s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
				t.Fatal(err)
			}
			var recovery Job
			if err := s.db.First(&recovery, "target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Error; err != nil {
				t.Fatal(err)
			}
			if !recovery.CreatedAt.Equal(now) {
				t.Fatal("recovery check must record its actual creation time")
			}
			now = now.Add(2 * time.Second)
			completeCheckFor(t, s, "companion")
			completeCheckFor(t, s, "gateway_core")
			var target, gateway Target
			var receipt Job
			if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.First(&gateway, "id = ?", "gateway_core").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.First(&receipt, "id = ?", target.ReceiptID).Error; err != nil {
				t.Fatal(err)
			}
			originalTarget, originalReceipt := target, receipt
			switch mutation {
			case "other target receipt":
				target.ReceiptID = gateway.ReceiptID
			case "different installed version":
				target.Installed = "2026.8.1"
			case "missing worker":
				receipt.Worker = ""
			case "different completion time":
				earlier := now.Add(-time.Second)
				receipt.FinishedAt = &earlier
			}
			if err := s.db.Save(&target).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Save(&receipt).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Review(ctx, "companion", "owner"); err == nil {
				t.Fatal("review accepted mismatched persisted evidence")
			}
			var failed Job
			if err := s.db.First(&failed, "id = ?", lease.Job.ID).Error; err != nil {
				t.Fatal(err)
			}
			if failed.Status != "needs_review" {
				t.Fatal("rejected review changed interrupted update")
			}
			var audits int64
			if err := s.db.Model(&Job{}).Where("kind = ?", "review").Count(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if audits != 0 {
				t.Fatal("rejected review wrote a success audit")
			}
			if err := s.db.Save(&originalTarget).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Save(&originalReceipt).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Review(ctx, "companion", "owner"); err != nil {
				t.Fatalf("valid post-failure observation rejected: %v", err)
			}
			if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
				t.Fatal(err)
			}
			if target.Policy != "observe" {
				t.Fatal("review enabled automatic installation")
			}
		})
	}
}
