package workflow

import "testing"

import "time"

func TestLegacyFallbackRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "")
	if legacyFallbackEnabled() {
		t.Fatal("legacy fallback must be disabled by default")
	}
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "yes")
	if !legacyFallbackEnabled() {
		t.Fatal("legacy fallback should accept explicit opt-in")
	}
}

func TestWorkflowDurableWorkerDefaultsToOneMinuteIdlePolling(t *testing.T) {
	t.Setenv("WORKFLOW_WORKER_POLL_SECONDS", "")
	if interval := workflowPollInterval(); interval != time.Minute {
		t.Fatalf("durable workflow poll interval = %s, want 1m", interval)
	}
}
