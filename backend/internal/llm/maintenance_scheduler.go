package llm

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

const modelMaintenanceEmergencyStopPollInterval = 100 * time.Millisecond
const modelMaintenanceSchedulerPermissionPollInterval = time.Minute
const modelMaintenanceSchedulerMaximumFallback = 24 * time.Hour

type modelMaintenanceSchedulerEvent uint8

const (
	modelMaintenanceSchedulerDue modelMaintenanceSchedulerEvent = iota + 1
	modelMaintenanceSchedulerWake
	modelMaintenanceSchedulerPermissionPoll
	modelMaintenanceSchedulerCancelled
)

type modelMaintenanceSchedulerContextKey struct{}

var activeModelMaintenanceSchedulers sync.Map

// StartModelMaintenanceScheduler keeps configured models and strict provider
// readiness evidence fresh when no task routes through them. Both schedulers
// use the shared pause gate; provider probes also require durable history and
// a cross-process lease. A missing backgroundAllowed callback prevents startup.
func StartModelMaintenanceScheduler(ctx context.Context, service *Service, backgroundAllowed func() bool) {
	if ctx == nil || service == nil || backgroundAllowed == nil || !modelMaintenanceSchedulerEnabled() {
		return
	}
	if normalizeProbePolicy(service.policy).RequireRecentLiveProviderProbe {
		startProviderProbeScheduler(ctx, service, backgroundAllowed)
	}
	startModelMaintenanceScheduler(service, func() {
		runModelMaintenanceSchedulerWithWait(
			ctx,
			backgroundAllowed,
			service.RunDueModelMaintenanceWithContext,
			service.modelMaintenanceWakeChannel(),
			waitForModelMaintenanceScheduler,
			time.Now,
		)
	}, ctx)
}

func startModelMaintenanceScheduler(service *Service, run func(), contexts ...context.Context) bool {
	if service == nil || run == nil {
		return false
	}
	if _, loaded := activeModelMaintenanceSchedulers.LoadOrStore(service, struct{}{}); loaded {
		return false
	}
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	if !lifecycle.Go(ctx, "llm-model-maintenance", func() {
		defer activeModelMaintenanceSchedulers.Delete(service)
		run()
	}) {
		activeModelMaintenanceSchedulers.Delete(service)
		return false
	}
	return true
}

func (s *Service) modelMaintenanceWakeChannel() chan struct{} {
	if s == nil {
		return nil
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.maintenanceWake == nil {
		s.maintenanceWake = make(chan struct{}, 1)
	}
	return s.maintenanceWake
}

func (s *Service) signalModelMaintenanceScheduler() {
	wake := s.modelMaintenanceWakeChannel()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
		// One buffered signal is enough: the scheduler always reads durable
		// state after waking, so coalescing repeated writes cannot lose work.
	}
}

func isModelMaintenanceSchedulerRun(ctx context.Context) bool {
	return ctx != nil && ctx.Value(modelMaintenanceSchedulerContextKey{}) == true
}

func runModelMaintenanceSchedulerWithWait(
	ctx context.Context,
	backgroundAllowed func() bool,
	run func(context.Context) ModelMaintenanceRun,
	wake <-chan struct{},
	wait func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent,
	now func() time.Time,
) {
	if ctx == nil || run == nil || wait == nil || now == nil {
		return
	}
	// Signals represent already-persisted state. The startup sweep below reads
	// that durable state, so an earlier request signal is already covered.
	drainModelMaintenanceSchedulerWake(wake)
	needsRun := true
	previouslyAllowed := false
	nextRunAt := time.Time{}
	retryAfterCancellation := time.Time{}

	for ctx.Err() == nil {
		allowed := modelMaintenanceBackgroundAllowed(backgroundAllowed)
		if allowed && !previouslyAllowed {
			needsRun = true
		}
		previouslyAllowed = allowed

		if allowed && needsRun && (retryAfterCancellation.IsZero() || !now().Before(retryAfterCancellation)) {
			// A run consumes all writes that happened before its durable reads.
			// Keep later signals buffered so a concurrent request cannot be lost.
			runCtx := context.WithValue(ctx, modelMaintenanceSchedulerContextKey{}, true)
			maintenanceRun := runScheduledModelMaintenance(runCtx, backgroundAllowed, run)
			if ctx.Err() != nil {
				return
			}
			completedAt := now()
			if maintenanceRun.Cancelled {
				// Emergency-stop cancellation is a pause, not scheduler shutdown.
				// Rate-limit recovery if the stop briefly toggled and avoid a tight
				// retry loop when the permission check is already true again.
				needsRun = true
				previouslyAllowed = false
				retryAfterCancellation = completedAt.Add(modelMaintenanceSchedulerPermissionPollInterval)
				nextRunAt = retryAfterCancellation
			} else {
				needsRun = false
				retryAfterCancellation = time.Time{}
				nextRunAt = completedAt.Add(modelMaintenanceSchedulerNextDelay(maintenanceRun, completedAt))
			}
		}

		if ctx.Err() != nil {
			return
		}
		waitFor := modelMaintenanceSchedulerPermissionPollInterval
		if allowed {
			if needsRun && !retryAfterCancellation.IsZero() && now().Before(retryAfterCancellation) {
				waitFor = retryAfterCancellation.Sub(now())
			} else if needsRun {
				continue
			} else {
				waitFor = nextRunAt.Sub(now())
				if waitFor < 0 {
					waitFor = 0
				}
			}
		}

		event := wait(ctx, waitFor, wake)
		switch event {
		case modelMaintenanceSchedulerCancelled:
			return
		case modelMaintenanceSchedulerWake, modelMaintenanceSchedulerDue:
			needsRun = true
		case modelMaintenanceSchedulerPermissionPoll:
			// Re-evaluate the stop state without touching provider or database.
		default:
			return
		}
	}
}

func drainModelMaintenanceSchedulerWake(wake <-chan struct{}) {
	if wake == nil {
		return
	}
	select {
	case <-wake:
	default:
	}
}

func modelMaintenanceBackgroundAllowed(backgroundAllowed func() bool) bool {
	return backgroundAllowed != nil && backgroundAllowed()
}

func modelMaintenanceSchedulerNextDelay(run ModelMaintenanceRun, now time.Time) time.Duration {
	delay := modelMaintenanceSchedulerMaximumFallback
	for _, result := range run.Results {
		if result.NextCheckDueAt == nil {
			continue
		}
		untilDue := result.NextCheckDueAt.Sub(now)
		if untilDue < 0 {
			untilDue = 0
		}
		if untilDue < delay {
			delay = untilDue
		}
	}
	return delay
}

func waitForModelMaintenanceScheduler(ctx context.Context, delay time.Duration, wake <-chan struct{}) modelMaintenanceSchedulerEvent {
	if ctx == nil || ctx.Err() != nil {
		return modelMaintenanceSchedulerCancelled
	}
	if delay < 0 {
		delay = 0
	}
	dueTimer := time.NewTimer(delay)
	defer dueTimer.Stop()
	permissionTimer := time.NewTimer(modelMaintenanceSchedulerPermissionPollInterval)
	defer permissionTimer.Stop()
	select {
	case <-ctx.Done():
		return modelMaintenanceSchedulerCancelled
	case <-wake:
		return modelMaintenanceSchedulerWake
	case <-dueTimer.C:
		return modelMaintenanceSchedulerDue
	case <-permissionTimer.C:
		return modelMaintenanceSchedulerPermissionPoll
	}
}

func runScheduledModelMaintenance(ctx context.Context, backgroundAllowed func() bool, run func(context.Context) ModelMaintenanceRun) ModelMaintenanceRun {
	if ctx == nil || run == nil || ctx.Err() != nil {
		return ModelMaintenanceRun{}
	}
	finish, admitted := lifecycle.Enter(ctx, "llm-maintenance-run")
	if !admitted {
		return ModelMaintenanceRun{Cancelled: true}
	}
	defer finish()
	if !modelMaintenanceBackgroundAllowed(backgroundAllowed) {
		return ModelMaintenanceRun{Cancelled: true}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if backgroundAllowed != nil {
		monitorReady := make(chan struct{})
		monitorDone := make(chan struct{})
		if !lifecycle.Go(runCtx, "llm-maintenance-permit-monitor", func() {
			defer close(monitorDone)
			ticker := time.NewTicker(modelMaintenanceEmergencyStopPollInterval)
			defer ticker.Stop()
			if runCtx.Err() != nil || !backgroundAllowed() {
				cancel()
				return
			}
			close(monitorReady)
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					if !backgroundAllowed() {
						cancel()
						return
					}
				}
			}
		}) {
			return ModelMaintenanceRun{Cancelled: true}
		}
		defer func() { cancel(); <-monitorDone }()
		select {
		case <-monitorReady:
		case <-monitorDone:
			return ModelMaintenanceRun{Cancelled: true}
		}
		if runCtx.Err() != nil {
			return ModelMaintenanceRun{Cancelled: true}
		}
		maintenanceRun := run(runCtx)
		cancel()
		<-monitorDone
		reportModelMaintenanceRun(maintenanceRun)
		return maintenanceRun
	}

	maintenanceRun := run(runCtx)
	reportModelMaintenanceRun(maintenanceRun)
	return maintenanceRun
}

// reportModelMaintenanceRun emits only aggregate state. It intentionally does
// not log provider endpoints, model output, or individual failure details.
func reportModelMaintenanceRun(run ModelMaintenanceRun) {
	if !modelMaintenanceNeedsReport(run) {
		return
	}
	log.Printf(
		"model maintenance eligible=%d checked=%d provider_managed=%d updated=%d failed=%d reused=%d",
		run.Eligible,
		run.Checked,
		run.ProviderManaged,
		run.Updated,
		run.Failed,
		run.Reused,
	)
}

func modelMaintenanceNeedsReport(run ModelMaintenanceRun) bool {
	return run.Failed > 0 || run.Updated > 0
}

func modelMaintenanceSchedulerEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED"))
	if raw == "" {
		return true
	}
	return envEnabled("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED")
}
