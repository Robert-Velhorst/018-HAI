package ambient

import (
	"context"
	"testing"

	"automation-hub-backend/internal/models"
)

type ambientSchedulerScanSpy struct {
	Service
	scans int
}

func (s *ambientSchedulerScanSpy) Scan(string) (*models.AmbientScan, error) {
	s.scans++
	return &models.AmbientScan{}, nil
}

func (s *ambientSchedulerScanSpy) ScanContext(ctx context.Context, _ string, allowed ...func() bool) (*models.AmbientScan, error) {
	if err := scanCheckpoint(ctx, schedulerBackgroundGate(allowed)); err != nil {
		return nil, err
	}
	s.scans++
	return &models.AmbientScan{Status: "completed"}, nil
}

func TestSchedulerFailsClosedWithoutSafetyGate(t *testing.T) {
	if schedulerBackgroundGate(nil)() {
		t.Fatal("missing background safety gate allowed ambient scans")
	}

	spy := &ambientSchedulerScanSpy{}
	(&Scheduler{service: spy}).runOnce(context.Background())
	if spy.scans != 0 {
		t.Fatalf("ambient scans = %d without an explicit safety gate, want 0", spy.scans)
	}

	(&Scheduler{service: spy, backgroundAllowed: schedulerBackgroundGate([]func() bool{nil})}).runOnce(context.Background())
	if spy.scans != 0 {
		t.Fatalf("ambient scans = %d with a nil safety callback, want 0", spy.scans)
	}
}

func TestSchedulerRechecksCurrentSafetyGateBeforeEachScan(t *testing.T) {
	allowed := true
	spy := &ambientSchedulerScanSpy{}
	scheduler := &Scheduler{
		service: spy,
		backgroundAllowed: schedulerBackgroundGate([]func() bool{func() bool {
			return allowed
		}}),
	}

	scheduler.runOnce(context.Background())
	if spy.scans != 1 {
		t.Fatalf("ambient scans = %d with safety gate enabled, want 1", spy.scans)
	}

	allowed = false
	scheduler.runOnce(context.Background())
	if spy.scans != 1 {
		t.Fatalf("ambient scans = %d after safety policy withdrawal, want 1", spy.scans)
	}
}

func TestAmbientPollIntervalUsesSafeBounds(t *testing.T) {
	for _, value := range []string{"", "1", "14", "3601", "invalid"} {
		t.Setenv("AMBIENT_WORKER_POLL_SECONDS", value)
		if got := ambientPollInterval(); got != defaultAmbientPoll {
			t.Fatalf("ambientPollInterval() with %q = %s, want default %s", value, got, defaultAmbientPoll)
		}
	}
	t.Setenv("AMBIENT_WORKER_POLL_SECONDS", "15")
	if got := ambientPollInterval(); got != minAmbientPollInterval {
		t.Fatalf("ambientPollInterval() = %s, want %s", got, minAmbientPollInterval)
	}
	t.Setenv("AMBIENT_WORKER_POLL_SECONDS", "3600")
	if got := ambientPollInterval(); got != maxAmbientPollInterval {
		t.Fatalf("ambientPollInterval() = %s, want %s", got, maxAmbientPollInterval)
	}
}
