package ambient

import (
	"context"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/schedulerstatus"
)

type Scheduler struct {
	service           Service
	backgroundAllowed func() bool
	running           atomic.Bool
	reviewHeld        atomic.Bool
}

// StartScheduler starts ambient scanning.
//
// Durable startup fails closed: a queue failure must not bypass a persistent
// review hold by silently switching to the context-only ticker. Legacy ticker
// mode requires both an explicit durable-scheduler opt-out and a global
// legacy-fallback opt-in.
func StartScheduler(ctx context.Context, service Service, allowed ...func() bool) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	policy := policyFromEnv()
	if !policy.SchedulerEnabled {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Detail: "disabled by ambient scheduler policy"})
		return
	}
	interval := time.Duration(policy.ScanIntervalSeconds) * time.Second
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if interval < 30*time.Second {
		interval = 5 * time.Minute
	}
	if durableSchedulerEnabled() {
		if err := startDurableScheduler(ctx, service, interval, backgroundAllowed); err != nil {
			if ctx.Err() != nil {
				schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Durable: true, Detail: "startup canceled before durable worker attached"})
				return
			}
			schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Durable: true, Detail: "durable worker unavailable; scheduler not started: " + safety.RedactSecrets(err.Error())})
			log.Printf("ambient scheduler not started: durable queue/review persistence unavailable (%s)", safety.RedactSecrets(err.Error()))
			return
		} else {
			schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Durable: true, Running: true, Detail: "durable worker attached"})
			return
		}
	}
	if !legacyFallbackEnabled() {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Detail: "legacy scheduler disabled; set DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED=true to explicitly opt in"})
		return
	}
	scheduler := &Scheduler{service: service, backgroundAllowed: backgroundAllowed}
	if lifecycle.Go(ctx, "ambient-scheduler", func() { scheduler.Start(ctx, interval) }) {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Running: true, Detail: "explicitly configured in-process scheduler; not crash-recoverable"})
	} else {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Detail: "lifecycle rejected worker during shutdown"})
	}
}

func (s *Scheduler) Start(ctx context.Context, interval time.Duration) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if interval < 30*time.Second {
		interval = 5 * time.Minute
	}
	if runOnStartup() {
		s.runOnce(ctx)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			s.runOnce(ctx)
		}
	}
}

func runOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AMBIENT_RUN_ON_STARTUP")))
	return value == "true" || value == "1" || value == "yes"
}

func (s *Scheduler) runOnce(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil || s.reviewHeld.Load() || s.service == nil || s.backgroundAllowed == nil || !s.backgroundAllowed() || !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	if err := runAmbientScan(ctx, s.service, s.backgroundAllowed); err != nil {
		if durablejob.RequiresManualReview(err) {
			s.reviewHeld.Store(true)
		}
		log.Printf("ambient scan failed: %s", safety.RedactSecrets(err.Error()))
	}
}

func schedulerBackgroundGate(allowed []func() bool) func() bool {
	if len(allowed) > 0 && allowed[0] != nil {
		return allowed[0]
	}
	return func() bool { return false }
}
