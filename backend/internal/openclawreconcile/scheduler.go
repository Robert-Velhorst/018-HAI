package openclawreconcile

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
	reconcileJobKind     = "openclaw-gateway.reconcile-sessions"
	reconcileMaxAttempts = 3
)

type reconciliationService interface {
	Reconcile(limit int) (int, error)
}

func RegisterDurableScheduling(runner *durablejob.Runner, service reconciliationService, interval time.Duration) error {
	if runner == nil || service == nil {
		return fmt.Errorf("OpenClaw gateway reconciliation requires a runner and service")
	}
	return runner.RegisterRecurring(reconcileJobKind, interval, reconcileMaxAttempts, func(context.Context) error {
		_, err := service.Reconcile(batchLimit())
		return err
	})
}

func StartDurableScheduler(ctx context.Context, service *Service) error {
	if !SchedulerEnabled() {
		return nil
	}
	repository, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repository, durablejob.Options{Queue: "openclaw-gateway-reconcile"})
	if err := RegisterDurableScheduling(runner, service, interval()); err != nil {
		return err
	}
	if !lifecycle.Go(ctx, "openclaw-terminal-reconciliation", func() { runner.Start(ctx, pollInterval()) }) {
		return context.Canceled
	}
	return nil
}

// SchedulerEnabled defaults to the explicit Gateway delegation switch. This
// preserves a no-network read-only default while ensuring any enabled session
// admission has a corresponding bounded terminal-observation worker.
func SchedulerEnabled() bool {
	value, exists := os.LookupEnv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_ENABLED")
	if !exists || strings.TrimSpace(value) == "" {
		value = os.Getenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED")
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func interval() time.Duration {
	return envSeconds("HAI_OPENCLAW_GATEWAY_RECONCILIATION_SECONDS", 30, 10, 3600)
}

func pollInterval() time.Duration {
	return envSeconds("HAI_OPENCLAW_GATEWAY_RECONCILIATION_POLL_SECONDS", 10, 5, 300)
}

func batchLimit() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HAI_OPENCLAW_GATEWAY_RECONCILIATION_BATCH")))
	if err != nil || value < 1 {
		return 20
	}
	if value > 100 {
		return 100
	}
	return value
}

func staleAfter() time.Duration {
	return envSeconds("HAI_OPENCLAW_GATEWAY_RECONCILIATION_STALE_SECONDS", 15*60, 60, 24*60*60)
}

func envSeconds(name string, fallback, minimum, maximum int) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value < minimum || value > maximum {
		value = fallback
	}
	return time.Duration(value) * time.Second
}
