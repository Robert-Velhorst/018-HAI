package openclawreconcile

import (
	"testing"
	"time"
)

func TestSchedulerUsesDelegationOptInAndBoundsInvalidValues(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_ENABLED", "")
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_SECONDS", "1")
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_POLL_SECONDS", "99999")
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_BATCH", "1000")
	if !SchedulerEnabled() || interval() != 30*time.Second || pollInterval() != 10*time.Second || batchLimit() != 100 {
		t.Fatalf("unsafe scheduler state: enabled=%t interval=%s poll=%s batch=%d", SchedulerEnabled(), interval(), pollInterval(), batchLimit())
	}
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_ENABLED", "false")
	if SchedulerEnabled() {
		t.Fatal("explicit reconciliation disable was ignored")
	}
}

func TestStaleAfterUsesSafeBounds(t *testing.T) {
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_STALE_SECONDS", "10")
	if staleAfter() != 15*time.Minute {
		t.Fatalf("too-small stale threshold = %s", staleAfter())
	}
	t.Setenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_STALE_SECONDS", "1200")
	if staleAfter() != 20*time.Minute {
		t.Fatalf("configured stale threshold = %s", staleAfter())
	}
}
