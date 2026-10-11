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
	Name     string `json:"name,omitempty"`
}

// startDurableScheduler builds the durable runner over the default queue,
// registers the source handlers, and starts the worker loop. Any failure is
// returned so the caller can fall back to the legacy ticker.
func startDurableScheduler(ctx context.Context, service Service, interval time.Duration) error {
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "source"})
	if err := RegisterDurableScheduling(runner, service, interval); err != nil {
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
		return 15 * time.Second
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < 1 {
		return 15 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// RegisterDurableScheduling wires the source sync handlers into a durable
// runner and makes sure exactly one scan job is scheduled. Call it once at
// startup; it is safe across restarts because the scan job is a singleton.
func RegisterDurableScheduling(runner *durablejob.Runner, service Service, interval time.Duration) error {
	if runner == nil || service == nil {
		return fmt.Errorf("durable scheduling needs both a runner and a source service")
	}
	if interval < 15*time.Second {
		interval = 10 * time.Minute
	}

	runner.Register(JobKindSync, syncHandler(service))
	if err := runner.RegisterRecurring(JobKindScan, interval, scanMaxAttempts, scanWork(runner, service)); err != nil {
		return fmt.Errorf("schedule source scan: %w", err)
	}
	log.Printf("source scheduler: durable scan job scheduled (interval %s)", interval)
	return nil
}

// scanWork finds due sources and enqueues a durable sync job for each. The
// recurring wrapper owns rescheduling. Enqueuing is safe to repeat: Sync refuses
// concurrent runs for the same source and upserts by external id, so a retried
// scan cannot corrupt state.
func scanWork(runner *durablejob.Runner, service Service) func(context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().UTC()
		due, err := service.DueSources(now)
		if err != nil {
			return fmt.Errorf("list due sources: %w", err)
		}
		for _, item := range due {
			payload, errMarshal := json.Marshal(syncJobPayload{SourceID: item.ID.String(), Name: item.Name})
			if errMarshal != nil {
				return fmt.Errorf("encode sync payload for %s: %w", item.Name, errMarshal)
			}
			if _, errEnqueue := runner.Enqueue(JobKindSync, string(payload), now, syncMaxAttempts); errEnqueue != nil {
				return fmt.Errorf("enqueue sync for %s: %w", item.Name, errEnqueue)
			}
		}
		if len(due) > 0 {
			log.Printf("source scheduler: enqueued %d durable sync job(s)", len(due))
		}
		return nil
	}
}

// syncHandler syncs the single source named in the payload. Returning an error
// hands the job back to the durable retry/backoff policy.
func syncHandler(service Service) durablejob.Handler {
	return func(ctx context.Context, job durablejob.Job) error {
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
			return err
		}
		if result != nil && result.Job.Status != "completed" {
			// Partial failures keep the cursor; retrying is the correct response.
			return fmt.Errorf("sync %s finished with status %s: %s", payload.Name, result.Job.Status, result.Job.Message)
		}
		return nil
	}
}
