package workflow

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/safety"
)

const (
	defaultSchedulerInterval = 10 * time.Minute
	minSchedulerInterval     = 15 * time.Second
	maxSchedulerInterval     = 24 * time.Hour
)

type ScheduledWorkflowService interface {
	RecoverStaleClaims(request RunDueRequest) (*ClaimRecoverySummary, error)
	RunDue(request RunDueRequest) (*WorkflowRunSummary, error)
	RunDueOpenLoops(request RunDueRequest) (*OpenLoopRunSummary, error)
}

type Scheduler struct {
	service           ScheduledWorkflowService
	interval          time.Duration
	limit             int
	backgroundAllowed func() bool
	running           atomic.Bool
}

func NewScheduler(service ScheduledWorkflowService, interval time.Duration, limit int, allowed ...func() bool) *Scheduler {
	if interval < minSchedulerInterval || interval > maxSchedulerInterval {
		interval = defaultSchedulerInterval
	}
	if limit <= 0 {
		limit = 2
	}
	return &Scheduler{service: service, interval: interval, limit: limit, backgroundAllowed: schedulerBackgroundGate(allowed)}
}

// StartScheduler starts the workflow sweep.
//
// It prefers the durable path (persisted, retried, crash-recovered — see
// durable_scheduler.go) and falls back to the legacy in-process ticker, saying
// so, if the durable queue cannot be reached.
func StartScheduler(ctx context.Context, service ScheduledWorkflowService, allowed ...func() bool) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if !schedulerEnabled("WORKFLOW_SCHEDULER_ENABLED", true) {
		return
	}
	interval := schedulerInterval("WORKFLOW_SCHEDULER_INTERVAL_SECONDS")
	limit := schedulerLimit()
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if durableSchedulerEnabled() {
		if err := startDurableScheduler(ctx, service, interval, limit, backgroundAllowed); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("workflow scheduler: durable queue unavailable (%s); falling back to the in-process ticker", safety.RedactSecrets(err.Error()))
		} else {
			return
		}
	}
	scheduler := NewScheduler(service, interval, limit, backgroundAllowed)
	lifecycle.Go(ctx, "workflow-scheduler", func() { scheduler.Start(ctx) })
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
	if ctx == nil || ctx.Err() != nil || s.service == nil || (s.backgroundAllowed != nil && !s.backgroundAllowed()) || !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	if err := runWorkflowSweep(ctx, s.service, s.limit, s.backgroundAllowed); err != nil {
		log.Printf("workflow scheduler: %s", safety.RedactSecrets(err.Error()))
	}
}

func schedulerBackgroundGate(allowed []func() bool) func() bool {
	if len(allowed) > 0 && allowed[0] != nil {
		return allowed[0]
	}
	return func() bool { return true }
}

func schedulerEnabled(name string, defaultEnabled bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return defaultEnabled
	}
	return value == "true" || value == "1" || value == "yes"
}

func schedulerInterval(name string) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultSchedulerInterval
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < int64(minSchedulerInterval/time.Second) || seconds > int64(maxSchedulerInterval/time.Second) {
		return defaultSchedulerInterval
	}
	return time.Duration(seconds) * time.Second
}

func schedulerLimit() int {
	value := strings.TrimSpace(os.Getenv("WORKFLOW_SCHEDULER_RUN_LIMIT"))
	if value == "" {
		return 2
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return 2
	}
	if parsed > 50 {
		return 50
	}
	return parsed
}

func schedulerRunOnStartup() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("WORKFLOW_SCHEDULER_RUN_ON_STARTUP")))
	return value == "true" || value == "1" || value == "yes"
}

func claimLeaseDuration() time.Duration {
	value := strings.TrimSpace(os.Getenv("WORKFLOW_CLAIM_LEASE_SECONDS"))
	if value == "" {
		return 15 * time.Minute
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 60 {
		return 15 * time.Minute
	}
	if seconds > int64((24 * time.Hour).Seconds()) {
		return 24 * time.Hour
	}
	return time.Duration(seconds) * time.Second
}
