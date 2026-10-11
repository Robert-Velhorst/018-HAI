package workflow

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
)

// Durable workflow scheduling.
//
// The legacy Scheduler is an in-process ticker, so a restart mid-sweep loses the
// run and a failure waits a whole interval with no backoff. This registers the
// same sweep as a durable, self-rescheduling job: it survives restarts, retries
// on backoff, and is recovered by lease if the worker dies holding it.
const (
	JobKindSweep      = "workflow.sweep"
	sweepMaxAttempts  = 3
	defaultPollSecond = 15 * time.Second
)

// RegisterDurableScheduling registers the workflow sweep as a durable recurring
// job. Safe to call on every startup: the job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service ScheduledWorkflowService, interval time.Duration, limit int, allowed ...func() bool) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable workflow scheduling needs both a runner and a service")
	}
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if err := runner.RegisterRecurring(JobKindSweep, interval, sweepMaxAttempts, func(ctx context.Context) error {
		if !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		return runWorkflowSweep(service, limit)
	}); err != nil {
		return err
	}
	log.Printf("workflow scheduler: durable sweep job scheduled (interval %s)", interval)
	return nil
}

// runWorkflowSweep performs one sweep: recover stale claims, advance due open
// loops, then run due workflows. Errors are aggregated so one failing stage
// still lets the others run, while the job as a whole reports failure and
// retries on backoff.
func runWorkflowSweep(service ScheduledWorkflowService, limit int) error {
	if limit <= 0 {
		limit = 2
	}
	request := RunDueRequest{Limit: limit}
	problems := []string{}

	recovery, err := service.RecoverStaleClaims(request)
	if err != nil {
		problems = append(problems, "claim recovery: "+err.Error())
	} else if recovery != nil && (recovery.WorkflowsBlocked > 0 || recovery.OpenLoopsReopened > 0 || recovery.Skipped > 0) {
		log.Printf("workflow claim recovery checked=%d workflows_blocked=%d open_loops_reopened=%d skipped=%d",
			recovery.Checked, recovery.WorkflowsBlocked, recovery.OpenLoopsReopened, recovery.Skipped)
	}

	if schedulerEnabled("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", true) {
		openLoops, err := service.RunDueOpenLoops(request)
		if err != nil {
			problems = append(problems, "open loops: "+err.Error())
		} else if openLoops != nil && (openLoops.Triggered > 0 || openLoops.Resolved > 0 || openLoops.Skipped > 0) {
			log.Printf("workflow open-loop scheduler checked=%d triggered=%d resolved=%d skipped=%d",
				openLoops.Checked, openLoops.Triggered, openLoops.Resolved, openLoops.Skipped)
		}
	}
	if delivery, ok := service.(ReminderDeliveryService); ok && schedulerEnabled("WORKFLOW_REMINDER_DELIVERY_ENABLED", true) {
		reminders, reminderErr := delivery.RunDueReminderDeliveries(request)
		if reminderErr != nil {
			problems = append(problems, "reminder deliveries: "+reminderErr.Error())
		} else if reminders != nil && (reminders.Delivered > 0 || reminders.Retried > 0 || reminders.Suppressed > 0 || reminders.DeadLettered > 0) {
			log.Printf("workflow reminder delivery checked=%d delivered=%d retried=%d suppressed=%d dead_lettered=%d", reminders.Checked, reminders.Delivered, reminders.Retried, reminders.Suppressed, reminders.DeadLettered)
		}
	}

	result, err := service.RunDue(request)
	if err != nil {
		problems = append(problems, "run due: "+err.Error())
	} else if result != nil && (result.Completed > 0 || result.Retried > 0 || result.Blocked > 0) {
		log.Printf("workflow scheduler checked=%d completed=%d retried=%d blocked=%d skipped=%d",
			result.Checked, result.Completed, result.Retried, result.Blocked, result.Skipped)
	}

	if len(problems) > 0 {
		return fmt.Errorf("workflow sweep: %s", strings.Join(problems, "; "))
	}
	return nil
}

// startDurableScheduler builds the runner over the default queue and starts it.
// Any failure is returned so the caller can fall back to the legacy ticker.
func startDurableScheduler(ctx context.Context, service ScheduledWorkflowService, interval time.Duration, limit int, allowed ...func() bool) error {
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "workflow"})
	if err := RegisterDurableScheduling(runner, service, interval, limit, allowed...); err != nil {
		return err
	}
	go runner.Start(ctx, workflowPollInterval())
	return nil
}

func workflowPollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("WORKFLOW_WORKER_POLL_SECONDS"))
	if value == "" {
		return defaultPollSecond
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 1 {
		return defaultPollSecond
	}
	return time.Duration(seconds) * time.Second
}

func durableSchedulerEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("WORKFLOW_SCHEDULER_DURABLE"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}
