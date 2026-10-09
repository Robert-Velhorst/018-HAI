package durablejob

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"automation-hub-backend/internal/backoff"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
)

// Job is the unit of work handed to a Handler. Aliased so callers registering
// handlers do not need to import the models package.
type Job = models.DurableJob

// Handler executes one job. It must be idempotent, using job.ID as the stable
// idempotency key for external side effects: delivery is at-least-once, so a
// handler can run again if a worker dies after the side effect but before the
// status write or the outcome of an expired lease is unknown.
type Handler func(ctx context.Context, job Job) error

// DefaultLease is how long a claimed job may be held before another worker is
// allowed to assume the holder died and reclaim it.
const DefaultLease = 5 * time.Minute

// DefaultDeferDelay prevents a paused policy gate from repeatedly claiming the
// same job while retaining it for the next permitted worker pass.
const DefaultDeferDelay = time.Minute

// MaxRetryAfter bounds provider-supplied retry metadata. A larger valid delay
// is treated as non-retryable so the worker cannot retry before the provider's
// requested time by clamping it to this limit.
const MaxRetryAfter = 24 * time.Hour

// RetryClassification is an optional handler error contract. Errors without
// this method retain the runner's existing retry behavior.
type RetryClassification interface {
	Retryable() bool
}

// RetryAfterMetadata carries an optional minimum delay before retrying.
type RetryAfterMetadata interface {
	RetryAfter() time.Duration
}

var ErrManualReviewRequired = errors.New("background work requires manual reconciliation")
var ErrReviewPersistenceUnavailable = errors.New("durable review persistence is unavailable or unconfirmed")
var ErrJobSettlementUnconfirmed = errors.New("durable job outcome write is unconfirmed")
var ErrQueueScopedLeaseRecoveryUnavailable = errors.New("queue-scoped durable job lease recovery is unavailable")

var errHandlerAdmissionClosed = errors.New("handler ownership admission is closed")

// ManualReviewClassification takes precedence over a joined policy deferral.
// An unknown outcome is not an ordinary pause, retry or exhausted recurrence.
type ManualReviewClassification interface{ RequiresManualReview() bool }

func RequiresManualReview(err error) bool {
	var review ManualReviewClassification
	if errors.As(err, &review) && review.RequiresManualReview() {
		return true
	}
	// A false classifier on one joined branch must not mask uncertainty on
	// another branch (for example a pause joined with a failed receipt write).
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			if RequiresManualReview(child) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return RequiresManualReview(wrapped.Unwrap())
	}
	return false
}

func (r *Runner) SupportsManualReview() bool {
	if r == nil {
		return false
	}
	_, ok := r.repo.(ManualReviewRepository)
	_, scheduled := r.repo.(ReviewSchedulingRepository)
	_, completion := r.repo.(ReviewRecurringRepository)
	return ok && scheduled && completion
}

type reviewResultError struct{ cause error }

func (e *reviewResultError) Error() string { return safety.RedactSecrets(e.cause.Error()) }
func (e *reviewResultError) Unwrap() error { return e.cause }

type invocationUnconfirmedError struct{ cause error }

func (e *invocationUnconfirmedError) Error() string            { return safety.RedactSecrets(e.cause.Error()) }
func (e *invocationUnconfirmedError) Unwrap() error            { return e.cause }
func (*invocationUnconfirmedError) RequiresManualReview() bool { return true }

func (r *Runner) holdForReview(ctx context.Context, job models.DurableJob, attempt int, cause error) error {
	repository, ok := r.repo.(ManualReviewRepository)
	if !ok {
		return &reviewResultError{cause: errors.Join(ErrReviewPersistenceUnavailable, cause)}
	}
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	owned, err := repository.MarkForReview(settle, job.ID, r.workerID, job.LeaseGeneration, r.now(), attempt, safety.RedactSecrets(cause.Error()))
	if err != nil || !owned {
		return &reviewResultError{cause: errors.Join(ErrReviewPersistenceUnavailable, cause, err)}
	}
	return &reviewResultError{cause: errors.Join(ErrManualReviewRequired, cause)}
}

func (r *Runner) settlementResult(ctx context.Context, job models.DurableJob, attempts int, acknowledged bool, err error) error {
	if acknowledged && err == nil {
		return nil
	}
	cause := errors.Join(ErrJobSettlementUnconfirmed, err)
	if job.ReplayPolicy == models.DurableJobReviewUnknown {
		return r.holdForReview(ctx, job, attempts, cause)
	}
	return &reviewResultError{cause: cause}
}

// DeferredError tells the runner that work was deliberately postponed by a
// safety or availability gate. It is neither a success nor a retryable failure:
// attempts remain unchanged and the job stays pending for a later pass.
type DeferredError struct {
	Reason string
}

func (e *DeferredError) Error() string {
	if e == nil || e.Reason == "" {
		return "job deferred"
	}
	return "job deferred: " + e.Reason
}

// Defer returns a typed handler result for work that must wait without
// consuming the job retry budget, such as an emergency stop or paused mode.
func Defer(reason string) error { return &DeferredError{Reason: reason} }

// Runner claims due jobs, executes their handler, and applies the retry policy.
// It is safe to run several Runners (in one process or many) against the same
// queue: claiming uses FOR UPDATE SKIP LOCKED.
type Runner struct {
	repo     Repository
	policy   backoff.Policy
	workerID string
	queue    string
	lease    time.Duration
	batch    int
	// now is injectable so retry scheduling is deterministic in tests.
	now func() time.Time

	mu        sync.RWMutex
	handlers  map[string]Handler
	recurring map[string]recurringSchedule
	statusMu  sync.RWMutex
	status    RunnerStatus
}

type recurringSchedule struct {
	interval     time.Duration
	maxAttempts  int
	payload      string
	replayPolicy string
}

// RunnerStatus provides bounded in-process lifecycle telemetry for a durable
// queue worker. It deliberately excludes job payloads and handler output.
type RunnerStatus struct {
	Running           bool
	StartedAt         time.Time
	LastPollAt        time.Time
	LastSuccessfulAt  time.Time
	LastError         string
	ConsecutiveErrors int
}

// Options configures a Runner. Zero values fall back to sane defaults.
type Options struct {
	WorkerID string
	Queue    string
	Policy   backoff.Policy
	Lease    time.Duration
	Batch    int
	Now      func() time.Time
}

// NewRunner builds a Runner over the given repository.
func NewRunner(repo Repository, opts Options) *Runner {
	if opts.WorkerID == "" {
		opts.WorkerID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	if opts.Queue == "" {
		opts.Queue = "default"
	}
	if opts.Policy == (backoff.Policy{}) {
		opts.Policy = backoff.DefaultPolicy()
	}
	if opts.Lease <= 0 {
		opts.Lease = DefaultLease
	}
	if opts.Batch <= 0 {
		opts.Batch = 10
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Runner{
		repo:      repo,
		policy:    opts.Policy,
		workerID:  opts.WorkerID,
		queue:     opts.Queue,
		lease:     opts.Lease,
		batch:     opts.Batch,
		now:       opts.Now,
		handlers:  map[string]Handler{},
		recurring: map[string]recurringSchedule{},
	}
}

// Register binds a handler to a job kind. Registering an existing kind replaces it.
func (r *Runner) Register(kind string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = handler
}

func (r *Runner) handlerFor(kind string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[kind]
	return handler, ok
}

func (r *Runner) recurringFor(kind string) (recurringSchedule, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schedule, ok := r.recurring[kind]
	return schedule, ok
}

// Status returns a snapshot suitable for diagnostics. It is intentionally
// separate from persisted job state: jobs remain the audit source of truth.
func (r *Runner) Status() RunnerStatus {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()
	return r.status
}

// Enqueue schedules a job. runAt zero means "run as soon as possible".
func (r *Runner) Enqueue(kind, payload string, runAt time.Time, maxAttempts int) (*models.DurableJob, error) {
	if runAt.IsZero() {
		runAt = r.now()
	}
	return r.repo.Enqueue(&models.DurableJob{
		Queue:        r.queue,
		Kind:         kind,
		Payload:      payload,
		RunAt:        runAt.UTC(),
		MaxAttempts:  maxAttempts,
		Status:       models.DurableJobPending,
		ReplayPolicy: r.replayPolicyFor(kind),
	})
}

// FindLatestDeadForPayload exposes terminal failure metadata for one stable
// work item so scanners can avoid immediately creating unlimited fresh retry
// cycles after a bounded job is dead-lettered.
func (r *Runner) FindLatestDeadForPayload(kind, payload string) (*Job, error) {
	return r.repo.FindLatestDeadByPayload(r.queue, kind, payload)
}

// EnsureScheduled enqueues a job of the given kind only when none is already
// pending or running. Recurring work (a periodic scan that re-enqueues itself)
// uses this at startup so restarts do not pile up duplicate schedules.
// It reports whether a new job was created.
func (r *Runner) EnsureScheduled(kind, payload string, runAt time.Time, maxAttempts int) (bool, error) {
	return r.ensureScheduled(kind, payload, runAt, maxAttempts, false)
}

// EnsureScheduledForPayload is the per-work-item counterpart to
// EnsureScheduled. It permits different payloads of one kind to run in
// parallel, while ensuring a periodic producer cannot enqueue duplicates for
// the same work item.
func (r *Runner) EnsureScheduledForPayload(kind, payload string, runAt time.Time, maxAttempts int) (bool, error) {
	return r.ensureScheduled(kind, payload, runAt, maxAttempts, true)
}

func (r *Runner) ensureScheduled(kind, payload string, runAt time.Time, maxAttempts int, matchPayload bool) (bool, error) {
	return r.ensureScheduledWithPolicy(kind, payload, runAt, maxAttempts, matchPayload, r.replayPolicyFor(kind))
}

func (r *Runner) replayPolicyFor(kind string) string {
	if schedule, ok := r.recurringFor(kind); ok && schedule.replayPolicy == models.DurableJobReviewUnknown {
		return models.DurableJobReviewUnknown
	}
	return models.DurableJobReplayAtLeastOnce
}

func (r *Runner) ensureScheduledWithPolicy(kind, payload string, runAt time.Time, maxAttempts int, matchPayload bool, replayPolicy string) (bool, error) {
	if runAt.IsZero() {
		runAt = r.now()
	}
	job := &models.DurableJob{
		Queue:        r.queue,
		Kind:         kind,
		Payload:      payload,
		RunAt:        runAt.UTC(),
		MaxAttempts:  maxAttempts,
		Status:       models.DurableJobPending,
		ReplayPolicy: replayPolicy,
	}
	var (
		created bool
		err     error
	)
	if replayPolicy == models.DurableJobReviewUnknown {
		repository, ok := r.repo.(ReviewSchedulingRepository)
		if !ok || matchPayload {
			return false, ErrReviewPersistenceUnavailable
		}
		created, err = repository.EnqueueReviewIfNoActive(job)
	} else if matchPayload {
		created, err = r.repo.EnqueueIfNoActiveMatchingPayload(job)
	} else {
		created, err = r.repo.EnqueueIfNoActive(job)
	}
	if err != nil {
		return false, fmt.Errorf("ensure active %s job: %w", kind, err)
	}
	return created, nil
}

// RegisterRecurring turns a periodic task into a durable, self-rescheduling
// singleton job — the replacement for an in-process ticker. The work survives
// restarts and gets bounded retry with backoff.
//
// The next occurrence is scheduled when the work succeeds *or* when this was its
// final attempt. That matters: rescheduling only on success would mean a short
// burst of failures dead-letters the job and silently kills the recurring
// schedule forever. Here a failing run still retries on backoff, and the
// schedule always continues.
func (r *Runner) RegisterRecurring(kind string, interval time.Duration, maxAttempts int, work func(ctx context.Context) error) error {
	return r.registerRecurring(kind, interval, maxAttempts, models.DurableJobReplayAtLeastOnce, work)
}

// RegisterReviewRecurring never automatically repeats an invocation whose
// outcome was lost to cancellation, failed settlement or an expired lease.
func (r *Runner) RegisterReviewRecurring(kind string, interval time.Duration, maxAttempts int, work func(context.Context) error) error {
	if !r.SupportsManualReview() {
		return ErrReviewPersistenceUnavailable
	}
	return r.registerRecurring(kind, interval, maxAttempts, models.DurableJobReviewUnknown, work)
}

func (r *Runner) registerRecurring(kind string, interval time.Duration, maxAttempts int, replayPolicy string, work func(context.Context) error) error {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	// Prevent a live poll from observing the newly enqueued occurrence before
	// its handler and policy have been published together.
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.recurring[kind]; ok && previous.replayPolicy == models.DurableJobReviewUnknown {
		replayPolicy = models.DurableJobReviewUnknown
	}
	if replayPolicy == models.DurableJobReviewUnknown {
		if _, err := r.repo.(ReviewSchedulingRepository).EnqueueReviewIfNoActive(&models.DurableJob{
			Queue: r.queue, Kind: kind, Payload: "{}", RunAt: r.now(), MaxAttempts: maxAttempts,
			Status: models.DurableJobPending, ReplayPolicy: replayPolicy,
		}); err != nil {
			return &reviewResultError{cause: errors.Join(ErrReviewPersistenceUnavailable, err)}
		}
	} else if _, err := r.ensureScheduledWithPolicy(kind, "{}", r.now(), maxAttempts, false, replayPolicy); err != nil {
		return fmt.Errorf("schedule %s: %w", kind, err)
	}
	r.handlers[kind] = func(ctx context.Context, _ Job) error { return work(ctx) }
	r.recurring[kind] = recurringSchedule{interval: interval, maxAttempts: maxAttempts, payload: "{}", replayPolicy: replayPolicy}
	return nil
}

// RunOnce performs a single durable cycle: recover leases abandoned by dead
// workers, claim the due batch, and execute it. It returns how many jobs were
// processed. Handler failures are recorded as retries/dead-letters, not
// returned, so one bad job cannot stall the queue.
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := r.now()
	r.mu.RLock()
	schedules := make(map[string]recurringSchedule, len(r.recurring))
	for kind, schedule := range r.recurring {
		schedules[kind] = schedule
	}
	r.mu.RUnlock()
	var reapErr error
	if reaper, ok := r.repo.(interface {
		ReapExpiredLeasesWithRecurring(string, time.Time, time.Duration, map[string]recurringSchedule) (int, error)
	}); ok && len(schedules) > 0 {
		_, reapErr = reaper.ReapExpiredLeasesWithRecurring(r.queue, now, r.lease, schedules)
	} else if reaper, ok := r.repo.(interface {
		ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error)
	}); ok {
		_, reapErr = reaper.ReapExpiredLeasesForQueue(r.queue, now, r.lease)
	} else {
		// A global reaper can apply this queue's lease duration to another
		// queue's live jobs. Repositories without queue-scoped recovery must
		// fail closed rather than risk reclaiming work still owned elsewhere.
		reapErr = ErrQueueScopedLeaseRecoveryUnavailable
	}
	if reapErr != nil {
		return 0, fmt.Errorf("reap expired leases: %w", reapErr)
	}
	processed := 0
	var firstErr error
	seen := make(map[string]struct{}, r.batch)
	for processed < r.batch {
		if err := ctx.Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		jobs, err := r.repo.ClaimDue(r.workerID, r.queue, r.now(), 1)
		if err != nil {
			return processed, fmt.Errorf("claim due jobs: %w", err)
		}
		if len(jobs) == 0 {
			break
		}
		job := jobs[0]
		jobID := job.ID.String()
		if _, alreadyProcessed := seen[jobID]; alreadyProcessed {
			// A zero-delay retry can become claimable again during this pass. Put
			// it back without replacing the failure that explains the retry.
			reason := job.LastError
			if reason == "" {
				reason = "already processed during this worker pass"
			}
			owned, releaseErr := r.repo.MarkDeferred(job.ID, r.workerID, job.LeaseGeneration, job.RunAt, reason)
			if settleErr := r.settlementResult(ctx, job, job.Attempts, owned, releaseErr); settleErr != nil {
				return processed, &reviewResultError{cause: errors.Join(firstErr, settleErr)}
			}
			break
		}
		seen[jobID] = struct{}{}
		if err := r.execute(ctx, job); err != nil && firstErr == nil {
			firstErr = err
		}
		processed++
	}
	return processed, firstErr
}

// execute runs one claimed job and records the outcome.
func (r *Runner) execute(ctx context.Context, job models.DurableJob) error {
	if err := ctx.Err(); err != nil {
		// This handler has not started, so releasing the claim is safe and avoids
		// delaying another worker until the lease expires during shutdown.
		owned, releaseErr := r.repo.MarkDeferred(job.ID, r.workerID, job.LeaseGeneration, r.now(), "worker stopped before handler start")
		if settleErr := r.settlementResult(ctx, job, job.Attempts, owned, releaseErr); settleErr != nil {
			return &reviewResultError{cause: errors.Join(err, settleErr)}
		}
		return err
	}
	attempt := job.Attempts + 1
	handler, ok := r.handlerFor(job.Kind)
	if !ok {
		if job.ReplayPolicy == models.DurableJobReviewUnknown {
			return r.holdForReview(ctx, job, job.Attempts, fmt.Errorf("no handler registered for review-sensitive kind %q", job.Kind))
		}
		// An unregistered kind is a deployment error, not a transient fault:
		// fail it straight to the dead letter rather than retrying forever.
		owned, err := r.repo.MarkDead(job.ID, r.workerID, job.LeaseGeneration, r.now(), attempt, fmt.Sprintf("no handler registered for kind %q", job.Kind))
		return r.settlementResult(ctx, job, attempt, owned, err)
	}

	err, ownsLease, heartbeatErr := r.safeInvokeWithHeartbeat(ctx, handler, job)
	if errors.Is(err, errHandlerAdmissionClosed) {
		// Ownership refused the launch. No handler ran, so a confirmed release
		// is safe and must not consume an attempt or invent an uncertain effect.
		owned, releaseErr := r.repo.MarkDeferred(job.ID, r.workerID, job.LeaseGeneration, r.now().Add(DefaultDeferDelay), "worker stopped before handler admission")
		if settleErr := r.settlementResult(ctx, job, job.Attempts, owned, releaseErr); settleErr != nil {
			return &reviewResultError{cause: errors.Join(err, settleErr)}
		}
		return err
	}
	if heartbeatErr != nil {
		if job.ReplayPolicy == models.DurableJobReviewUnknown {
			return r.holdForReview(ctx, job, attempt, heartbeatErr)
		}
		return heartbeatErr
	}
	if !ownsLease {
		// Another worker reclaimed the job. The stale result is intentionally
		// discarded; handlers remain responsible for idempotent side effects.
		if job.ReplayPolicy == models.DurableJobReviewUnknown {
			return ErrManualReviewRequired
		}
		return nil
	}
	if RequiresManualReview(err) {
		return r.holdForReview(ctx, job, attempt, err)
	}
	if job.ReplayPolicy == models.DurableJobReviewUnknown &&
		(ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return r.holdForReview(ctx, job, attempt, errors.Join(ctx.Err(), err))
	}
	var deferred *DeferredError
	if errors.As(err, &deferred) {
		owned, markErr := r.repo.MarkDeferred(job.ID, r.workerID, job.LeaseGeneration, r.now().Add(DefaultDeferDelay), deferred.Error())
		return r.settlementResult(ctx, job, job.Attempts, owned, markErr)
	}
	if err := ctx.Err(); err != nil {
		// Shutdown is not a completed handler failure, even on the last allowed
		// attempt. Retain the lease until expiry so an unwinding handler cannot
		// overlap its replacement. Recovery consumes the unknown-outcome attempt.
		return err
	}
	retryable, retryAfter := retryDirective(err)
	if schedule, recurring := r.recurringFor(job.Kind); recurring && (err == nil || attempt >= job.MaxAttempts || !retryable) {
		successorPolicy := schedule.replayPolicy
		if job.ReplayPolicy == models.DurableJobReviewUnknown {
			successorPolicy = models.DurableJobReviewUnknown
		}
		terminalStatus, lastErr := models.DurableJobSucceeded, ""
		if err != nil {
			terminalStatus, lastErr = models.DurableJobDead, err.Error()
		}
		next := &models.DurableJob{Queue: r.queue, Kind: job.Kind, Payload: schedule.payload,
			RunAt: r.now().Add(schedule.interval), MaxAttempts: schedule.maxAttempts, ReplayPolicy: successorPolicy}
		var owned bool
		var markErr error
		if successorPolicy == models.DurableJobReviewUnknown {
			if repository, ok := r.repo.(ReviewRecurringRepository); ok {
				owned, _, markErr = repository.CompleteReviewRecurring(ctx, job.ID, r.workerID, job.LeaseGeneration, r.now(), terminalStatus, attempt, lastErr, next)
			} else {
				markErr = ErrReviewPersistenceUnavailable
			}
		} else {
			owned, _, markErr = r.repo.CompleteRecurring(job.ID, r.workerID, job.LeaseGeneration, r.now(), terminalStatus, attempt, lastErr, next)
		}
		return r.settlementResult(ctx, job, attempt, owned, markErr)
	}
	if err == nil {
		now := r.now()
		if repository, ok := r.repo.(AttemptAwareSuccessRepository); ok {
			owned, markErr := repository.MarkSucceededWithAttempts(job.ID, r.workerID, job.LeaseGeneration, now, attempt)
			return r.settlementResult(ctx, job, attempt, owned, markErr)
		}
		owned, markErr := r.repo.MarkSucceeded(job.ID, r.workerID, job.LeaseGeneration, now)
		return r.settlementResult(ctx, job, attempt, owned, markErr)
	}
	if !retryable || attempt >= job.MaxAttempts {
		owned, markErr := r.repo.MarkDead(job.ID, r.workerID, job.LeaseGeneration, r.now(), attempt, err.Error())
		return r.settlementResult(ctx, job, attempt, owned, markErr)
	}
	retryNow := r.now()
	delay := r.policy.Delay(attempt)
	if retryAfter > delay {
		delay = retryAfter
	}
	retryAt := retryNow.Add(delay)
	owned, markErr := r.repo.MarkForRetry(job.ID, r.workerID, job.LeaseGeneration, retryAt, attempt, err.Error())
	return r.settlementResult(ctx, job, attempt, owned, markErr)
}

func retryDirective(err error) (retryable bool, retryAfter time.Duration) {
	if err == nil {
		return true, 0
	}
	retryable = true
	var classification RetryClassification
	if errors.As(err, &classification) {
		retryable = classification.Retryable()
	}
	var metadata RetryAfterMetadata
	if errors.As(err, &metadata) {
		retryAfter = metadata.RetryAfter()
		if retryAfter > MaxRetryAfter {
			return false, 0
		}
		if retryAfter < 0 {
			retryAfter = 0
		}
	}
	return retryable, retryAfter
}

func (r *Runner) safeInvokeWithHeartbeat(ctx context.Context, handler Handler, job models.DurableJob) (error, bool, error) {
	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	if !lifecycle.Go(handlerCtx, "durable-job-handler", func() {
		result <- r.safeInvoke(handlerCtx, handler, job)
	}) {
		return errors.Join(errHandlerAdmissionClosed, context.Canceled, ctx.Err()), true, nil
	}

	interval := r.lease / 3
	if interval <= 0 {
		select {
		case err := <-result:
			return err, true, nil
		case <-ctx.Done():
			cancel()
			return ctx.Err(), true, nil
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err, true, nil
		case <-ctx.Done():
			cancel()
			return ctx.Err(), true, nil
		case <-ticker.C:
			owned, err := r.repo.ExtendLease(job.ID, r.workerID, job.LeaseGeneration, r.now())
			if err != nil {
				cancel()
				return nil, true, fmt.Errorf("extend lease for %s: %w", job.ID, err)
			}
			if !owned {
				cancel()
				return nil, false, nil
			}
		}
	}
}

// safeInvoke turns a panicking handler into a normal error so one bad job can
// never take down the worker process.
func (r *Runner) safeInvoke(ctx context.Context, handler Handler, job models.DurableJob) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("handler panicked: %v", recovered)
			if job.ReplayPolicy == models.DurableJobReviewUnknown {
				err = &invocationUnconfirmedError{cause: err}
			}
		}
	}()
	return handler(ctx, job)
}

// Start polls the queue until the context is cancelled. It performs one pass
// immediately because RegisterRecurring may have recovered work that is already
// due at process startup. Waiting for the idle interval here makes restart
// recovery unnecessarily slow and pressures callers to use wasteful short
// polling intervals.
func (r *Runner) Start(ctx context.Context, interval time.Duration) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	r.markStarted(r.now())
	defer r.markStopped()
	r.runAndReport(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runAndReport(ctx)
		}
	}
}

func (r *Runner) runAndReport(ctx context.Context) {
	_, err := r.RunOnce(ctx)
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return
	}
	now := r.now()
	becameUnhealthy, recovered := r.recordPollResult(now, err)
	if becameUnhealthy {
		log.Printf("durablejob: queue %q worker %q poll failed: %v", r.queue, r.workerID, err)
	}
	if recovered {
		log.Printf("durablejob: queue %q worker %q recovered", r.queue, r.workerID)
	}
}

func (r *Runner) markStarted(now time.Time) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = true
	if r.status.StartedAt.IsZero() {
		r.status.StartedAt = now.UTC()
	}
}

func (r *Runner) markStopped() {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
}

func (r *Runner) recordPollResult(now time.Time, err error) (becameUnhealthy, recovered bool) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.LastPollAt = now.UTC()
	if err == nil {
		recovered = r.status.LastError != ""
		r.status.LastSuccessfulAt = now.UTC()
		r.status.LastError = ""
		r.status.ConsecutiveErrors = 0
		return false, recovered
	}
	message := safety.RedactSecrets(err.Error())
	becameUnhealthy = r.status.LastError != message
	r.status.LastError = message
	r.status.ConsecutiveErrors++
	return becameUnhealthy, false
}
