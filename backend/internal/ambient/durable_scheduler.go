package ambient

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
)

// Durable ambient scanning.
//
// Replaces the in-process ticker with a durable, self-rescheduling job so an
// ambient scan survives a restart, retries on backoff, and is recovered by
// lease if the worker dies mid-scan.
const (
	JobKindScan     = "ambient.scan"
	scanMaxAttempts = 3
)

// RegisterDurableScheduling registers the ambient scan as a durable recurring
// job. Safe to call on every startup: the job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service Service, interval time.Duration, allowed ...func() bool) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable ambient scheduling needs both a runner and a service")
	}
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if err := runner.RegisterRecurring(JobKindScan, interval, scanMaxAttempts, func(ctx context.Context) error {
		if !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		return runAmbientScan(service)
	}); err != nil {
		return err
	}
	log.Printf("ambient scheduler: durable scan job scheduled (interval %s)", interval)
	return nil
}

// runAmbientScan performs one ambient scan. Returning an error hands the job to
// the durable retry/backoff policy instead of dropping the interval.
func runAmbientScan(service Service) error {
	scan, err := service.Scan("scheduler")
	if err != nil {
		return fmt.Errorf("ambient scan: %w", err)
	}
	if scan != nil && (scan.Created > 0 || scan.Updated > 0 || scan.Advanced > 0) {
		log.Printf("ambient scan examined=%d created=%d updated=%d deduplicated=%d advanced=%d filtered=%d skipped=%d blocked=%d",
			scan.ItemsExamined, scan.Created, scan.Updated, scan.Deduplicated, scan.Advanced, scan.Filtered, scan.Skipped, scan.Blocked)
	}
	return nil
}

// startDurableScheduler builds the runner over the default queue and starts it.
// Any failure is returned so the caller can fall back to the legacy ticker.
func startDurableScheduler(ctx context.Context, service Service, interval time.Duration, allowed ...func() bool) error {
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "ambient"})
	if err := RegisterDurableScheduling(runner, service, interval, allowed...); err != nil {
		return err
	}
	go runner.Start(ctx, ambientPollInterval())
	return nil
}

func ambientPollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("AMBIENT_WORKER_POLL_SECONDS"))
	if value == "" {
		return 15 * time.Second
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 1 {
		return 15 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func durableSchedulerEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("AMBIENT_SCHEDULER_DURABLE"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}
