package openclawmaintenance

import (
	"context"
	"strings"
	"testing"
)

func TestInstallationReasonDoesNotBlockReadyCompanion(t *testing.T) {
	s := NewService(nil)
	s.canSafelyInstall = func() bool { return true }
	if reason := s.installationReason("companion"); reason != "" {
		t.Fatalf("ready Companion must not expose a blocking reason: %q", reason)
	}
	if reason := s.installationReason("gateway_core"); reason == "" {
		t.Fatal("Windows containment must not clear the Gateway/core WSL blocker")
	}
	s.canSafelyInstall = func() bool { return false }
	s.installCapabilityReason = func(string) string { return "Authenticated worker capability expired." }
	if reason := s.installationReason("companion"); reason != "Authenticated worker capability expired." {
		t.Fatalf("expected current capability blocker, got %q", reason)
	}
	s.canSafelyInstall = nil
	s.installCapabilityReason = nil
	if reason := s.installationReason("companion"); reason == "" {
		t.Fatal("missing capability verifier must remain blocked")
	}
}

func TestOverviewReadyCompanionHasNoBlockingReason(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	completeCheckFor(t, s, "companion")
	targets, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ID == "companion" {
			if target.InstallStatus != "capability_ready" || target.InstallBlockedReason != "" {
				t.Fatalf("ready Companion must not be projected as blocked: %+v", target)
			}
			return
		}
	}
	t.Fatal("Companion target missing")
}

func TestOverviewMissingPublisherBlocksEvenWithContainment(t *testing.T) {
	s := testServiceWithDefaultPolicy(t)
	ctx := context.Background()
	if err := s.Check(ctx, "companion"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(ctx, "worker")
	if err != nil || lease == nil {
		t.Fatalf("lease: %v", err)
	}
	if err := s.Confirm(ctx, lease.Job.ID, "worker", lease.Token); err != nil {
		t.Fatal(err)
	}
	report := checkReportAt(s.now())
	report.PublisherVerified = false
	report.PublisherPinStatus = "missing"
	if err := s.Complete(ctx, lease.Job.ID, "worker", lease.Token, report); err != nil {
		t.Fatal(err)
	}
	targets, err := s.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ID == "companion" {
			if target.InstallStatus != "blocked" || !strings.Contains(target.InstallBlockedReason, "THUMBPRINT is not configured") {
				t.Fatalf("missing publisher pin must remain a structured blocker: %+v", target)
			}
			return
		}
	}
	t.Fatal("Companion target missing")
}
