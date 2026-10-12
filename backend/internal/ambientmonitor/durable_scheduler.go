package ambientmonitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifecycle"
)

const (
	monitorSweepJobKind = "outcome-monitor.sweep"
	monitorWorkerID     = "outcome-monitor-scheduler"
	monitorMaxAttempts  = 3
)

func monitorSafetyGate(allowed func() bool) func() bool {
	if allowed == nil {
		return func() bool { return false }
	}
	return allowed
}

type schedulerService interface {
	DueScopes(context.Context, time.Time, int) ([]Scope, error)
	PendingCompositionScopes(context.Context, time.Time, int) ([]Scope, error)
	RecoverExpiredLeases(context.Context, Scope, time.Time) (int, error)
	RecoverExpiredCompositionLeases(context.Context, Scope, time.Time) (int, error)
	ProcessDue(context.Context, ProcessDueRequest) (ProcessDueResult, error)
}

// RegisterDurableScheduling creates a singleton, restart-safe advisory sweep.
// The safety callback is checked for every run, so emergency stop/read-only
// control remains authoritative over background processing.
func RegisterDurableScheduling(runner *durablejob.Runner, service schedulerService, allowed func() bool, interval time.Duration) error {
	if runner == nil || service == nil {
		return fmt.Errorf("ambient outcome scheduling requires a runner and service")
	}
	allowed = monitorSafetyGate(allowed)
	return runner.RegisterRecurring(monitorSweepJobKind, interval, monitorMaxAttempts, func(ctx context.Context) error {
		if !allowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		return runMonitorSweep(ctx, service, time.Now().UTC(), allowed)
	})
}

func runMonitorSweep(ctx context.Context, service schedulerService, now time.Time, allowed func() bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dueScopes, err := service.DueScopes(ctx, now, monitorScopeLimit())
	if err != nil {
		return fmt.Errorf("discover due monitor scopes: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	compositionScopes, err := service.PendingCompositionScopes(ctx, now, monitorScopeLimit())
	if err != nil {
		return fmt.Errorf("discover due composition scopes: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scopes := mergeScopes(dueScopes, compositionScopes, monitorScopeLimit())
	failures := 0
	for _, scope := range scopes {
		if ctx.Err() != nil || !allowed() {
			if err := ctx.Err(); err != nil {
				return err
			}
			return durablejob.Defer("background processing is paused by safety policy")
		}
		if _, err := service.RecoverExpiredLeases(ctx, scope, now); err != nil {
			failures++
			continue
		}
		if _, err := service.RecoverExpiredCompositionLeases(ctx, scope, now); err != nil {
			failures++
			continue
		}
		for processed := 0; processed < monitorBatchLimit(); processed++ {
			if ctx.Err() != nil || !allowed() {
				if err := ctx.Err(); err != nil {
					return err
				}
				return durablejob.Defer("background processing is paused by safety policy")
			}
			asOf := time.Now().UTC().Truncate(time.Microsecond)
			result, err := service.ProcessDue(ctx, ProcessDueRequest{
				Scope: scope, WorkerID: monitorWorkerID, Now: asOf,
				LeaseDuration: monitorLeaseDuration(), Limit: 1,
			})
			if err != nil {
				failures++
				break
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			terminalCompositionFailure := false
			for _, failure := range result.Compositions.Failures {
				if !failure.Retrying {
					terminalCompositionFailure = true
					break
				}
			}
			if len(result.Failures) > 0 || terminalCompositionFailure {
				failures++
			}
			if result.Claimed == 0 && result.Compositions.Claimed == 0 {
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("ambient outcome sweep failed for %d scoped batch(es)", failures)
	}
	return nil
}

func mergeScopes(groupsA, groupsB []Scope, limit int) []Scope {
	seen := make(map[Scope]struct{}, len(groupsA)+len(groupsB))
	result := make([]Scope, 0, len(groupsA)+len(groupsB))
	for _, groups := range [][]Scope{groupsA, groupsB} {
		for _, scope := range groups {
			if _, exists := seen[scope]; exists {
				continue
			}
			seen[scope] = struct{}{}
			result = append(result, scope)
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func StartDurableScheduler(ctx context.Context, service *Service, allowed func() bool) error {
	repository, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repository, durablejob.Options{Queue: "outcome-monitor"})
	if err := RegisterDurableScheduling(runner, service, allowed, monitorSweepInterval()); err != nil {
		return err
	}
	if !lifecycle.Go(ctx, "ambient-monitor-worker", func() { runner.Start(ctx, monitorPollInterval()) }) {
		return context.Canceled
	}
	return nil
}

func DurableSchedulerEnabled() bool {
	return envBool("OUTCOME_MONITOR_SCHEDULER_ENABLED", true)
}

func monitorSweepInterval() time.Duration {
	return envSeconds("OUTCOME_MONITOR_SWEEP_SECONDS", 300, 60, 86400)
}

func monitorPollInterval() time.Duration {
	// Outcome monitoring is advisory and queued work is lease-protected. A
	// Startup executes immediately and recurring work persists its next due
	// time. A five-minute idle poll therefore removes most empty-installation
	// database wake-ups while retaining bounded recovery after a worker loss.
	return envSeconds("OUTCOME_MONITOR_POLL_SECONDS", 300, 1, 300)
}

func monitorLeaseDuration() time.Duration {
	return envSeconds("OUTCOME_MONITOR_LEASE_SECONDS", 120, 5, 1800)
}

func monitorScopeLimit() int { return envInt("OUTCOME_MONITOR_SCOPE_LIMIT", 50, 1, 100) }
func monitorBatchLimit() int { return envInt("OUTCOME_MONITOR_BATCH_LIMIT", 20, 1, 100) }

func envSeconds(key string, fallback, minimum, maximum int) time.Duration {
	return time.Duration(envInt(key, fallback, minimum, maximum)) * time.Second
}

func envInt(key string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
