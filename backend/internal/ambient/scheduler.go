package ambient

import (
	"context"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/schedulerstatus"
)

type Scheduler struct {
	service           Service
	backgroundAllowed func() bool
	running           atomic.Bool
}

// StartScheduler starts ambient scanning.
//
// It prefers the durable path (persisted, retried, crash-recovered — see
// durable_scheduler.go). A failed durable queue stops the scheduler by default
// rather than silently using a non-recoverable ticker. The fallback is only
// available when explicitly enabled for local development.
func StartScheduler(ctx context.Context, service Service, allowed ...func() bool) {
	policy := policyFromEnv()
	if !policy.SchedulerEnabled {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Detail: "disabled by AMBIENT_SCHEDULER_ENABLED"})
		return
	}
	interval := time.Duration(policy.ScanIntervalSeconds) * time.Second
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if interval < 30*time.Second {
		interval = 5 * time.Minute
	}
	if durableSchedulerEnabled() {
		if err := startDurableScheduler(ctx, service, interval, backgroundAllowed); err != nil {
			if !legacyFallbackEnabled() {
				schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Durable: true, Detail: "durable queue unavailable: " + err.Error()})
				log.Printf("ambient scheduler: durable queue unavailable (%v); scheduler not started; set DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED=true only for local development", err)
				return
			}
			schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Detail: "durable queue unavailable; explicitly enabled legacy fallback: " + err.Error()})
			log.Printf("ambient scheduler: durable queue unavailable (%v); using the explicitly enabled in-process fallback", err)
		} else {
			schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Durable: true, Running: true, Detail: "durable worker attached"})
			return
		}
	} else {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Detail: "legacy scheduler explicitly configured"})
	}
	if !durableSchedulerEnabled() {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Running: true, Detail: "legacy scheduler explicitly configured"})
	} else if legacyFallbackEnabled() {
		schedulerstatus.Record(schedulerstatus.State{Name: "ambient", Enabled: true, Running: true, Detail: "explicitly enabled legacy fallback"})
	}
	scheduler := &Scheduler{service: service, backgroundAllowed: backgroundAllowed}
	go scheduler.Start(ctx, interval)
}

func legacyFallbackEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED")))
	return value == "true" || value == "1" || value == "yes"
}

func (s *Scheduler) Start(ctx context.Context, interval time.Duration) {
	if interval < 30*time.Second {
		interval = 5 * time.Minute
	}
	if runOnStartup() {
		s.runOnce()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func runOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AMBIENT_RUN_ON_STARTUP")))
	return value == "true" || value == "1" || value == "yes"
}

func (s *Scheduler) runOnce() {
	if s.service == nil || (s.backgroundAllowed != nil && !s.backgroundAllowed()) || !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	scan, err := s.service.Scan("scheduler")
	if err != nil {
		log.Printf("ambient scan failed: %v", err)
		return
	}
	if scan.Created > 0 || scan.Updated > 0 || scan.Advanced > 0 {
		log.Printf("ambient scan examined=%d created=%d updated=%d deduplicated=%d advanced=%d filtered=%d skipped=%d blocked=%d", scan.ItemsExamined, scan.Created, scan.Updated, scan.Deduplicated, scan.Advanced, scan.Filtered, scan.Skipped, scan.Blocked)
	}
}

func schedulerBackgroundGate(allowed []func() bool) func() bool {
	if len(allowed) > 0 && allowed[0] != nil {
		return allowed[0]
	}
	return func() bool { return true }
}
