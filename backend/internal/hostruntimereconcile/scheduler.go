package hostruntimereconcile

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
	reconcileJobKind     = "host-runtime.reconcile-completions"
	reconcileMaxAttempts = 3
)

type reconciliationService interface {
	ReconcileCompleted(limit int) (int, error)
}

// RegisterDurableScheduling uses HAI's existing durable queue so terminal host
// results survive backend restarts and are retried without re-executing work.
func RegisterDurableScheduling(runner *durablejob.Runner, service reconciliationService, interval time.Duration) error {
	if runner == nil || service == nil {
		return fmt.Errorf("host runtime reconciliation requires a runner and service")
	}
	return runner.RegisterRecurring(reconcileJobKind, interval, reconcileMaxAttempts, func(context.Context) error {
		_, err := service.ReconcileCompleted(batchLimit())
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
	runner := durablejob.NewRunner(repository, durablejob.Options{Queue: "host-runtime-reconcile"})
	if err := RegisterDurableScheduling(runner, service, interval()); err != nil {
		return err
	}
	if !lifecycle.Go(ctx, "host-runtime-reconciliation", func() { runner.Start(ctx, pollInterval()) }) {
		return context.Canceled
	}
	return nil
}

func SchedulerEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_RECONCILIATION_ENABLED")))
	return value == "" || value == "1" || value == "true" || value == "yes"
}

func interval() time.Duration {
	return envSeconds("HAI_HOST_RUNTIME_RECONCILIATION_SECONDS", 30, 10, 3600)
}
func pollInterval() time.Duration {
	return envSeconds("HAI_HOST_RUNTIME_RECONCILIATION_POLL_SECONDS", 10, 5, 300)
}
func batchLimit() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_RECONCILIATION_BATCH")))
	if err != nil || value < 1 {
		return 20
	}
	if value > 100 {
		return 100
	}
	return value
}

func envSeconds(name string, fallback, minimum, maximum int) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value < minimum || value > maximum {
		value = fallback
	}
	return time.Duration(value) * time.Second
}
