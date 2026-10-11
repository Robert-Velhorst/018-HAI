package workflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/safety"
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
	defaultPollSecond = 5 * time.Minute
	minPollInterval   = 15 * time.Second
	maxPollInterval   = time.Hour
)

type workflowSweepError struct{ cause error }

func (e *workflowSweepError) Error() string {
	return "workflow sweep: " + safety.RedactSecrets(e.cause.Error())
}

func (e *workflowSweepError) Unwrap() error { return e.cause }

func workflowSweepMustStop(err error) bool {
	var deferred *durablejob.DeferredError
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrClaimRecoveryContextUnavailable) || errors.Is(err, ErrClaimRecoveryOutcomeUnconfirmed) ||
		errors.Is(err, ErrTaskExecutionContextUnavailable) ||
		errors.Is(err, ErrFollowUpContextUnavailable) || errors.Is(err, ErrReminderDeliveryContextUnavailable) || errors.As(err, &deferred)
}

// RegisterDurableScheduling registers the workflow sweep as a durable recurring
// job. Safe to call on every startup: the job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service ScheduledWorkflowService, interval time.Duration, limit int, allowed ...func() bool) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable workflow scheduling needs both a runner and a service")
	}
	if _, ok := service.(ContextualClaimRecoveryBatchService); !ok {
		return ErrClaimRecoveryContextUnavailable
	}
	if schedulerEnabled("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", true) {
		if _, ok := service.(ContextualFollowUpBatchService); !ok {
			return ErrFollowUpContextUnavailable
		}
	}
	if _, legacy := service.(ReminderDeliveryService); legacy && schedulerEnabled("WORKFLOW_REMINDER_DELIVERY_ENABLED", true) {
		if _, ok := service.(ContextualReminderDeliveryBatchService); !ok {
			return ErrReminderDeliveryContextUnavailable
		}
	}
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if _, ok := service.(ContextualWorkflowExecutionBatchService); !ok {
		return ErrTaskExecutionContextUnavailable
	}
	if err := runner.RegisterRecurring(JobKindSweep, interval, sweepMaxAttempts, func(ctx context.Context) error {
		return runWorkflowSweep(ctx, service, limit, backgroundAllowed)
	}); err != nil {
		return err
	}
	log.Printf("workflow scheduler: durable sweep job scheduled (interval %s)", interval)
	return nil
}

// runWorkflowSweep performs one sweep: recover stale claims, advance due open
// loops, then run due workflows. Errors are aggregated so one failing stage
// still lets the others run, unless cancellation or safety policy stops the
// sweep. Entered task calls forward cancellation into the model/runtime path;
// recovery, follow-up and reminder delivery use owned context transactions.
func runWorkflowSweep(ctx context.Context, service ScheduledWorkflowService, limit int, allowed ...func() bool) error {
	if ctx == nil || service == nil {
		return ErrFollowUpContextUnavailable
	}
	gate := schedulerBackgroundGate(allowed)
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !gate() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		return nil
	}
	if err := checkpoint(); err != nil {
		return err
	}
	recoveryService, contextualRecovery := service.(ContextualClaimRecoveryBatchService)
	if !contextualRecovery {
		return ErrClaimRecoveryContextUnavailable
	}
	followups, contextual := service.(ContextualFollowUpBatchService)
	openLoopEnabled := schedulerEnabled("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", true)
	if openLoopEnabled && !contextual {
		return ErrFollowUpContextUnavailable
	}
	reminders, contextualReminders := service.(ContextualReminderDeliveryBatchService)
	reminderEnabled := schedulerEnabled("WORKFLOW_REMINDER_DELIVERY_ENABLED", true)
	if _, legacy := service.(ReminderDeliveryService); legacy && reminderEnabled && !contextualReminders {
		return ErrReminderDeliveryContextUnavailable
	}
	tasks, contextualTasks := service.(ContextualWorkflowExecutionBatchService)
	if !contextualTasks {
		return ErrTaskExecutionContextUnavailable
	}
	if limit <= 0 {
		limit = 2
	}
	request := RunDueRequest{Limit: limit}
	problems := []error{}

	recovery, err := recoveryService.RecoverStaleClaimsContext(ctx, request)
	if err == nil && recovery == nil {
		return &workflowSweepError{cause: ErrClaimRecoveryOutcomeUnconfirmed}
	}
	if err != nil {
		if workflowSweepMustStop(err) {
			return &workflowSweepError{cause: err}
		}
		problems = append(problems, fmt.Errorf("claim recovery: %w", err))
	} else if recovery != nil && (recovery.WorkflowsBlocked > 0 || recovery.OpenLoopsReopened > 0 || recovery.Skipped > 0) {
		log.Printf("workflow claim recovery checked=%d workflows_blocked=%d open_loops_reopened=%d skipped=%d",
			recovery.Checked, recovery.WorkflowsBlocked, recovery.OpenLoopsReopened, recovery.Skipped)
	}

	if err := checkpoint(); err != nil {
		return err
	}
	if openLoopEnabled {
		openLoops, err := followups.RunDueOpenLoopsContext(ctx, request)
		if err != nil {
			if workflowSweepMustStop(err) {
				return &workflowSweepError{cause: err}
			}
			problems = append(problems, fmt.Errorf("open loops: %w", err))
		} else if openLoops != nil && (openLoops.Triggered > 0 || openLoops.Resolved > 0 || openLoops.Skipped > 0) {
			log.Printf("workflow open-loop scheduler checked=%d triggered=%d resolved=%d skipped=%d",
				openLoops.Checked, openLoops.Triggered, openLoops.Resolved, openLoops.Skipped)
		}
	}
	if err := checkpoint(); err != nil {
		return err
	}
	if contextualReminders && reminderEnabled {
		reminders, reminderErr := reminders.RunDueReminderDeliveriesContext(ctx, request)
		if reminderErr == nil && reminders == nil {
			return &workflowSweepError{cause: ErrReminderDeliveryContextUnavailable}
		}
		if reminderErr != nil {
			if workflowSweepMustStop(reminderErr) {
				return &workflowSweepError{cause: reminderErr}
			}
			problems = append(problems, fmt.Errorf("reminder deliveries: %w", reminderErr))
		} else if reminders != nil && (reminders.Delivered > 0 || reminders.Retried > 0 || reminders.Suppressed > 0 || reminders.DeadLettered > 0) {
			log.Printf("workflow reminder delivery checked=%d delivered=%d retried=%d suppressed=%d dead_lettered=%d", reminders.Checked, reminders.Delivered, reminders.Retried, reminders.Suppressed, reminders.DeadLettered)
		}
	}

	if err := checkpoint(); err != nil {
		return err
	}
	result, err := tasks.RunDueContext(ctx, request)
	if err == nil && result == nil {
		return &workflowSweepError{cause: ErrTaskExecutionContextUnavailable}
	}
	if err != nil {
		if workflowSweepMustStop(err) {
			return &workflowSweepError{cause: err}
		}
		problems = append(problems, fmt.Errorf("run due: %w", err))
	} else if result != nil && (result.Completed > 0 || result.Retried > 0 || result.Blocked > 0) {
		log.Printf("workflow scheduler checked=%d completed=%d retried=%d blocked=%d skipped=%d",
			result.Checked, result.Completed, result.Retried, result.Blocked, result.Skipped)
	}

	if err := checkpoint(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return &workflowSweepError{cause: errors.Join(problems...)}
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
	if !lifecycle.Go(ctx, "workflow-durable-worker", func() { runner.Start(ctx, workflowPollInterval()) }) {
		return context.Canceled
	}
	return nil
}

func workflowPollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("WORKFLOW_WORKER_POLL_SECONDS"))
	if value == "" {
		return defaultPollSecond
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < int64(minPollInterval/time.Second) || seconds > int64(maxPollInterval/time.Second) {
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
