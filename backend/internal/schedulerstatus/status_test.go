package schedulerstatus

import (
	"context"
	"strings"
	"testing"
)

func TestProbeReportsStoppedDurableScheduler(t *testing.T) {
	Record(State{Name: "unit-stopped", Enabled: true, Durable: true, Running: false, Detail: "queue unavailable"})
	err := Probe("unit-stopped").Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not running") || !strings.Contains(err.Error(), "queue unavailable") {
		t.Fatalf("probe error = %v, want stopped scheduler detail", err)
	}
}

func TestProbeAcceptsDurableRunningScheduler(t *testing.T) {
	Record(State{Name: "unit-running", Enabled: true, Durable: true, Running: true})
	if err := Probe("unit-running").Run(context.Background()); err != nil {
		t.Fatalf("durable running scheduler should be healthy: %v", err)
	}
}
