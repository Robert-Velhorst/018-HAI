package source

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

const (
	defaultSchedulerInterval = 10 * time.Minute
	minSchedulerInterval     = 15 * time.Second
	maxSchedulerInterval     = 24 * time.Hour
)

type Scheduler struct {
	service           Service
	interval          time.Duration
	backgroundAllowed func() bool
	running           atomic.Bool
}

func NewScheduler(service Service, interval time.Duration, allowed ...func() bool) *Scheduler {
	if interval < minSchedulerInterval || interval > maxSchedulerInterval {
		interval = defaultSchedulerInterval
	}
	return &Scheduler{service: service, interval: interval, backgroundAllowed: schedulerBackgroundGate(allowed)}
}

// StartScheduler starts durable scheduled syncing, or an explicitly enabled
// volatile fallback. Startup fails closed when durable scheduling is
// unavailable unless SOURCE_SCHEDULER_VOLATILE_FALLBACK is explicitly enabled.
func StartScheduler(ctx context.Context, service Service, allowed ...func() bool) error {
	return startScheduler(ctx, service, startDurableScheduler, startVolatileScheduler, startDurableManualWorker, allowed...)
}

type manualWorkerStarter func(context.Context, Service, ...func() bool) error
type durableSchedulerStarter func(context.Context, Service, time.Duration, ...func() bool) error
type volatileSchedulerStarter func(context.Context, Service, time.Duration, ...func() bool) bool

func startScheduler(ctx context.Context, service Service, startDurable durableSchedulerStarter, startVolatile volatileSchedulerStarter, startManual manualWorkerStarter, allowed ...func() bool) error {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	startManualIfEnabled := func() {
		if envBool("SOURCE_MANUAL_WORKER_ENABLED") {
			if err := startManual(ctx, service, allowed...); err != nil {
				log.Printf("source manual worker unavailable: %v", err)
			}
		}
	}
	if !schedulerEnabled() {
		startManualIfEnabled()
		return nil
	}
	interval := schedulerInterval()
	backgroundAllowed := schedulerBackgroundGate(allowed)
	var durableErr error
	if durableSchedulerEnabled() {
		durableErr = startDurable(ctx, service, interval, backgroundAllowed)
		if durableErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
	} else {
		durableErr = errors.New("durable source scheduling is disabled")
	}
	if !volatileSchedulerFallbackEnabled() {
		return fmt.Errorf("source scheduler durable startup failed: %w (set SOURCE_SCHEDULER_VOLATILE_FALLBACK=true to explicitly allow degraded in-memory scheduling)", durableErr)
	}
	if ctx.Err() != nil {
		return nil
	}
	if !startVolatile(ctx, service, interval, backgroundAllowed) {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("source scheduler durable startup failed: %w; explicitly enabled volatile fallback could not start", durableErr)
	}
	log.Printf("source scheduler DEGRADED: volatile in-memory fallback is active after durable startup failure (%v); scheduled work is not crash-recoverable", durableErr)
	return nil
}

func durableSchedulerEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_DURABLE"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func volatileSchedulerFallbackEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_VOLATILE_FALLBACK"))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func startVolatileScheduler(ctx context.Context, service Service, interval time.Duration, allowed ...func() bool) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	scheduler := NewScheduler(service, interval, allowed...)
	return lifecycle.Go(ctx, "source-scheduler-volatile-degraded", func() { scheduler.Start(ctx) })
}

func (s *Scheduler) Start(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if schedulerRunOnStartup() {
		s.runOnce(ctx)
	}
	ticker := time.NewTicker(s.interval)
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

func (s *Scheduler) runOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if s.backgroundAllowed != nil && !s.backgroundAllowed() {
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	result, err := runScheduledSyncsWithContext(ctx, s.service, time.Now().UTC())
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
		return defaultSchedulerInterval
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < int64(minSchedulerInterval/time.Second) || seconds > int64(maxSchedulerInterval/time.Second) {
		return defaultSchedulerInterval
	}
	return time.Duration(seconds) * time.Second
}

func schedulerRunOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SOURCE_SCHEDULER_RUN_ON_STARTUP")))
	return value == "true" || value == "1" || value == "yes"
}
