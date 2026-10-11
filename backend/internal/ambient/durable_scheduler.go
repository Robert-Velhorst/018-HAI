package ambient

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

// Durable ambient scanning.
//
// Replaces the in-process ticker with a durable, self-rescheduling job so an
// ambient scan survives a restart, retries on backoff, and is recovered by
// review if the worker dies mid-scan; an unknown result is never replay-safe.
const (
	JobKindScan            = "ambient.scan"
	scanMaxAttempts        = 3
	defaultAmbientPoll     = 5 * time.Minute
	minAmbientPollInterval = 15 * time.Second
	maxAmbientPollInterval = time.Hour
)

// RegisterDurableScheduling registers the ambient scan as a durable recurring
// job. Safe to call on every startup: the job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service Service, interval time.Duration, allowed ...func() bool) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable ambient scheduling needs both a runner and a service")
	}
	if _, ok := service.(ContextualScanService); !ok {
		return ErrScanContextUnavailable
	}
	if !runner.SupportsManualReview() {
		return durablejob.ErrReviewPersistenceUnavailable
	}
	backgroundAllowed := schedulerBackgroundGate(allowed)
	if err := runner.RegisterReviewRecurring(JobKindScan, interval, scanMaxAttempts, func(ctx context.Context) error {
		return runAmbientScan(ctx, service, backgroundAllowed)
	}); err != nil {
		return err
	}
	log.Printf("ambient scheduler: durable scan job scheduled (interval %s)", interval)
	return nil
}

// runAmbientScan performs one ambient scan. Returning an error hands the job to
// the durable retry/backoff policy instead of dropping the interval.
type ambientScanError struct{ cause error }

func (e *ambientScanError) Error() string {
	return "ambient scan: " + safety.RedactSecrets(e.cause.Error())
}
func (e *ambientScanError) Unwrap() error { return e.cause }
func (e *ambientScanError) RequiresManualReview() bool {
	return errors.Is(e.cause, ErrScanOutcomeUnconfirmed) || durablejob.RequiresManualReview(e.cause)
}

func runAmbientScan(ctx context.Context, service Service, allowed ...func() bool) error {
	gate := schedulerBackgroundGate(allowed)
	if err := scanCheckpoint(ctx, gate); err != nil {
		return err
	}
	contextual, ok := service.(ContextualScanService)
	if !ok {
		return ErrScanContextUnavailable
	}
	scan, err := contextual.ScanContext(ctx, "scheduler", gate)
	if scan != nil && scan.Status == "outcome_unconfirmed" {
		err = errors.Join(ErrScanOutcomeUnconfirmed, err)
	}
	if err != nil {
		return &ambientScanError{cause: err}
	}
	if scan == nil || scan.Status != "completed" {
		return &ambientScanError{cause: ErrScanOutcomeUnconfirmed}
	}
	if err := scanCheckpoint(ctx, gate); err != nil {
		return err
	}
	if scan != nil && (scan.Created > 0 || scan.Updated > 0 || scan.Advanced > 0) {
		log.Printf("ambient scan examined=%d created=%d updated=%d deduplicated=%d advanced=%d filtered=%d skipped=%d blocked=%d",
			scan.ItemsExamined, scan.Created, scan.Updated, scan.Deduplicated, scan.Advanced, scan.Filtered, scan.Skipped, scan.Blocked)
	}
	return nil
}

// startDurableScheduler builds the runner over the default queue and starts it.
// Any failure is returned; startup must not bypass durable review with a ticker.
func startDurableScheduler(ctx context.Context, service Service, interval time.Duration, allowed ...func() bool) error {
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "ambient"})
	if err := RegisterDurableScheduling(runner, service, interval, allowed...); err != nil {
		return err
	}
	if !lifecycle.Go(ctx, "ambient-durable-worker", func() { runner.Start(ctx, ambientPollInterval()) }) {
		return context.Canceled
	}
	return nil
}

func ambientPollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("AMBIENT_WORKER_POLL_SECONDS"))
	if value == "" {
		return defaultAmbientPoll
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < int64(minAmbientPollInterval/time.Second) || seconds > int64(maxAmbientPollInterval/time.Second) {
		return defaultAmbientPoll
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
