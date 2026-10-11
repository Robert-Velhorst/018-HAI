package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type Scheduler struct {
	service           Service
	interval          time.Duration
	backgroundAllowed func() bool
	running           atomic.Bool
}

func NewScheduler(service Service, interval time.Duration, allowed ...func() bool) *Scheduler {
	if interval < 15*time.Second {
		interval = 10 * time.Minute
	}
	return &Scheduler{service: service, interval: interval, backgroundAllowed: schedulerBackgroundGate(allowed)}
}

// StartScheduler starts scheduled source syncing.
//
// It prefers the durable path: scans and per-source syncs become persisted jobs
// with backoff retry and crash recovery (see durable_scheduler.go). If the
// durable queue cannot be reached — or SOURCE_SCHEDULER_DURABLE is explicitly
// disabled — it falls back to the legacy in-process ticker and says so, rather
// than silently running no scheduler at all.
func StartScheduler(ctx context.Context, service Service, allowed ...func() bool) {
	if !schedulerEnabled() {
		return
	}
	interval := schedulerInterval()
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if durableSchedulerEnabled() {
		if err := startDurableScheduler(ctx, service, interval, backgroundAllowed); err != nil {
			log.Printf("source scheduler: durable queue unavailable (%v); falling back to the in-process ticker", err)
		} else {
			return
		}
	}
	scheduler := NewScheduler(service, interval, backgroundAllowed)
	go scheduler.Start(ctx)
}

func durableSchedulerEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_DURABLE"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if schedulerRunOnStartup() {
		s.runOnce()
	}
	ticker := time.NewTicker(s.interval)
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

func (s *Scheduler) runOnce() {
	if s.backgroundAllowed != nil && !s.backgroundAllowed() {
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	result, err := s.service.RunDueScheduledSyncs(time.Now().UTC())
	if err != nil {
		log.Printf("source scheduler failed: %v", err)
		return
	}
	if result.Due > 0 || result.Failed > 0 {
		log.Printf("source scheduler checked=%d due=%d completed=%d failed=%d", result.Checked, result.Due, result.Completed, result.Failed)
	}
}

func schedulerBackgroundGate(allowed []func() bool) func() bool {
	if len(allowed) > 0 && allowed[0] != nil {
		return allowed[0]
	}
	return func() bool { return true }
}

func schedulerEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_ENABLED")))
	return value == "" || value == "true" || value == "1" || value == "yes"
}

func schedulerInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("SOURCE_SCHEDULER_INTERVAL_SECONDS"))
	if value == "" {
		return 10 * time.Minute
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 15 {
		return 10 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func schedulerRunOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_RUN_ON_STARTUP")))
	return value == "true" || value == "1" || value == "yes"
}
