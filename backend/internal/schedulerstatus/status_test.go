package schedulerstatus

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProbeReportsUnavailableAndDegradedSchedulers(t *testing.T) {
	if err := Probe("not-recorded").Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not reported") {
		t.Fatalf("missing-state probe error = %v", err)
	}

	Record(State{Name: "test-disabled", Detail: "disabled by test"})
	if err := Probe("test-disabled").Run(context.Background()); err == nil || !strings.Contains(err.Error(), "disabled by test") {
		t.Fatalf("disabled-state probe error = %v", err)
	}

	Record(State{Name: "test-degraded", Enabled: true, Running: true, Detail: "volatile fallback"})
	if err := Probe("test-degraded").Run(context.Background()); err == nil || !strings.Contains(err.Error(), "without durable") {
		t.Fatalf("degraded-state probe error = %v", err)
	}
}

func TestProbeAcceptsDurableRunningScheduler(t *testing.T) {
	Record(State{Name: "test-durable", Enabled: true, Durable: true, Running: true})
	if err := Probe("test-durable").Run(context.Background()); err != nil {
		t.Fatalf("durable running scheduler should be healthy: %v", err)
	}
}

func TestProbeReportsStoppedDurableScheduler(t *testing.T) {
	Record(State{Name: "unit-stopped", Enabled: true, Durable: true, Running: false, Detail: "queue unavailable"})
	err := Probe("unit-stopped").Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not running") || !strings.Contains(err.Error(), "queue unavailable") {
		t.Fatalf("probe error = %v, want stopped scheduler detail", err)
	}
}

func TestRecordNormalizesAndTimestampsState(t *testing.T) {
	Record(State{Name: " test-normalized ", Detail: " detail "})
	state, ok := Snapshot("test-normalized")
	if !ok || state.Name != "test-normalized" || state.Detail != "detail" || state.UpdatedAt.IsZero() || state.UpdatedAt.Location() != time.UTC {
		t.Fatalf("recorded state = %+v, found=%t", state, ok)
	}
}
