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
// mode requires an explicit AMBIENT_SCHEDULER_DURABLE=false operator opt-out.
func StartScheduler(ctx context.Context, service Service, allowed ...func() bool) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	policy := policyFromEnv()
	if !policy.SchedulerEnabled {
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
				return
			}
			log.Printf("ambient scheduler not started: durable queue/review persistence unavailable (%s)", safety.RedactSecrets(err.Error()))
			return
		} else {
			return
		}
	}
	scheduler := &Scheduler{service: service, backgroundAllowed: backgroundAllowed}
	lifecycle.Go(ctx, "ambient-scheduler", func() { scheduler.Start(ctx, interval) })
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
