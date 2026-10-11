package ambientmonitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
)

const (
	monitorSweepJobKind = "outcome-monitor.sweep"
	monitorWorkerID     = "outcome-monitor-scheduler"
	monitorMaxAttempts  = 3
)

type schedulerService interface {
	DueScopes(context.Context, time.Time, int) ([]Scope, error)
	PendingCompositionScopes(context.Context, time.Time, int) ([]Scope, error)
	RecoverExpiredLeases(context.Context, Scope, time.Time) (int, error)
	RecoverExpiredCompositionLeases(context.Context, Scope, time.Time) (int, error)
	ProcessDue(context.Context, ProcessDueRequest) (ProcessDueResult, error)
}

const (
	SweepResultCompleted   = "completed"
	SweepResultFailed      = "failed"
	SweepResultInterrupted = "interrupted"
	SweepResultSkipped     = "skipped"
)

// SweepMetrics is the privacy-safe operational summary of one scheduler pass.
// Counts are bounded by the configured scope and batch limits; no owner,
// workspace, target, prompt, source, or failure detail leaves the scheduler.
type SweepMetrics struct {
	Duration                   time.Duration
	Result                     string
	DueCollectionScopes        int
	DueCompositionScopes       int
	CollectionLeasesRecovered  int
	CompositionLeasesRecovered int
	CollectionClaimed          int
	CollectionCompleted        int
	CollectionFailed           int
	CompositionClaimed         int
	CompositionSucceeded       int
	CompositionRetrying        int
	CompositionFailed          int
}

type SweepObserver interface {
	ObserveOutcomeMonitorSweep(SweepMetrics)
}

// RegisterDurableScheduling creates a singleton, restart-safe advisory sweep.
// The safety callback is checked for every run, so emergency stop/read-only
// control remains authoritative over background processing.
func RegisterDurableScheduling(runner *durablejob.Runner, service schedulerService, allowed func() bool, interval time.Duration, observer SweepObserver) error {
	if runner == nil || service == nil {
		return fmt.Errorf("ambient outcome scheduling requires a runner and service")
	}
	if allowed == nil {
		allowed = func() bool { return true }
	}
	return runner.RegisterRecurring(monitorSweepJobKind, interval, monitorMaxAttempts, func(ctx context.Context) error {
		if !allowed() {
			observeSweep(observer, SweepMetrics{Result: SweepResultSkipped})
			return nil
		}
		return runMonitorSweep(ctx, service, time.Now().UTC(), allowed, observer)
	})
}

func runMonitorSweep(ctx context.Context, service schedulerService, now time.Time, allowed func() bool, observer SweepObserver) (sweepErr error) {
	started := time.Now()
	observation := SweepMetrics{Result: SweepResultCompleted}
	defer func() {
		observation.Duration = time.Since(started)
		if sweepErr != nil {
			observation.Result = SweepResultFailed
		}
		observeSweep(observer, observation)
	}()

	dueScopes, err := service.DueScopes(ctx, now, monitorScopeLimit())
	if err != nil {
		return fmt.Errorf("discover due monitor scopes: %w", err)
	}
	observation.DueCollectionScopes = len(dueScopes)
	compositionScopes, err := service.PendingCompositionScopes(ctx, now, monitorScopeLimit())
	if err != nil {
		return fmt.Errorf("discover due composition scopes: %w", err)
	}
	observation.DueCompositionScopes = len(compositionScopes)
	scopes := mergeScopes(dueScopes, compositionScopes, monitorScopeLimit())
	failures := 0
	for _, scope := range scopes {
		if ctx.Err() != nil || !allowed() {
			observation.Result = SweepResultInterrupted
			return nil
		}
		recovered, err := service.RecoverExpiredLeases(ctx, scope, now)
		if err != nil {
			failures++
			continue
		}
		observation.CollectionLeasesRecovered += recovered
		compositionRecovered, err := service.RecoverExpiredCompositionLeases(ctx, scope, now)
		if err != nil {
			failures++
			continue
		}
		observation.CompositionLeasesRecovered += compositionRecovered
		for processed := 0; processed < monitorBatchLimit(); processed++ {
			if ctx.Err() != nil || !allowed() {
				observation.Result = SweepResultInterrupted
				return nil
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
			observation.CollectionClaimed += result.Claimed
			observation.CollectionCompleted += len(result.Completions)
			observation.CollectionFailed += len(result.Failures)
			observation.CompositionClaimed += result.Compositions.Claimed
			observation.CompositionSucceeded += result.Compositions.Succeeded
			terminalCompositionFailures := 0
			for _, failure := range result.Compositions.Failures {
				if failure.Retrying {
					observation.CompositionRetrying++
				} else {
					terminalCompositionFailures++
				}
			}
			observation.CompositionFailed += terminalCompositionFailures
			if len(result.Failures) > 0 || terminalCompositionFailures > 0 {
				failures++
			}
			if result.Claimed == 0 && result.Compositions.Claimed == 0 {
				break
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("ambient outcome sweep failed for %d scoped batch(es)", failures)
	}
	return nil
}

func observeSweep(observer SweepObserver, observation SweepMetrics) {
	if observer != nil {
		observer.ObserveOutcomeMonitorSweep(observation)
	}
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

func StartDurableScheduler(ctx context.Context, service *Service, allowed func() bool, observer SweepObserver) error {
	repository, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repository, durablejob.Options{Queue: "outcome-monitor"})
	if err := RegisterDurableScheduling(runner, service, allowed, monitorSweepInterval(), observer); err != nil {
		return err
	}
	go runner.Start(ctx, monitorPollInterval())
	return nil
}

func DurableSchedulerEnabled() bool {
	return envBool("OUTCOME_MONITOR_SCHEDULER_ENABLED", true)
}

func monitorSweepInterval() time.Duration {
	return envSeconds("OUTCOME_MONITOR_SWEEP_SECONDS", 300, 60, 86400)
}

func monitorPollInterval() time.Duration {
	return envSeconds("OUTCOME_MONITOR_POLL_SECONDS", 15, 1, 300)
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
