package source

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestSourceSchedulerIntervalUsesSafeBounds(t *testing.T) {
	for _, value := range []string{"", "1", "14", "86401", "invalid"} {
		t.Setenv("SOURCE_SCHEDULER_INTERVAL_SECONDS", value)
		if got := schedulerInterval(); got != defaultSchedulerInterval {
			t.Fatalf("schedulerInterval() with %q = %s, want default %s", value, got, defaultSchedulerInterval)
		}
	}
	t.Setenv("SOURCE_SCHEDULER_INTERVAL_SECONDS", "15")
	if got := schedulerInterval(); got != minSchedulerInterval {
		t.Fatalf("schedulerInterval() = %s, want %s", got, minSchedulerInterval)
	}
	t.Setenv("SOURCE_SCHEDULER_INTERVAL_SECONDS", "86400")
	if got := schedulerInterval(); got != maxSchedulerInterval {
		t.Fatalf("schedulerInterval() = %s, want %s", got, maxSchedulerInterval)
	}
}

func configureSchedulerTest(t *testing.T, durable, fallback string) {
	t.Helper()
	t.Setenv("SOURCE_SCHEDULER_ENABLED", "true")
	t.Setenv("SOURCE_SCHEDULER_DURABLE", durable)
	t.Setenv("SOURCE_SCHEDULER_VOLATILE_FALLBACK", fallback)
	t.Setenv("SOURCE_MANUAL_WORKER_ENABLED", "false")
	t.Setenv("SOURCE_SCHEDULER_RUN_ON_STARTUP", "false")
}

func TestStartSchedulerFailsClosedWhenDurableStartupFails(t *testing.T) {
	configureSchedulerTest(t, "true", "")
	durableErr := errors.New("queue unavailable")
	volatileStarts := 0
	err := startScheduler(
		context.Background(), nil,
		func(context.Context, Service, time.Duration, ...func() bool) error { return durableErr },
		func(context.Context, Service, time.Duration, ...func() bool) bool { volatileStarts++; return true },
		func(context.Context, Service, ...func() bool) error { return nil },
	)
	if !errors.Is(err, durableErr) {
		t.Fatalf("startScheduler() error = %v, want wrapped durable startup error", err)
	}
	if volatileStarts != 0 {
		t.Fatalf("volatile fallback started %d times without explicit opt-in", volatileStarts)
	}
}

func TestStartSchedulerUsesOnlyExplicitlyEnabledVolatileFallback(t *testing.T) {
	configureSchedulerTest(t, "true", "true")
	durableErr := errors.New("queue unavailable")
	volatileStarts := 0
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	err := startScheduler(
		context.Background(), nil,
		func(context.Context, Service, time.Duration, ...func() bool) error { return durableErr },
		func(context.Context, Service, time.Duration, ...func() bool) bool { volatileStarts++; return true },
		func(context.Context, Service, ...func() bool) error { return nil },
	)
	if err != nil {
		t.Fatalf("startScheduler() error = %v, want explicit fallback to start", err)
	}
	if volatileStarts != 1 {
		t.Fatalf("volatile fallback started %d times, want exactly once", volatileStarts)
	}
	if !strings.Contains(logs.String(), "DEGRADED") || !strings.Contains(logs.String(), "not crash-recoverable") {
		t.Fatalf("fallback log does not expose degraded mode: %q", logs.String())
	}
}

func TestStartSchedulerKeepsSuccessfulDurableStartup(t *testing.T) {
	configureSchedulerTest(t, "true", "false")
	durableStarts, volatileStarts := 0, 0
	err := startScheduler(
		context.Background(), nil,
		func(context.Context, Service, time.Duration, ...func() bool) error { durableStarts++; return nil },
		func(context.Context, Service, time.Duration, ...func() bool) bool { volatileStarts++; return true },
		func(context.Context, Service, ...func() bool) error { return nil },
	)
	if err != nil {
		t.Fatalf("startScheduler() error = %v", err)
	}
	if durableStarts != 1 || volatileStarts != 0 {
		t.Fatalf("durable starts=%d, volatile starts=%d; want 1 and 0", durableStarts, volatileStarts)
	}
}

func TestStartSchedulerCancellationSkipsStartupAndCleansUpVolatileWorker(t *testing.T) {
	configureSchedulerTest(t, "true", "true")
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	starts := 0
	err := startScheduler(
		canceledCtx, nil,
		func(context.Context, Service, time.Duration, ...func() bool) error { starts++; return nil },
		func(context.Context, Service, time.Duration, ...func() bool) bool { starts++; return true },
		func(context.Context, Service, ...func() bool) error { starts++; return nil },
	)
	if err != nil || starts != 0 {
		t.Fatalf("canceled startup = (%v), starts=%d; want nil and no workers", err, starts)
	}

	group := lifecycle.New(context.Background())
	ctx := group.Context()
	durableErr := errors.New("queue unavailable")
	err = startScheduler(
		ctx, nil,
		func(context.Context, Service, time.Duration, ...func() bool) error { return durableErr },
		startVolatileScheduler,
		func(context.Context, Service, ...func() bool) error { return nil },
	)
	if err != nil {
		t.Fatalf("startScheduler() with explicit fallback error = %v", err)
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	if err := group.StopAndWait(cleanupCtx); err != nil {
		t.Fatalf("volatile scheduler did not stop with its lifecycle: %v", err)
	}
}
