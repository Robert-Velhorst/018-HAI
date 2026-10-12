package ambient

import "testing"

import "time"

func TestLegacyFallbackRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "")
	if legacyFallbackEnabled() {
		t.Fatal("legacy fallback must be disabled by default")
	}
	t.Setenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED", "1")
	if !legacyFallbackEnabled() {
		t.Fatal("legacy fallback should accept explicit opt-in")
	}
}

func TestAmbientDurableWorkerDefaultsToOneMinuteIdlePolling(t *testing.T) {
	t.Setenv("AMBIENT_WORKER_POLL_SECONDS", "")
	if interval := ambientPollInterval(); interval != time.Minute {
		t.Fatalf("durable ambient poll interval = %s, want 1m", interval)
	}
}
