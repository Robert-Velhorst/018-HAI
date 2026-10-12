package openclawmaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s := testServiceWithDefaultPolicy(t)
	for _, id := range []string{"gateway_core", "companion"} {
		if err := s.Policy(context.Background(), id, "observe", "test-owner"); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func testServiceWithDefaultPolicy(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("TEST_OPENCLAW_MAINTENANCE_DSN")
	if dsn == "" {
		t.Skip("set TEST_OPENCLAW_MAINTENANCE_DSN to an isolated PostgreSQL test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	probe, cancelProbe := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelProbe()
	rawDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err = rawDB.PingContext(probe); err != nil {
		rawDB.Close()
		t.Fatalf("test PostgreSQL unavailable: %v", err)
	}
	schema := "hai_maintenance_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	// Each test owns a new schema; no existing application tables are touched.
	scoped, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw, _ := scoped.DB()
		raw.Close()
		db.Exec("DROP SCHEMA " + schema + " CASCADE")
		raw, _ = db.DB()
		raw.Close()
	})
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "pre", "0072_openclaw_maintenance.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err = scoped.Exec(string(sql)).Error; err != nil {
		t.Fatal(err)
	}
	retryBudgetMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "pre", "0083_openclaw_maintenance_retry_budget.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err = scoped.Exec(string(retryBudgetMigration)).Error; err != nil {
		t.Fatal(err)
	}
	if err = scoped.Exec("CREATE TABLE openclaw_gateway_session_receipts (status text, reconciled_at timestamptz)").Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(scoped)
	s.SetBackgroundProcessingGate(func() bool { return true })
	// Service workflow tests may exercise the install authorization path with a
	// fake executor; the production default remains fail-closed.
	s.canSafelyInstall = func() bool { return true }
	s.SetHealthVerifier(func(context.Context) bool { return true })
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDefaultPoliciesRequireExplicitOwnerApproval(t *testing.T) {
	for _, id := range []string{"gateway_core", "companion"} {
		if got := defaultPolicy(id); got != "observe" {
			t.Errorf("%s default policy = %q, want observe until an owner opts in", id, got)
		}
	}
}

func TestTargetSpecificInstallGateFailsClosed(t *testing.T) {
	s := NewService(nil)
	s.SetBackgroundProcessingGate(func() bool { return true })
	s.canSafelyInstall = func() bool { return true }
	if err := s.canApply(nil, &Target{ID: "gateway_core"}, "2026.9.1"); !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("Gateway/core install error = %v, want WSL-specific blocker", err)
	}
	if err := s.canApply(nil, &Target{ID: "companion"}, "2026.9.1"); err == nil {
		t.Fatalf("Companion install passed without owner-approved verified update state: %v", err)
	}
	s.canSafelyInstall = nil
	if err := s.canApply(nil, &Target{ID: "companion"}, "2026.9.1"); !errors.Is(err, ErrCompanionCapabilityRequired) {
		t.Fatalf("Companion install error = %v, want fresh-capability blocker", err)
	}
}

func TestEmergencyStopBlocksMaintenanceApplyBeforeAnyDatabaseWork(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	s := NewService(nil)
	s.SetBackgroundProcessingGate(func() bool { return true })
	s.canSafelyInstall = func() bool { return true }
	target := &Target{ID: "companion", Policy: "auto_install_verified", State: "update_available", Installed: "2026.6.10", Available: "2026.9.1", CheckedAt: timePointer(time.Now().UTC())}
	if err := s.canApply(nil, target, "2026.9.1"); err == nil {
		t.Fatal("maintenance apply passed while emergency stop was active")
	}
}

func TestGlobalPauseBlocksOpenClawScheduleLeaseAndApplyAdmission(t *testing.T) {
	s := NewService(nil)
	s.SetBackgroundProcessingGate(func() bool { return false })
	if err := s.Schedule(context.Background()); err != nil {
		t.Fatalf("paused Schedule returned an error: %v", err)
	}
	lease, err := s.Lease(context.Background(), "worker")
	if err != nil || lease != nil {
		t.Fatalf("paused Lease = (%+v, %v), want no job and no error", lease, err)
	}
	target := &Target{ID: "companion", Policy: "auto_install_verified", State: "update_available"}
	if err := s.canApply(nil, target, "2026.9.1"); !errors.Is(err, ErrBackgroundProcessingPaused) {
		t.Fatalf("paused canApply = %v, want ErrBackgroundProcessingPaused", err)
	}
}

func TestCheckRetryDelayFallsBackToDailyReadOnlyProbes(t *testing.T) {
	for _, tt := range []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: checkRetryInterval},
		{failures: 1, want: checkRetryInterval},
		{failures: maxAutomaticCheckFailures - 1, want: checkRetryInterval},
		{failures: maxAutomaticCheckFailures, want: checkInterval},
		{failures: maxAutomaticCheckFailures + 1, want: checkInterval},
	} {
		if got := checkRetryDelay(tt.failures); got != tt.want {
			t.Errorf("checkRetryDelay(%d) = %s, want %s", tt.failures, got, tt.want)
		}
	}
}

func TestUnattendedInstallationFailsClosedButDailyChecksRemainAvailable(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	s.canSafelyInstall = func() bool { return true }
	completeCheck(t, s)
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "update_available" || target.Available != "2026.9.1" {
		t.Fatalf("read-only check lost the available version: %+v", target)
	}
	var applies int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "gateway_core", "apply", []string{"pending", "leased"}).Count(&applies).Error; err != nil {
		t.Fatal(err)
	}
	if applies != 0 {
		t.Fatalf("queued %d unattended installs while process-tree termination is unsupported", applies)
	}
	if err := s.Apply(context.Background(), "gateway_core", target.Available); !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("manual unattended install error = %v, want explicit fail-closed blocker", err)
	}
	if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Update("next_check", s.now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	var checks int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "check", "pending").Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("daily read-only check queue unavailable while installs are blocked: pending=%d", checks)
	}
}

func TestOverviewExposesCompanionPublisherPinBlocker(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	// This scenario must report both independent blockers: publisher identity
	// is unverified and the worker has not proved process-tree containment.
	s.canSafelyInstall = func() bool { return false }
	ctx := context.Background()
	if err := s.Check(ctx, "companion"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(ctx, "worker")
	if err != nil || lease == nil {
		t.Fatalf("lease Companion check: lease=%+v err=%v", lease, err)
	}
	if err = s.Confirm(ctx, lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	report := checkReport()
	report.PublisherVerified = false
	report.PublisherPinStatus = "missing"
	if err = s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal(err)
	}
	targets, err := s.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ID != "companion" {
			continue
		}
		if target.State != "update_available" || target.InstallStatus != "blocked" || !strings.Contains(target.InstallBlockedReason, "THUMBPRINT is not configured") || !strings.Contains(target.InstallBlockedReason, "Job Object containment") {
			t.Fatalf("Companion update did not expose both fail-closed blockers: %+v", target)
		}
		return
	}
	t.Fatal("Companion target missing from overview")
}

func TestPublisherPinStillBlocksApplyWhenContainmentIsAvailable(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	ctx := context.Background()
	if err := s.Check(ctx, "companion"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(ctx, "worker")
	if err != nil || lease == nil {
		t.Fatalf("lease Companion check: lease=%+v err=%v", lease, err)
	}
	if err = s.Confirm(ctx, lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	report := checkReport()
	report.PublisherVerified = false
	report.PublisherPinStatus = "missing"
	if err = s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal(err)
	}
	if err = s.Policy(ctx, "companion", "auto_install_verified", "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(ctx, "companion", report.Available); err == nil || !strings.Contains(err.Error(), "publisher verification") {
		t.Fatalf("apply with missing publisher pin = %v, want publisher-verification blocker", err)
	}
}

func TestVerifiedApplyEvidenceRejectsStaleFutureAndMismatchedReceipts(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	checkedAt := now.Add(-time.Minute)
	target := Target{ID: "gateway_core", ReceiptID: "check-1", Installed: "2026.6.10", Available: "2026.9.1", CheckedAt: &checkedAt}
	receipt := Job{ID: "check-1", Target: target.ID, Kind: "check", Status: "completed", FinishedAt: &checkedAt}
	report := Report{Installed: target.Installed, Available: target.Available, Evidence: strings.Repeat("a", 64), ProviderMetadataAt: &checkedAt, PublisherVerified: true, Outcome: "ok"}
	if !verifiedApplyEvidence(&target, &receipt, &report, target.Available, now) {
		t.Fatal("rejected a current, completed, publisher-verified check receipt")
	}

	for _, mutate := range []struct {
		name string
		fn   func(*Target, *Job, *Report)
	}{
		{"stale target timestamp", func(target *Target, receipt *Job, _ *Report) {
			stale := now.Add(-checkInterval - time.Second)
			target.CheckedAt, receipt.FinishedAt = &stale, &stale
		}},
		{"check expires at next scheduled check", func(target *Target, receipt *Job, _ *Report) {
			due := now.Add(-checkInterval)
			target.CheckedAt, receipt.FinishedAt = &due, &due
		}},
		{"future target timestamp", func(target *Target, receipt *Job, _ *Report) {
			future := now.Add(time.Second)
			target.CheckedAt, receipt.FinishedAt = &future, &future
		}},
		{"receipt target mismatch", func(_ *Target, receipt *Job, _ *Report) { receipt.Target = "companion" }},
		{"receipt not completed", func(_ *Target, receipt *Job, _ *Report) { receipt.Status = "pending" }},
		{"receipt is not a check", func(_ *Target, receipt *Job, _ *Report) { receipt.Kind = "apply" }},
		{"receipt timestamp mismatch", func(_ *Target, receipt *Job, _ *Report) {
			stale := now.Add(-2 * time.Hour)
			receipt.FinishedAt = &stale
		}},
		{"installed version mismatch", func(_ *Target, _ *Job, report *Report) { report.Installed = "2026.6.9" }},
		{"failed read-only check", func(_ *Target, _ *Job, report *Report) { report.Outcome = "unavailable" }},
		{"publisher not verified", func(_ *Target, _ *Job, report *Report) { report.PublisherVerified = false }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			targetCopy, receiptCopy, reportCopy := target, receipt, report
			mutate.fn(&targetCopy, &receiptCopy, &reportCopy)
			if verifiedApplyEvidence(&targetCopy, &receiptCopy, &reportCopy, target.Available, now) {
				t.Fatal("accepted stale or mismatched update evidence")
			}
		})
	}
}

func TestGatewayCoreCheckNeverQueuesAnInstall(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	completeCheck(t, s)

	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	var receipt Job
	if err := s.db.First(&receipt, "id = ?", target.ReceiptID).Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckedAt == nil || receipt.FinishedAt == nil || !target.CheckedAt.Equal(*receipt.FinishedAt) || target.CheckedAt.Nanosecond()%int(time.Microsecond) != 0 {
		t.Fatalf("persisted check timestamps differ or exceed database precision: checked_at=%v receipt_finished_at=%v", target.CheckedAt, receipt.FinishedAt)
	}
	if target.Policy != "observe" || target.State != "update_available" {
		t.Fatalf("read-only Gateway release state or default policy is wrong: %+v", target)
	}
	var jobs []Job
	if err := s.db.Where("target = ? AND kind = ? AND status = ?", "gateway_core", "apply", "pending").Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("Gateway/core queued an install despite WSL containment blocker: %+v", jobs)
	}
}

func TestVerifiedCompanionUpdateQueuesOnlyAfterOwnerApprovalAndCapability(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	if err := s.Policy(context.Background(), "companion", "auto_install_verified", "test-owner"); err != nil {
		t.Fatal(err)
	}
	completeCheckFor(t, s, "companion")

	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.Policy != "auto_install_verified" || target.State != "update_available" {
		t.Fatalf("verified newer Companion release was not recognized: %+v", target)
	}
	var jobs []Job
	if err := s.db.Where("target = ? AND kind = ? AND status = ?", "companion", "apply", "pending").Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Version != "2026.9.1" || jobs[0].Evidence != checkReport().Evidence {
		t.Fatalf("verified Companion update was not queued exactly once with its evidence: %+v", jobs)
	}
}

func TestInitializePreservesExplicitGatewayObservePolicy(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	if err := s.Policy(context.Background(), "gateway_core", "observe", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.Policy != "observe" || target.UpdatedBy != "owner" {
		t.Fatalf("restart changed explicit owner policy: policy=%s updatedBy=%s", target.Policy, target.UpdatedBy)
	}
}

func TestInitializePreservesExplicitCompanionObservePolicy(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	if err := s.Policy(context.Background(), "companion", "observe", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.Policy != "observe" || target.UpdatedBy != "owner" {
		t.Fatalf("restart changed explicit Companion policy: policy=%s updatedBy=%s", target.Policy, target.UpdatedBy)
	}
}

func TestInitializeDisablesLegacyImplicitAutomaticPolicy(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Update("policy", "auto_install_verified").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.Policy != "observe" || target.UpdatedBy != "" {
		t.Fatalf("implicit legacy install policy was not disabled: policy=%s updatedBy=%s", target.Policy, target.UpdatedBy)
	}
	var audit Job
	if err := s.db.Where("target = ? AND kind = ?", "gateway_core", "policy").Order("created_at DESC").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	var details map[string]string
	if json.Unmarshal([]byte(audit.Result), &details) != nil || details["owner"] != "system-safety-default" {
		t.Fatal("legacy policy downgrade was not recorded in the audit ledger")
	}
}

func TestInitializePreservesOwnerApprovedCompanionPolicy(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	if err := s.Policy(context.Background(), "companion", "auto_install_verified", "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.Policy != "auto_install_verified" || target.UpdatedBy != "test-owner" {
		t.Fatalf("owner-approved Companion policy was not preserved: policy=%s updatedBy=%s", target.Policy, target.UpdatedBy)
	}
	var audit Job
	if err := s.db.Where("target = ? AND kind = ?", "companion", "policy").Order("created_at DESC").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	var details map[string]string
	if json.Unmarshal([]byte(audit.Result), &details) != nil || details["owner"] != "test-owner" {
		t.Fatal("owner approval was not recorded in the policy audit ledger")
	}
}

func TestUnresolvedGatewayAdmissionAndReviewBlockMaintenanceUpdates(t *testing.T) {
	for _, status := range []string{"admitting", "admitted", "needs_review"} {
		t.Run(status, func(t *testing.T) {
			s := testService(t)
			completeCheckFor(t, s, "companion")
			if err := s.Policy(context.Background(), "companion", "auto_install_verified", "owner"); err != nil {
				t.Fatal(err)
			}
			if err := s.db.Exec("INSERT INTO openclaw_gateway_session_receipts (status) VALUES (?)", status).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(context.Background(), "companion", "2026.9.1"); err == nil || !strings.Contains(err.Error(), "active delegated sessions") {
				t.Fatalf("unresolved %s receipt did not block maintenance update: %v", status, err)
			}
		})
	}
}

func checkReport() Report {
	return checkReportAt(time.Now().UTC())
}

func checkReportAt(metadataAt time.Time) Report {
	return Report{Installed: "2026.6.10", Available: "2026.9.1", Evidence: strings.Repeat("a", 64), ProviderMetadataAt: timePointer(metadataAt), PublisherVerified: true, PublisherPinStatus: "verified", Outcome: "ok"}
}
func completeCheck(t *testing.T, s *Service) {
	completeCheckFor(t, s, "gateway_core")
}

func completeCheckFor(t *testing.T, s *Service, target string) {
	t.Helper()
	ctx := context.Background()
	if err := s.Check(ctx, target); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lease(ctx, "worker")
	if err != nil || l == nil {
		t.Fatalf("lease: %v", err)
	}
	if err = s.Confirm(ctx, l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, l.Job.ID, "worker", l.Token, checkReportAt(s.now())); err != nil {
		t.Fatal(err)
	}
}

func completeCurrentCheckFor(t *testing.T, s *Service, target string) {
	t.Helper()
	ctx := context.Background()
	if err := s.Check(ctx, target); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lease(ctx, "worker")
	if err != nil || l == nil || l.Job.Target != target || l.Job.Kind != "check" {
		t.Fatalf("lease current check for %s: lease=%+v err=%v", target, l, err)
	}
	if err := s.Confirm(ctx, l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	report := checkReportAt(s.now())
	report.Installed, report.Available = "2026.9.1", "2026.9.1"
	if err := s.Complete(ctx, l.Job.ID, "worker", l.Token, report); err != nil {
		t.Fatal(err)
	}
}

func prepareFreshCurrentChecks(t *testing.T, s *Service) time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	// Lease checks gateway_core before companion, so seed in the same order.
	for _, id := range []string{"gateway_core", "companion"} {
		completeCurrentCheckFor(t, s, id)
	}
	return now
}

func applyLease(t *testing.T, s *Service) *Lease {
	t.Helper()
	ctx := context.Background()
	completeCheckFor(t, s, "companion")
	if err := s.Policy(ctx, "companion", "auto_install_verified", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, "companion", "2026.9.1"); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lease(ctx, "worker")
	if err != nil || l == nil {
		t.Fatalf("lease: %v", err)
	}
	if err = s.Confirm(ctx, l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestConcurrentCheckQueuesOnce(t *testing.T) {
	s := testService(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Check(context.Background(), "gateway_core"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var n int64
	s.db.Model(&Job{}).Where("kind = ?", "check").Count(&n)
	if n != 1 {
		t.Fatalf("queued %d checks", n)
	}
}

func TestConcurrentScheduledChecksQueueOncePerTarget(t *testing.T) {
	s := testService(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if err := s.db.Model(&Target{}).Where("id IN ?", []string{"companion", "gateway_core"}).Update("next_check", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Schedule(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, target := range []string{"companion", "gateway_core"} {
		var pending int64
		if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", target, "check", "pending").Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if pending != 1 {
			t.Fatalf("target %s queued %d pending checks, want one", target, pending)
		}
	}
	if _, due, err := s.NextScheduledCheck(context.Background()); err != nil || due {
		t.Fatalf("active target checks should suppress another deadline wake: due=%t err=%v", due, err)
	}
}

func TestRestartCatchesUpPersistedOverdueCheck(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	due := now.Add(-30 * time.Minute)
	if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Update("next_check", due).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("next_check", now.Add(checkInterval)).Error; err != nil {
		t.Fatal(err)
	}

	// A fresh service instance represents a backend restart; the persisted
	// NextCheck, rather than an in-memory timer, is the source of truth.
	restarted := NewService(s.db)
	restarted.SetBackgroundProcessingGate(func() bool { return true })
	restarted.now = func() time.Time { return now }
	if err := restarted.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	gotDue, hasDue, err := restarted.NextScheduledCheck(ctx)
	if err != nil || !hasDue || !gotDue.Equal(due) {
		t.Fatalf("restart lost the overdue deadline: due=%s found=%t err=%v", gotDue, hasDue, err)
	}
	if err = restarted.Schedule(ctx); err != nil {
		t.Fatal(err)
	}
	var gatewayPending, companionPending int64
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "check", "pending").Count(&gatewayPending).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Count(&companionPending).Error; err != nil {
		t.Fatal(err)
	}
	if gatewayPending != 1 || companionPending != 0 {
		t.Fatalf("restart catch-up queued gateway=%d companion=%d checks; want 1 and 0", gatewayPending, companionPending)
	}
}

func TestSuccessfulCheckStartsTwentyFourHourIntervalAtCompletion(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	var before Target
	if err := s.db.First(&before, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), "gateway_core"); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if !target.NextCheck.Equal(before.NextCheck) {
		t.Fatalf("queueing moved the check deadline from %s to %s", before.NextCheck, target.NextCheck)
	}
	l, err := s.Lease(context.Background(), "worker")
	if err != nil || l == nil {
		t.Fatalf("lease: %v", err)
	}
	if err = s.Confirm(context.Background(), l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	if err = s.Complete(context.Background(), l.Job.ID, "worker", l.Token, checkReportAt(now)); err != nil {
		t.Fatal(err)
	}
	if err = s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckedAt == nil || !target.CheckedAt.Equal(now) || !target.NextCheck.Equal(now.Add(checkInterval)) {
		t.Fatalf("successful completion did not anchor the next interval: checked=%v next=%s", target.CheckedAt, target.NextCheck)
	}
}

func TestFailedCheckPreservesLastGoodStateAndSchedulesBoundedRetry(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("next_check", now.AddDate(10, 0, 0)).Error; err != nil {
		t.Fatal(err)
	}
	completeCheck(t, s)
	var before Target
	if err := s.db.First(&before, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), "gateway_core"); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lease(context.Background(), "worker")
	if err != nil || l == nil {
		t.Fatalf("lease: %v", err)
	}
	if err = s.Confirm(context.Background(), l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	failed := Report{Evidence: strings.Repeat("0", 64), Outcome: "unavailable"}
	if err = s.Complete(context.Background(), l.Job.ID, "worker", l.Token, failed); err != nil {
		t.Fatal(err)
	}
	var after Target
	if err = s.db.First(&after, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if after.Installed != before.Installed || after.Available != before.Available || after.ReceiptID != before.ReceiptID || after.CheckedAt == nil || !after.CheckedAt.Equal(*before.CheckedAt) {
		t.Fatal("failed check overwrote last confirmed version evidence")
	}
	if after.Policy != "observe" || after.State != "check_failed" || !after.NextCheck.Equal(now.Add(checkRetryInterval)) {
		t.Fatalf("failed check recovery state incorrect: policy=%s state=%s next=%s", after.Policy, after.State, after.NextCheck)
	}
	if next, found, err := s.NextScheduledCheck(context.Background()); err != nil || !found || !next.Equal(after.NextCheck) {
		t.Fatalf("scheduler lost persisted retry deadline: next=%s found=%t err=%v", next, found, err)
	}
	if err = s.Apply(context.Background(), "gateway_core", "2026.9.1"); !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("failed check install attempt did not preserve the WSL blocker: %v", err)
	}
	var failedJob Job
	if err = s.db.First(&failedJob, "id = ?", l.Job.ID).Error; err != nil || failedJob.Status != "failed" {
		t.Fatalf("read-only check was not recorded as retryable failure: status=%s err=%v", failedJob.Status, err)
	}
	if err = s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	var checks int64
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "check", "pending").Count(&checks).Error; err != nil || checks != 0 {
		t.Fatalf("scheduled retry before retry deadline: count=%d err=%v", checks, err)
	}
	now = after.NextCheck
	if err = s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "check", "pending").Count(&checks).Error; err != nil || checks != 1 {
		t.Fatalf("due read-only retry not queued once: count=%d err=%v", checks, err)
	}
}

func TestStaleProviderMetadataCannotRefreshTargetOrAuthorizeUpdate(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	completeCheckFor(t, s, "companion")
	var before Target
	if err := s.db.First(&before, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), "companion"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(context.Background(), "worker")
	if err != nil || lease == nil || lease.Job.Kind != "check" {
		t.Fatalf("lease: %+v, %v", lease, err)
	}
	if err := s.Confirm(context.Background(), lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	stale := checkReportAt(now.Add(-providerMetadataMaxAge - time.Second))
	if err := s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, stale); err != nil {
		t.Fatalf("stale provider data should be recorded as an unavailable check: %v", err)
	}
	var after Target
	if err := s.db.First(&after, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if after.Installed != before.Installed || after.Available != before.Available || after.ReceiptID != before.ReceiptID ||
		after.CheckedAt == nil || before.CheckedAt == nil || !after.CheckedAt.Equal(*before.CheckedAt) {
		t.Fatalf("stale provider metadata replaced confirmed version evidence: before=%+v after=%+v", before, after)
	}
	if after.State != "check_failed" || after.CheckFailures != 1 || !after.NextCheck.Equal(now.Add(checkRetryInterval)) || !strings.Contains(after.Reason, "provider response was stale") {
		t.Fatalf("stale metadata was not visibly failed and scheduled for bounded retry: %+v", after)
	}
	var rejected Job
	if err := s.db.First(&rejected, "id = ?", lease.Job.ID).Error; err != nil {
		t.Fatal(err)
	}
	var recorded Report
	if err := json.Unmarshal([]byte(rejected.Result), &recorded); err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "failed" || recorded.Outcome != "unavailable" || recorded.ProviderMetadataAt == nil {
		t.Fatalf("stale source evidence was not recorded as unavailable: job=%+v report=%+v", rejected, recorded)
	}
	if err := s.Apply(context.Background(), "companion", after.Available); err == nil {
		t.Fatal("stale provider metadata remained usable for an update")
	}
}

func TestSuccessfulCheckReceiptRetrySurvivesProviderMetadataExpiry(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	if err := s.Check(ctx, "gateway_core"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(ctx, "worker")
	if err != nil || lease == nil || lease.Job.Kind != "check" {
		t.Fatalf("lease check: lease=%+v err=%v", lease, err)
	}
	if err := s.Confirm(ctx, lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	report := checkReportAt(now)
	if err := s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal("initial fresh check receipt:", err)
	}
	var beforeTarget Target
	var beforeJob Job
	if err := s.db.First(&beforeTarget, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&beforeJob, "id = ?", lease.Job.ID).Error; err != nil {
		t.Fatal(err)
	}

	// A retry after the freshness window is still the same already-committed
	// receipt. It must not be reclassified as a new stale provider response.
	now = now.Add(providerMetadataMaxAge + time.Second)
	if err := s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatalf("exact receipt retry after metadata expiry: %v", err)
	}
	var afterTarget Target
	var afterJob Job
	if err := s.db.First(&afterTarget, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&afterJob, "id = ?", lease.Job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterJob.Status != beforeJob.Status || afterJob.Result != beforeJob.Result || afterTarget.Installed != beforeTarget.Installed ||
		afterTarget.Available != beforeTarget.Available || afterTarget.ReceiptID != beforeTarget.ReceiptID ||
		afterTarget.State != beforeTarget.State || afterTarget.CheckedAt == nil || beforeTarget.CheckedAt == nil ||
		!afterTarget.CheckedAt.Equal(*beforeTarget.CheckedAt) || !afterTarget.NextCheck.Equal(beforeTarget.NextCheck) {
		t.Fatalf("idempotent receipt retry changed accepted state: before target=%+v job=%+v; after target=%+v job=%+v", beforeTarget, beforeJob, afterTarget, afterJob)
	}

	changed := report
	changed.Available = "2026.9.2"
	if err := s.Complete(ctx, lease.Job.ID, "worker", lease.Token, changed); err == nil {
		t.Fatal("changed receipt was accepted as a retry")
	}
}

func TestAutomaticChecksContinueDailyAfterFastRetryLimitAndRecover(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("next_check", now.AddDate(20, 0, 0)).Error; err != nil {
		t.Fatal(err)
	}
	failed := Report{Evidence: strings.Repeat("0", 64), Outcome: "unavailable"}
	for attempt := 1; attempt <= maxAutomaticCheckFailures; attempt++ {
		if err := s.Schedule(context.Background()); err != nil {
			t.Fatal(err)
		}
		lease, err := s.Lease(context.Background(), "worker")
		if err != nil || lease == nil || lease.Job.Target != "gateway_core" {
			t.Fatalf("attempt %d did not lease the Gateway check: lease=%+v err=%v", attempt, lease, err)
		}
		if err = s.Confirm(context.Background(), lease.Job.ID, "worker", lease.Token); err != nil {
			t.Fatal(err)
		}
		now = now.Add(2 * time.Minute)
		if err = s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, failed); err != nil {
			t.Fatal(err)
		}
		var target Target
		if err = s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
			t.Fatal(err)
		}
		if !target.NextCheck.Equal(now.Add(checkRetryDelay(attempt))) {
			t.Fatalf("attempt %d scheduled the wrong retry delay: next=%s failures=%d", attempt, target.NextCheck, target.CheckFailures)
		}
		if attempt < maxAutomaticCheckFailures {
			now = target.NextCheck
		}
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckFailures != maxAutomaticCheckFailures || !strings.Contains(target.Reason, "continue daily") || !target.NextCheck.Equal(now.Add(checkInterval)) {
		t.Fatalf("daily recovery cadence was not durably recorded: failures=%d next=%s reason=%q", target.CheckFailures, target.NextCheck, target.Reason)
	}
	if err := s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "check", "pending").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("daily probe was queued before its due time: pending=%d err=%v", pending, err)
	}
	now = target.NextCheck
	if err := s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(context.Background(), "worker")
	if err != nil || lease == nil || lease.Job.Target != "gateway_core" {
		t.Fatalf("due daily read-only probe did not lease: lease=%+v err=%v", lease, err)
	}
	if err = s.Confirm(context.Background(), lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, failed); err != nil {
		t.Fatal(err)
	}
	if err = s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckFailures != maxAutomaticCheckFailures+1 || !target.NextCheck.Equal(now.Add(checkInterval)) {
		t.Fatalf("continued outage did not remain on daily read-only checks: failures=%d next=%s", target.CheckFailures, target.NextCheck)
	}
	now = target.NextCheck
	if err = s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	lease, err = s.Lease(context.Background(), "worker")
	if err != nil || lease == nil || lease.Job.Target != "gateway_core" {
		t.Fatalf("recovery probe did not lease: lease=%+v err=%v", lease, err)
	}
	if err = s.Confirm(context.Background(), lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, checkReportAt(now)); err != nil {
		t.Fatal(err)
	}
	if err = s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckFailures != 0 {
		t.Fatalf("successful daily recovery did not reset the failure streak: %d", target.CheckFailures)
	}
	if target.State != "update_available" {
		t.Fatalf("successful fresh recovery did not refresh version state: %+v", target)
	}
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "gateway_core", "apply", "pending").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("Gateway/core WSL install must remain blocked after recovery: pending=%d err=%v", pending, err)
	}
}

func TestApplyRejectsFutureDatedCheckEvidence(t *testing.T) {
	s := testService(t)
	completeCheckFor(t, s, "companion")
	if err := s.Policy(context.Background(), "companion", "auto_install_verified", "owner"); err != nil {
		t.Fatal(err)
	}
	future := s.now().Add(time.Minute)
	if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("checked_at", future).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), "companion", "2026.9.1"); err == nil {
		t.Fatal("future-dated check metadata authorized an update")
	}
}

func TestPolicyLedgerRetainsActor(t *testing.T) {
	s := testService(t)
	if err := s.Policy(context.Background(), "companion", "observe", "owner-one"); err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := s.db.Where("kind = ? AND target = ?", "policy", "companion").Order("created_at DESC").First(&j).Error; err != nil {
		t.Fatal(err)
	}
	var audit map[string]string
	if json.Unmarshal([]byte(j.Result), &audit) != nil || audit["owner"] != "owner-one" {
		t.Fatal("policy event lost its author")
	}
}

func TestFailedUpdateBlocksTasksUntilExplicitFreshReview(t *testing.T) {
	s := testService(t)
	l := applyLease(t, s)
	ctx := context.Background()
	if err := s.Permit(ctx, l.Job.ID, "worker", l.Token); err != nil {
		t.Fatal(err)
	}
	r := checkReport()
	r.Outcome = "update_failed"
	if err := s.Complete(ctx, l.Job.ID, "worker", l.Token, r); err != nil {
		t.Fatal(err)
	}
	var recoveryChecks, retries int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Count(&recoveryChecks).Error; err != nil || recoveryChecks != 1 {
		t.Fatalf("ambiguous update did not queue exactly one read-only recovery check: count=%d err=%v", recoveryChecks, err)
	}
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "companion", "apply", []string{"pending", "leased"}).Count(&retries).Error; err != nil || retries != 0 {
		t.Fatalf("ambiguous update was automatically replayed: active apply count=%d err=%v", retries, err)
	}
	if release, err := s.AcquireTask(ctx); err == nil {
		release()
		t.Fatal("failed update allowed task execution")
	}
	if err := s.Review(ctx, "companion", "owner"); err == nil {
		t.Fatal("review accepted without fresh check")
	}
	completeCheckFor(t, s, "companion")
	if release, err := s.AcquireTask(ctx); err == nil {
		release()
		t.Fatal("check silently cleared required review")
	}
	if err := s.Review(ctx, "companion", "owner"); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "update_available" || target.Reason != "" || target.Policy != "observe" {
		t.Fatalf("owner review did not restore fresh observed state safely: %+v", target)
	}
	if release, err := s.AcquireTask(ctx); err == nil {
		release()
		t.Fatal("owner review cleared the unresolved update-available admission state")
	} else if !errors.Is(err, ErrTaskAdmissionRequiresFreshCurrentChecks) {
		t.Fatalf("task admission returned an unexpected error after review: %v", err)
	}
	if err := s.Complete(ctx, l.Job.ID, "worker", l.Token, r); err != nil {
		t.Fatal("retry after review:", err)
	}
}

func TestVerifiedCurrentCompanionApplyAllowsImmediateTaskContinuation(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	completeCurrentCheckFor(t, s, "gateway_core")

	lease := applyLease(t, s)
	now = now.Add(time.Minute)
	report := checkReportAt(now)
	report.Installed = lease.Job.Version
	report.Available = lease.Job.Version
	report.HealthOK = true
	report.ProcessTreeStatus = "verified"
	if err := s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal("complete verified apply:", err)
	}

	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "current" || target.ReceiptID != lease.Job.ID || target.CheckedAt == nil || !target.CheckedAt.Equal(now) || !target.NextCheck.Equal(now.Add(checkInterval)) {
		t.Fatalf("verified current apply was not recorded as fresh maintenance evidence: %+v", target)
	}
	release, err := s.AcquireTask(context.Background())
	if err != nil {
		t.Fatalf("task remained blocked after a healthy verified upgrade: %v", err)
	}
	release()
}

func TestCurrentChecksRemainAdmissibleUntilTheDailyRefreshDeadline(t *testing.T) {
	s := testService(t)
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return checkedAt }
	for _, target := range []string{"gateway_core", "companion"} {
		completeCurrentCheckFor(t, s, target)
	}

	checkedAt = checkedAt.Add(providerMetadataMaxAge + time.Minute)
	release, err := s.AcquireTask(context.Background())
	if err != nil {
		t.Fatalf("fresh daily check was rejected only because its provider response aged past 15 minutes: %v", err)
	}
	release()

	checkedAt = checkedAt.Add(checkInterval - providerMetadataMaxAge - time.Minute)
	if release, err := s.AcquireTask(context.Background()); err == nil {
		release()
		t.Fatal("task admission continued after the daily check deadline")
	} else if !errors.Is(err, ErrTaskAdmissionRequiresFreshCurrentChecks) {
		t.Fatalf("daily check expiry returned an unexpected error: %v", err)
	}
}

func TestVerifiedUpdateMayUseFreshReceiptUntilDailyCheckDeadline(t *testing.T) {
	s := testService(t)
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return checkedAt }
	completeCheckFor(t, s, "companion")
	s.canSafelyInstall = func() bool { return true }
	if err := s.Policy(context.Background(), "companion", "auto_install_verified", "owner"); err != nil {
		t.Fatal(err)
	}

	checkedAt = checkedAt.Add(providerMetadataMaxAge + time.Minute)
	if err := s.Apply(context.Background(), "companion", "2026.9.1"); err != nil {
		t.Fatalf("verified update was rejected although its source metadata was fresh when checked and the daily receipt remains current: %v", err)
	}
}

func TestVerifiedApplyFindingNewerReleaseQueuesImmediateReadOnlyCheck(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	completeCurrentCheckFor(t, s, "gateway_core")

	lease := applyLease(t, s)
	now = now.Add(time.Minute)
	report := checkReportAt(now)
	report.Installed = lease.Job.Version
	report.Available = "2026.9.2"
	report.HealthOK = true
	report.ProcessTreeStatus = "verified"
	if err := s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal("complete verified apply with a newer release observed:", err)
	}

	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "update_available" || target.NextCheck.After(now) || target.CheckedAt != nil || target.ReceiptID != "" {
		t.Fatalf("newer release did not invalidate old check evidence and become due immediately: %+v", target)
	}
	var pendingChecks, activeApplies int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Count(&pendingChecks).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "companion", "apply", []string{"pending", "leased"}).Count(&activeApplies).Error; err != nil {
		t.Fatal(err)
	}
	if pendingChecks != 1 || activeApplies != 0 {
		t.Fatalf("post-update reconciliation queued checks=%d and active updates=%d; want one read-only check and no replay", pendingChecks, activeApplies)
	}
	if _, err := s.AcquireTask(context.Background()); !errors.Is(err, ErrTaskAdmissionRequiresFreshCurrentChecks) {
		t.Fatalf("task admission did not remain blocked until the newer release was reconciled: %v", err)
	}
}

func TestVerifiedApplyWithUnavailableReleaseMetadataQueuesReadOnlyCheck(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	completeCurrentCheckFor(t, s, "gateway_core")

	lease := applyLease(t, s)
	now = now.Add(time.Minute)
	report := checkReportAt(now)
	report.Installed = lease.Job.Version
	report.Available = ""
	report.HealthOK = true
	report.ProcessTreeStatus = "verified"
	if err := s.Complete(context.Background(), lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal("complete verified apply without release metadata:", err)
	}

	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "unknown" || target.CheckedAt != nil || target.ReceiptID != "" || target.NextCheck.After(now) {
		t.Fatalf("missing release metadata retained stale pre-install evidence: %+v", target)
	}
	var pendingChecks, activeApplies int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Count(&pendingChecks).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "companion", "apply", []string{"pending", "leased"}).Count(&activeApplies).Error; err != nil {
		t.Fatal(err)
	}
	if pendingChecks != 1 || activeApplies != 0 {
		t.Fatalf("unknown release metadata queued checks=%d and active updates=%d; want one read-only check and no replay", pendingChecks, activeApplies)
	}
	if _, err := s.AcquireTask(context.Background()); !errors.Is(err, ErrTaskAdmissionRequiresFreshCurrentChecks) {
		t.Fatalf("task admission was allowed without current release metadata: %v", err)
	}
}

func TestReceiptRetryDoesNotReprobeOrChangeHealthDecision(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	l := applyLease(t, s)
	ctx := context.Background()
	calls := 0
	s.SetHealthVerifier(func(context.Context) bool { calls++; return false })
	r := checkReportAt(now)
	r.Installed = l.Job.Version
	r.HealthOK = true
	r.ProcessTreeStatus = "verified"
	for i := 0; i < 2; i++ {
		if err := s.Complete(ctx, l.Job.ID, "worker", l.Token, r); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("health probed %d times for one receipt", calls)
	}
	now = now.Add(providerMetadataMaxAge + time.Second)
	if err := s.Complete(ctx, l.Job.ID, "worker", l.Token, r); err != nil {
		t.Fatalf("exact receipt retry after metadata expiry: %v", err)
	}
	if calls != 1 {
		t.Fatalf("exact receipt retry re-probed backend health %d times", calls)
	}
	if release, err := s.AcquireTask(ctx); err == nil {
		release()
		t.Fatal("backend health failure did not block work")
	}
	r.Available = "2026.9.2"
	if s.Complete(ctx, l.Job.ID, "worker", l.Token, r) == nil {
		t.Fatal("accepted changed retry")
	}
	if calls != 1 {
		t.Fatalf("changed terminal receipt triggered a health probe; calls=%d", calls)
	}
}

func TestExpiredApplyIsNotReplayed(t *testing.T) {
	s := testService(t)
	l := applyLease(t, s)
	now := time.Now().UTC().Add(21 * time.Minute)
	s.now = func() time.Time { return now }
	if _, err := s.Lease(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	var j Job
	s.db.First(&j, "id = ?", l.Job.ID)
	if j.Status != "needs_review" {
		t.Fatalf("expired update status %s", j.Status)
	}
	if release, err := s.AcquireTask(context.Background()); err == nil {
		release()
		t.Fatal("expired update allowed task execution")
	}
	var checks, applies int64
	s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "companion", "check", []string{"pending", "leased"}).Count(&checks)
	s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", "companion", "apply", []string{"pending", "leased"}).Count(&applies)
	if checks != 1 || applies != 0 {
		t.Fatalf("expired install recovery was unsafe: checks=%d active installs=%d", checks, applies)
	}
}

func TestStalePendingInstallerBecomesNeedsReviewAndRecoveryIsReadOnly(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Updates(map[string]any{
		"policy": "auto_install_verified", "state": "update_available", "installed": "2026.6.10", "available": "2026.9.1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	job := Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "apply", Version: "2026.9.1", Status: "pending", CreatedAt: now.Add(-stalePendingJobAfter - time.Second)}
	if err := s.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Overview(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&job, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "needs_review" || job.FinishedAt == nil {
		t.Fatalf("stale pending installer status=%s finished=%v, want terminal needs_review", job.Status, job.FinishedAt)
	}
	var activeApply, pendingCheck int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status IN ?", job.Target, "apply", []string{"pending", "leased"}).Count(&activeApply).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", job.Target, "check", "pending").Count(&pendingCheck).Error; err != nil {
		t.Fatal(err)
	}
	if activeApply != 0 || pendingCheck != 1 {
		t.Fatalf("stale installer recovery created active apply=%d and pending checks=%d", activeApply, pendingCheck)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", job.Target).Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "needs_review" || target.Policy != "observe" {
		t.Fatalf("stale installer did not preserve review and safe policy state: %+v", target)
	}
}

func TestStalePendingCheckGetsBoundedRetryAndDoesNotDuplicate(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	job := Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "check", Status: "pending", CreatedAt: now.Add(-stalePendingJobAfter - time.Second)}
	if err := s.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stored Job
	if err := s.db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.FinishedAt == nil {
		t.Fatalf("stale read-only check status=%s finished=%v, want terminal failed", stored.Status, stored.FinishedAt)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", job.Target).Error; err != nil {
		t.Fatal(err)
	}
	if target.CheckFailures != 1 || !target.NextCheck.Equal(now.Add(checkRetryInterval)) {
		t.Fatalf("stale check did not schedule bounded retry: failures=%d next=%s", target.CheckFailures, target.NextCheck)
	}
	var pending int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", job.Target, "check", "pending").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("stale check was immediately duplicated before its retry deadline: %d pending", pending)
	}
}

func TestLeaseCancelsUnsupportedPendingInstallerWithoutStartingIt(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	s.canSafelyInstall = func() bool { return true }
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	job := Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "apply", Version: "2026.9.1", Status: "pending", CreatedAt: now}
	if err := s.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(context.Background(), "worker")
	if err != nil || lease != nil {
		t.Fatalf("unsupported installer was leased: lease=%+v err=%v", lease, err)
	}
	if err := s.db.First(&job, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "cancelled" || !strings.Contains(job.Result, "gateway_core_wsl_containment_unverified") {
		t.Fatalf("unsupported pending installer was not terminally blocked: status=%s result=%s", job.Status, job.Result)
	}
}

func TestExpiredReadOnlyCheckSchedulesRetryWithoutLosingConfirmedState(t *testing.T) {
	s := testService(t)
	completeCheck(t, s)
	var before Target
	if err := s.db.First(&before, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), "gateway_core"); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lease(context.Background(), "worker")
	if err != nil || l == nil {
		t.Fatalf("lease: %v", err)
	}
	now := time.Now().UTC().Add(21 * time.Minute).Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	if _, err = s.Lease(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	var target Target
	if err = s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.Installed != before.Installed || target.Available != before.Available || target.ReceiptID != before.ReceiptID || target.CheckedAt == nil || !target.CheckedAt.Equal(*before.CheckedAt) {
		t.Fatal("expired read-only check changed the last confirmed evidence")
	}
	if target.State != "check_failed" || !target.NextCheck.Equal(now.Add(checkRetryInterval)) {
		t.Fatalf("expired check was not given a bounded retry: state=%s next=%s", target.State, target.NextCheck)
	}
}

func TestAcquireTaskRequiresFreshCurrentMaintenance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Service, time.Time)
		admit  bool
	}{
		{name: "fresh current checks admit", admit: true},
		{
			name: "missing receipt denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("receipt_id", uuid.NewString()).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "stale receipt denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
					t.Fatal(err)
				}
				var receipt Job
				if err := s.db.First(&receipt, "id = ?", target.ReceiptID).Error; err != nil {
					t.Fatal(err)
				}
				stale := now.Add(-checkInterval)
				target.CheckedAt, receipt.StartedAt, receipt.FinishedAt = &stale, &stale, &stale
				receipt.CreatedAt = stale
				if err := s.db.Save(&receipt).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.db.Save(&target).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "stale provider metadata denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
					t.Fatal(err)
				}
				var receipt Job
				if err := s.db.First(&receipt, "id = ?", target.ReceiptID).Error; err != nil {
					t.Fatal(err)
				}
				var report Report
				if err := json.Unmarshal([]byte(receipt.Result), &report); err != nil {
					t.Fatal(err)
				}
				stale := now.Add(-providerMetadataMaxAge - time.Second)
				report.ProviderMetadataAt = &stale
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.db.Model(&Job{}).Where("id = ?", receipt.ID).Update("result", string(encoded)).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "future receipt denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
					t.Fatal(err)
				}
				var receipt Job
				if err := s.db.First(&receipt, "id = ?", target.ReceiptID).Error; err != nil {
					t.Fatal(err)
				}
				future := now.Add(time.Second)
				target.CheckedAt, receipt.FinishedAt = &future, &future
				if err := s.db.Save(&receipt).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.db.Save(&target).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid receipt denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
					t.Fatal(err)
				}
				report := checkReport()
				report.Installed, report.Available = "2026.9.1", "2026.9.1"
				report.Evidence = "invalid"
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.db.Model(&Job{}).Where("id = ?", target.ReceiptID).Update("result", string(encoded)).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "due deadline denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("next_check", now).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing deadline denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Update("next_check", time.Time{}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "update available denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
					t.Fatal(err)
				}
				report := checkReport()
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.db.Model(&Job{}).Where("id = ?", target.ReceiptID).Update("result", string(encoded)).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.db.Model(&Target{}).Where("id = ?", target.ID).Updates(map[string]any{
					"installed": report.Installed, "available": report.Available, "state": "update_available",
				}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unknown state denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("state", "unknown").Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "failed state denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Updates(map[string]any{"state": "check_failed", "check_failures": 1}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "installing state denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "companion").Update("state", "installing").Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "review state denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				if err := s.db.Model(&Target{}).Where("id = ?", "gateway_core").Update("state", "needs_review").Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "failed receipt denies",
			mutate: func(t *testing.T, s *Service, _ time.Time) {
				var target Target
				if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
					t.Fatal(err)
				}
				if err := s.db.Model(&Job{}).Where("id = ?", target.ReceiptID).Update("status", "failed").Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "pending maintenance denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				if err := s.db.Create(&Job{ID: uuid.NewString(), Target: "companion", Kind: "apply", Version: "2026.9.2", Status: "pending", CreatedAt: now}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "leased maintenance denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				if err := s.db.Create(&Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "check", Status: "leased", CreatedAt: now}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unresolved review job denies",
			mutate: func(t *testing.T, s *Service, now time.Time) {
				finished := now
				if err := s.db.Create(&Job{ID: uuid.NewString(), Target: "companion", Kind: "apply", Status: "needs_review", CreatedAt: now, FinishedAt: &finished}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testService(t)
			now := prepareFreshCurrentChecks(t, s)
			if tt.mutate != nil {
				tt.mutate(t, s, now)
			}
			release, err := s.AcquireTask(context.Background())
			if tt.admit {
				if err != nil {
					t.Fatal(err)
				}
				release()
				return
			}
			if err == nil {
				release()
				t.Fatal("admitted delegated work without fresh, current maintenance evidence")
			}
			if !errors.Is(err, ErrTaskAdmissionRequiresFreshCurrentChecks) {
				t.Fatalf("admission error is not the actionable fail-closed error: %v", err)
			}
		})
	}
}

func TestRevocationStopsPermitAndPendingUpdates(t *testing.T) {
	s := testService(t)
	l := applyLease(t, s)
	ctx := context.Background()
	if err := s.Policy(ctx, "companion", "observe", "owner"); err != nil {
		t.Fatal(err)
	}
	if s.Permit(ctx, l.Job.ID, "worker", l.Token) == nil {
		t.Fatal("revoked policy still permitted installation")
	}
}

func TestAdmissionLockPreventsConcurrentUpdateLease(t *testing.T) {
	s := testService(t)
	prepareFreshCurrentChecks(t, s)
	ctx := context.Background()
	release, err := s.AcquireTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err = s.Lease(bounded, "worker"); err == nil {
		t.Fatal("maintenance bypassed task admission lock")
	}
}

func TestGatewayInstallStaysBlockedDuringCompanionUpdate(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	companionLease := applyLease(t, s)
	if companionLease.Job.Target != "companion" || companionLease.Job.Kind != "apply" {
		t.Fatalf("expected Companion apply lease, got %+v", companionLease.Job)
	}
	if err := s.Policy(ctx, "gateway_core", "auto_install_verified", "owner"); !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("Gateway policy error = %v, want permanent WSL blocker", err)
	}
	if err := s.Apply(ctx, "gateway_core", "2026.9.1"); !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("Gateway apply error = %v, want permanent WSL blocker", err)
	}
	var gatewayApplies int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ?", "gateway_core", "apply").Count(&gatewayApplies).Error; err != nil || gatewayApplies != 0 {
		t.Fatalf("Gateway install job count=%d err=%v", gatewayApplies, err)
	}
}
