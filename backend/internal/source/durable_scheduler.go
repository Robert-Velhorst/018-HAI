package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"

	"github.com/google/uuid"
)

// Durable source scheduling.
//
// The legacy Scheduler (scheduler.go) is an in-process ticker: it both finds due
// sources and syncs them inside one goroutine, so a restart mid-sync loses the
// work and a failure waits a whole interval with no backoff.
//
// This replaces that with two durable job kinds:
//
//	JobKindScan — a singleton, self-rescheduling job. It finds due sources,
//	              enqueues one sync job each, then re-enqueues itself.
//	JobKindSync — syncs exactly one source. Because it is a durable job it gets
//	              bounded retry with backoff and lease-based crash recovery for
//	              free, per source rather than per sweep.
//
// A failing source therefore retries on its own backoff schedule instead of
// stalling or silently dropping the sweep.
const (
	JobKindScan = "source.scan"
	JobKindSync = "source.sync"

	// A sweep is cheap and re-runs on the next interval, so retry it only briefly.
	scanMaxAttempts = 3
	// An individual sync is worth retrying harder before dead-lettering.
	syncMaxAttempts = 5
)

// syncJobPayload identifies which source a sync job refers to.
type syncJobPayload struct {
	SourceID string `json:"sourceId"`
}

// terminalScheduledSyncFailureReporter is intentionally optional so existing
// source-service fakes and integrations remain compatible. The production
// service implements it to turn a dead-lettered connector failure into a
// reviewable workflow instead of leaving it only in the durable-job table.
type terminalScheduledSyncFailureReporter interface {
	ReportScheduledSyncTerminalFailure(sourceID uuid.UUID, reason string)
}

// startDurableScheduler builds the durable runner over the default queue,
// registers the source handlers, and starts the worker loop. Any failure is
// returned so the caller can fall back to the legacy ticker.
func startDurableScheduler(ctx context.Context, service Service, interval time.Duration, allowed ...func() bool) error {
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "source"})
	if err := RegisterDurableScheduling(runner, service, interval, allowed...); err != nil {
		return err
	}
	go runner.Start(ctx, durablePollInterval())
	return nil
}

// durablePollInterval is how often the worker checks for due jobs. It is much
// shorter than the scan interval because it also drives retries.
func durablePollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("SOURCE_WORKER_POLL_SECONDS"))
	if value == "" {
		return time.Minute
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 1 {
		return time.Minute
	}
	return time.Duration(seconds) * time.Second
}

// RegisterDurableScheduling wires the source sync handlers into a durable
// runner and makes sure exactly one scan job is scheduled. Call it once at
// startup; it is safe across restarts because the scan job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service Service, interval time.Duration, allowed ...func() bool) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable scheduling needs both a runner and a source service")
	}
	if interval < 15*time.Second {
		interval = 10 * time.Minute
	}

	backgroundAllowed := schedulerBackgroundGate(allowed)
	runner.Register(JobKindSync, syncHandler(service, backgroundAllowed))
	if err := runner.RegisterRecurring(JobKindScan, interval, scanMaxAttempts, scanWork(runner, service, backgroundAllowed)); err != nil {
		return fmt.Errorf("schedule source scan: %w", err)
	}
	log.Printf("source scheduler: durable scan job scheduled (interval %s)", interval)
	return nil
}

// scanWork finds due sources and enqueues a durable sync job for each. The
// recurring wrapper owns rescheduling. Enqueuing is safe to repeat: Sync refuses
// concurrent runs for the same source and upserts by external id, so a retried
// scan cannot corrupt state.
func scanWork(runner *durablejob.Runner, service Service, allowed ...func() bool) func(context.Context) error {
	backgroundAllowed := schedulerBackgroundGate(allowed)
	return func(ctx context.Context) error {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now().UTC()
		due, err := service.DueSources(now)
		if err != nil {
			return fmt.Errorf("list due sources: %w", err)
		}
		enqueued := 0
		for _, item := range due {
			if err := ctx.Err(); err != nil {
				return err
			}
			// The payload is also the durable deduplication key. Keep it to the
			// immutable source identity so renaming a source cannot queue a second
			// active sync for the same record.
			payload, errMarshal := json.Marshal(syncJobPayload{SourceID: item.ID.String()})
			if errMarshal != nil {
				return fmt.Errorf("encode sync payload for %s: %w", item.Name, errMarshal)
			}
			created, errEnqueue := runner.EnsureScheduledForPayload(JobKindSync, string(payload), now, syncMaxAttempts)
			if errEnqueue != nil {
				return fmt.Errorf("enqueue sync for %s: %w", item.Name, errEnqueue)
			}
			if !created {
				continue
			}
			enqueued++
		}
		if enqueued > 0 {
			log.Printf("source scheduler: enqueued %d durable sync job(s)", enqueued)
		}
		return nil
	}
}

// syncHandler syncs the single source named in the payload. Returning an error
// hands the job back to the durable retry/backoff policy.
func syncHandler(service Service, allowed ...func() bool) durablejob.Handler {
	backgroundAllowed := schedulerBackgroundGate(allowed)
	return func(ctx context.Context, job durablejob.Job) error {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		var payload syncJobPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			// Malformed payload will never succeed; surface it so the job
			// dead-letters instead of retrying forever.
			return fmt.Errorf("decode sync payload: %w", err)
		}
		sourceID, err := uuid.Parse(strings.TrimSpace(payload.SourceID))
		if err != nil {
			return fmt.Errorf("sync payload has an invalid source id %q: %w", payload.SourceID, err)
		}
		result, err := service.SyncContext(ctx, sourceID, ImportRequest{Mode: ModeScheduledSync})
		if err != nil {
			if errors.Is(err, ErrSyncInProgress) {
				// Another worker or an operator is already syncing this source.
				// Not a failure: the next scan will pick it up if still due.
				return nil
			}
			if errors.Is(err, ErrSourceNotEnabled) {
				// The source was paused or revoked after this job was queued. That
				// is an intentional operator state change, not a retryable fault.
				return nil
			}
			if job.Attempts+1 >= job.MaxAttempts {
				reportTerminalScheduledSyncFailure(service, sourceID, err.Error())
			}
			return err
		}
		if result != nil && result.Job.Status != "completed" {
			// Partial failures keep the cursor; retrying is the correct response.
			failure := fmt.Errorf("sync source %s finished with status %s: %s", sourceID, result.Job.Status, result.Job.Message)
			if job.Attempts+1 >= job.MaxAttempts {
				reportTerminalScheduledSyncFailure(service, sourceID, failure.Error())
			}
			return failure
		}
		return nil
	}
}

func reportTerminalScheduledSyncFailure(service Service, sourceID uuid.UUID, reason string) {
	reporter, ok := service.(terminalScheduledSyncFailureReporter)
	if !ok {
		return
	}
	reporter.ReportScheduledSyncTerminalFailure(sourceID, reason)
}
