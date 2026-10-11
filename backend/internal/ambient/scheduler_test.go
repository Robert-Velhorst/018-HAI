package ambient

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/schedulerstatus"
)

func TestStartSchedulerReportsPolicyDisabledState(t *testing.T) {
	t.Setenv("AMBIENT_SCHEDULER_ENABLED", "false")
	StartScheduler(context.Background(), nil)
	state, ok := schedulerstatus.Snapshot("ambient")
	if !ok || state.Enabled || state.Running {
		t.Fatalf("disabled scheduler state = %+v, found=%t", state, ok)
	}
}

type schedulerScanRecorder struct {
	Service
	calls int
}

func (s *schedulerScanRecorder) Scan(string) (*models.AmbientScan, error) {
	s.calls++
	return &models.AmbientScan{}, nil
}

func TestLegacySchedulerSkipsStartupScanAfterCancellation(t *testing.T) {
	t.Setenv("AMBIENT_RUN_ON_STARTUP", "true")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := &schedulerScanRecorder{}
	(&Scheduler{service: service, backgroundAllowed: func() bool { return true }}).Start(ctx, time.Hour)

	if service.calls != 0 {
		t.Fatalf("startup scans = %d after context cancellation, want 0", service.calls)
	}
}
