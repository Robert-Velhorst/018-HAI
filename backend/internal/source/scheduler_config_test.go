package source

import "testing"

import "time"

func TestLegacyFallbackRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "")
	if legacyFallbackEnabled() {
		t.Fatal("legacy fallback must be disabled by default")
	}
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "true")
	if !legacyFallbackEnabled() {
		t.Fatal("legacy fallback should accept explicit opt-in")
	}
}

func TestSourceDurableWorkerDefaultsToOneMinuteIdlePolling(t *testing.T) {
	t.Setenv("SOURCE_WORKER_POLL_SECONDS", "")
	if interval := durablePollInterval(); interval != time.Minute {
		t.Fatalf("durable source poll interval = %s, want 1m", interval)
	}
}
