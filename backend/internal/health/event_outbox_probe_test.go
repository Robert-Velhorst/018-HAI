package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/events"
)

func TestEventOutboxProbeReportsDeliveryHealth(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	recent, boundary, stale := now.Add(-time.Minute), now.Add(-2*time.Minute), now.Add(-3*time.Minute)
	readFailure := errors.New("statistics store unavailable")
	for _, test := range []struct {
		name      string
		stats     events.OutboxStats
		loadError error
		want      string
	}{
		{name: "empty"},
		{name: "recent pending", stats: events.OutboxStats{Pending: 1, OldestPendingAt: &recent}},
		{name: "warning boundary", stats: events.OutboxStats{Pending: 1, OldestPendingAt: &boundary}},
		{name: "aged pending", stats: events.OutboxStats{Pending: 3, OldestPendingAt: &stale}, want: "3 event deliveries are pending; oldest is 3m0s old"},
		{name: "unknown age", stats: events.OutboxStats{Pending: 1}, want: "delivery age is unavailable"},
		{name: "dead letters", stats: events.OutboxStats{DeadLettered: 2, Pending: 3, OldestPendingAt: &stale}, want: "2 event deliveries are dead-lettered"},
		{name: "read failure", loadError: readFailure, want: readFailure.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), outboxProbeContextKey{}, "request")
			calls := 0
			probe := eventOutboxProbe(func(got context.Context) (events.OutboxStats, error) {
				calls++
				if got != ctx {
					t.Fatal("outbox read did not receive the probe context")
				}
				return test.stats, test.loadError
			}, func() time.Time { return now })
			err := probe.Run(ctx)
			if test.want == "" && err != nil {
				t.Fatalf("healthy outbox: %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if test.loadError != nil && !errors.Is(err, test.loadError) {
				t.Fatal("lost statistics read failure")
			}
			if calls != 1 {
				t.Fatalf("outbox reads = %d, want one", calls)
			}
		})
	}
}

type outboxProbeContextKey struct{}

func TestEventOutboxProbeRejectsCancellationBeforeDatabaseAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EventOutboxProbe().Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation before acquiring a database", err)
	}
}

func TestEventOutboxProbeDoesNotReportHealthyAfterCancelledRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	probe := eventOutboxProbe(func(context.Context) (events.OutboxStats, error) {
		cancel()
		return events.OutboxStats{}, nil
	}, time.Now)
	if err := probe.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v", err)
	}
}

func TestEventOutboxProbeIsWiredAsNonCriticalReadinessWarning(t *testing.T) {
	count := 0
	for _, probe := range Probes(config.Configuration{}) {
		if probe.Name == "events.outbox" {
			count++
			if probe.Critical {
				t.Fatal("delivery degradation must not make core routes unavailable")
			}
		}
	}
	if count != 1 {
		t.Fatalf("registered outbox probes = %d, want one", count)
	}
	probe := eventOutboxProbe(func(context.Context) (events.OutboxStats, error) {
		return events.OutboxStats{DeadLettered: 2}, nil
	}, time.Now)
	checks := doctor.RunProbes(context.Background(), time.Second, []doctor.Probe{probe})
	if len(checks) != 1 || checks[0].Severity != doctor.SeverityWarn || !strings.Contains(checks[0].Detail, "dead-lettered") {
		t.Fatalf("readiness did not expose delivery degradation: %#v", checks)
	}
	if (doctor.Report{Checks: checks}).HasFailures() {
		t.Fatal("outbox degradation marked the application unavailable")
	}
}
