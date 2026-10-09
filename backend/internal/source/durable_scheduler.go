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
	"automation-hub-backend/internal/lifecycle"

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
// Once a source sync exhausts its attempts, the scanner waits one configured
// source interval before starting a new cycle, preserving recovery without a
// tight loop of freshly dead-lettered jobs.
const (
	JobKindScan = "source.scan"
	JobKindSync = "source.sync"

	// A sweep is cheap and re-runs on the next interval, so retry it only briefly.
	scanMaxAttempts = 3
	// An individual sync is worth retrying harder before dead-lettering.
	syncMaxAttempts            = 5
	defaultDurablePollInterval = 5 * time.Minute
	minDurablePollInterval     = 15 * time.Second
	maxDurablePollInterval     = time.Hour
	syncLeaseDeferredReason    = "source sync is already in progress; retry after the active sync finishes"
)

// syncJobPayload identifies which source a sync job refers to.
type syncJobPayload struct {
	SourceID string `json:"sourceId"`
}

// startDurableScheduler builds the durable runner over the default queue,
// registers the source handlers, and starts the worker loop. Any failure is
// returned so the caller can fall back to the legacy ticker.
func startDurableScheduler(ctx context.Context, service Service, interval time.Duration, allowed ...func() bool) error {
	setManualSyncWorkerReady(service, false)
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "source"})
	if err := RegisterDurableScheduling(runner, service, interval, allowed...); err != nil {
		return err
	}
	setManualSyncWorkerReady(service, true)
	if !lifecycle.Go(ctx, "source-durable-worker", func() {
		defer setManualSyncWorkerReady(service, false)
		runner.Start(ctx, durablePollInterval())
	}) {
		setManualSyncWorkerReady(service, false)
		return context.Canceled
	}
	return nil
}

type manualSyncWorkerReadySetter interface {
	setManualSyncWorkerReady(ready bool)
}

func setManualSyncWorkerReady(service Service, ready bool) {
	if setter, ok := service.(manualSyncWorkerReadySetter); ok {
		setter.setManualSyncWorkerReady(ready)
	}
}

// durablePollInterval bounds recovery and retry latency when no immediate
// worker pass is available. The runner performs a pass at startup, while
// recurring work holds its own due time, so a five-minute idle interval avoids
// continuous database wake-ups on an empty local installation.
func durablePollInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("SOURCE_WORKER_POLL_SECONDS"))
	if value == "" {
		return defaultDurablePollInterval
	}
	var seconds int64
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds < int64(minDurablePollInterval/time.Second) || seconds > int64(maxDurablePollInterval/time.Second) {
		return defaultDurablePollInterval
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
	runner.Register(JobKindManualSync, manualSyncHandler(service, backgroundAllowed))
	runner.Register(JobKindTrelloWebhook, trelloWebhookHandler(service, backgroundAllowed))
	runner.Register(JobKindExtractionCorrection, extractionCorrectionJobHandler(service, backgroundAllowed))
	if err := runner.RegisterRecurring(JobKindScan, interval, scanMaxAttempts, scanWork(runner, service, backgroundAllowed)); err != nil {
		return fmt.Errorf("schedule source scan: %w", err)
	}
	log.Printf("source scheduler: durable scan job scheduled (interval %s)", interval)
	return nil
}

func manualSyncHandler(service Service, allowed ...func() bool) durablejob.Handler {
	backgroundAllowed := schedulerBackgroundGate(allowed)
	return func(ctx context.Context, job durablejob.Job) error {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		var payload manualSyncPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return fmt.Errorf("decode manual source sync payload: %w", err)
		}
		executor, ok := service.(manualSyncExecutionService)
		if !ok {
			return errors.New("manual source sync worker is unavailable")
		}
		return executor.RunManualSyncJob(ctx, payload, job.ID, job.Attempts+1, job.MaxAttempts)
	}
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
			latestDead, errHistory := runner.FindLatestDeadForPayload(JobKindSync, string(payload))
			if errHistory != nil {
				return fmt.Errorf("check dead-letter cooldown for %s: %w", item.Name, errHistory)
			}
			if latestDead != nil && sourceDeadLetterCooldownActive(latestDead.CompletedAt, item.SyncFrequency, now) {
				continue
			}
			if _, errEnqueue := runner.EnsureScheduledForPayload(JobKindSync, string(payload), now, syncMaxAttempts); errEnqueue != nil {
				return fmt.Errorf("enqueue sync for %s: %w", item.Name, errEnqueue)
			}
		}
		if len(due) > 0 {
			log.Printf("source scheduler: enqueued %d durable sync job(s)", len(due))
		}
		return nil
	}
}

func sourceDeadLetterCooldownActive(completedAt *time.Time, syncFrequency string, now time.Time) bool {
	if completedAt == nil {
		return true
	}
	interval, ok := parseSyncFrequency(syncFrequency)
	if !ok {
		return true
	}
	return now.Before(completedAt.Add(interval))
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
		// Deferred scheduled jobs are retried only if the source is still due.
		// Keep this read off the normal path: DueSources enumerates all enabled
		// sources, which would multiply scan work per ordinary sync job.
		if strings.HasPrefix(job.LastError, "job deferred:") {
			dueSources, err := service.DueSources(time.Now().UTC())
			if err != nil {
				return fmt.Errorf("%s; source due-state recheck failed: %w", job.LastError, err)
			}
			due := false
			for _, source := range dueSources {
				if source.ID == sourceID {
					due = true
					break
				}
			}
			if !due {
				return nil
			}
		}
		result, err := syncSourceWithContext(ctx, service, sourceID, ImportRequest{Mode: ModeScheduledSync})
		if err != nil {
			if errors.Is(err, ErrSyncInProgress) {
				// Keep this durable intent alive without consuming a provider retry.
				// On the next attempt, the due-state check above will either retry
				// after a failed concurrent sync or discard stale work after success.
				return durablejob.Defer(syncLeaseDeferredReason)
			}
			return err
		}
		if result != nil && result.Job.Status != "completed" {
			if result.Job.Status == "running" {
				return durablejob.Defer("Trello sync has additional durable pages to process")
			}
			// Partial failures keep the cursor; retrying is the correct response.
			return fmt.Errorf("sync source %s finished with status %s: %s", payload.SourceID, result.Job.Status, result.Job.Message)
		}
		return nil
	}
}
