package hostruntimereconcile

import (
	"testing"
	"time"
)

func TestSchedulerBoundsInvalidEnvironment(t *testing.T) {
	t.Setenv("HAI_HOST_RUNTIME_RECONCILIATION_SECONDS", "1")
	t.Setenv("HAI_HOST_RUNTIME_RECONCILIATION_POLL_SECONDS", "99999")
	t.Setenv("HAI_HOST_RUNTIME_RECONCILIATION_BATCH", "1000")
	if interval() != 30*time.Second || pollInterval() != 10*time.Second || batchLimit() != 100 {
		t.Fatalf("unsafe scheduler values were accepted: interval=%s poll=%s batch=%d", interval(), pollInterval(), batchLimit())
	}
}
