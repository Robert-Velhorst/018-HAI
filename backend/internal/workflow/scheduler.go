package workflow

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
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
	if interval < 15*time.Second {
		interval = 10 * time.Minute
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
	if !schedulerEnabled("WORKFLOW_SCHEDULER_ENABLED", true) {
		return
	}
	interval := schedulerInterval("WORKFLOW_SCHEDULER_INTERVAL_SECONDS")
	limit := schedulerLimit()
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if durableSchedulerEnabled() {
		if err := startDurableScheduler(ctx, service, interval, limit, backgroundAllowed); err != nil {
			log.Printf("workflow scheduler: durable queue unavailable (%v); falling back to the in-process ticker", err)
		} else {
			return
		}
	}
	scheduler := NewScheduler(service, interval, limit, backgroundAllowed)
	go scheduler.Start(ctx)
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
	if s.service == nil || (s.backgroundAllowed != nil && !s.backgroundAllowed()) || !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	request := RunDueRequest{Limit: s.limit}
	recovery, err := s.service.RecoverStaleClaims(request)
	if err != nil {
		log.Printf("workflow claim recovery failed: %v", err)
	} else if recovery != nil && (recovery.WorkflowsBlocked > 0 || recovery.OpenLoopsReopened > 0 || recovery.Skipped > 0) {
		log.Printf("workflow claim recovery checked=%d workflows_blocked=%d open_loops_reopened=%d skipped=%d", recovery.Checked, recovery.WorkflowsBlocked, recovery.OpenLoopsReopened, recovery.Skipped)
	}
	if schedulerEnabled("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", true) {
		openLoops, err := s.service.RunDueOpenLoops(request)
		if err != nil {
			log.Printf("workflow open-loop scheduler failed: %v", err)
		} else if openLoops != nil && (openLoops.Triggered > 0 || openLoops.Resolved > 0 || openLoops.Skipped > 0) {
			log.Printf("workflow open-loop scheduler checked=%d triggered=%d resolved=%d skipped=%d", openLoops.Checked, openLoops.Triggered, openLoops.Resolved, openLoops.Skipped)
		}
	}
	if delivery, ok := s.service.(ReminderDeliveryService); ok && schedulerEnabled("WORKFLOW_REMINDER_DELIVERY_ENABLED", true) {
		reminders, err := delivery.RunDueReminderDeliveries(request)
		if err != nil {
			log.Printf("workflow reminder delivery failed: %v", err)
		} else if reminders != nil && (reminders.Delivered > 0 || reminders.Retried > 0 || reminders.Suppressed > 0 || reminders.DeadLettered > 0) {
			log.Printf("workflow reminder delivery checked=%d delivered=%d retried=%d suppressed=%d dead_lettered=%d", reminders.Checked, reminders.Delivered, reminders.Retried, reminders.Suppressed, reminders.DeadLettered)
		}
	}

	result, err := s.service.RunDue(request)
	if err != nil {
		log.Printf("workflow scheduler failed: %v", err)
		return
	}
	if result != nil && (result.Completed > 0 || result.Retried > 0 || result.Blocked > 0) {
		log.Printf("workflow scheduler checked=%d completed=%d retried=%d blocked=%d skipped=%d", result.Checked, result.Completed, result.Retried, result.Blocked, result.Skipped)
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
		return 10 * time.Minute
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 15 {
		return 10 * time.Minute
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
