package durablejob

import (
	"context"
	"fmt"
	"sync"
	"time"

	"automation-hub-backend/internal/backoff"
	"automation-hub-backend/internal/models"
)

// Job is the unit of work handed to a Handler. Aliased so callers registering
// handlers do not need to import the models package.
type Job = models.DurableJob

// Handler executes one job. It must be idempotent: delivery is at-least-once,
// so a handler can run twice if a worker dies after the side effect but before
// the status write.
type Handler func(ctx context.Context, job Job) error

// DefaultLease is how long a claimed job may be held before another worker is
// allowed to assume the holder died and reclaim it.
const DefaultLease = 5 * time.Minute

// maxIdlePollMultiplier bounds idle backoff so work enqueued by another
// process is still discovered promptly. With the default 15-second worker
// interval, an empty queue settles at one database poll per minute instead of
// four, while active queues are drained without waiting for another tick.
const maxIdlePollMultiplier = 4

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

	mu       sync.RWMutex
	handlers map[string]Handler
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
		repo:     repo,
		policy:   opts.Policy,
		workerID: opts.WorkerID,
		queue:    opts.Queue,
		lease:    opts.Lease,
		batch:    opts.Batch,
		now:      opts.Now,
		handlers: map[string]Handler{},
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

// Enqueue schedules a job. runAt zero means "run as soon as possible".
func (r *Runner) Enqueue(kind, payload string, runAt time.Time, maxAttempts int) (*models.DurableJob, error) {
	if runAt.IsZero() {
		runAt = r.now()
	}
	return r.repo.Enqueue(&models.DurableJob{
		Queue:       r.queue,
		Kind:        kind,
		Payload:     payload,
		RunAt:       runAt.UTC(),
		MaxAttempts: maxAttempts,
		Status:      models.DurableJobPending,
	})
}

// EnsureScheduled enqueues a job of the given kind only when none is already
// pending or running. Recurring work (a periodic scan that re-enqueues itself)
// uses this at startup so restarts do not pile up duplicate schedules.
// It reports whether a new job was created.
func (r *Runner) EnsureScheduled(kind, payload string, runAt time.Time, maxAttempts int) (bool, error) {
	if runAt.IsZero() {
		runAt = r.now()
	}
	created, err := r.repo.EnqueueIfNoActive(&models.DurableJob{
		Queue:       r.queue,
		Kind:        kind,
		Payload:     payload,
		RunAt:       runAt.UTC(),
		MaxAttempts: maxAttempts,
		Status:      models.DurableJobPending,
	})
	if err != nil {
		return false, fmt.Errorf("ensure singleton %s job: %w", kind, err)
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
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	r.Register(kind, func(ctx context.Context, job Job) error {
		err := work(ctx)
		finalAttempt := job.Attempts+1 >= job.MaxAttempts
		if err == nil || finalAttempt {
			if _, scheduleErr := r.Enqueue(kind, "{}", r.now().Add(interval), maxAttempts); scheduleErr != nil {
				return fmt.Errorf("reschedule %s: %w", kind, scheduleErr)
			}
		}
		return err
	})
	if _, err := r.EnsureScheduled(kind, "{}", r.now(), maxAttempts); err != nil {
		return fmt.Errorf("schedule %s: %w", kind, err)
	}
	return nil
}

// RunOnce performs a single durable cycle: recover leases abandoned by dead
// workers, claim the due batch, and execute it. It returns how many jobs were
// processed. Handler failures are recorded as retries/dead-letters, not
// returned, so one bad job cannot stall the queue.
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	now := r.now()
	if _, err := r.repo.ReapExpiredLeases(now, r.lease); err != nil {
		return 0, fmt.Errorf("reap expired leases: %w", err)
	}
	jobs, err := r.repo.ClaimDue(r.workerID, r.queue, now, r.batch)
	if err != nil {
		return 0, fmt.Errorf("claim due jobs: %w", err)
	}
	processed := 0
	var firstErr error
	for _, job := range jobs {
		if err := r.execute(ctx, job); err != nil && firstErr == nil {
			firstErr = err
		}
		processed++
	}
	return processed, firstErr
}

// execute runs one claimed job and records the outcome.
func (r *Runner) execute(ctx context.Context, job models.DurableJob) error {
	attempt := job.Attempts + 1
	handler, ok := r.handlerFor(job.Kind)
	if !ok {
		// An unregistered kind is a deployment error, not a transient fault:
		// fail it straight to the dead letter rather than retrying forever.
		_, err := r.repo.MarkDead(job.ID, r.workerID, job.LeaseGeneration, r.now(), attempt, fmt.Sprintf("no handler registered for kind %q", job.Kind))
		return err
	}

	err, ownsLease, heartbeatErr := r.safeInvokeWithHeartbeat(ctx, handler, job)
	if heartbeatErr != nil {
		return heartbeatErr
	}
	if !ownsLease {
		// Another worker reclaimed the job. The stale result is intentionally
		// discarded; handlers remain responsible for idempotent side effects.
		return nil
	}
	if err == nil {
		_, markErr := r.repo.MarkSucceeded(job.ID, r.workerID, job.LeaseGeneration, r.now())
		return markErr
	}
	if attempt >= job.MaxAttempts {
		_, markErr := r.repo.MarkDead(job.ID, r.workerID, job.LeaseGeneration, r.now(), attempt, err.Error())
		return markErr
	}
	retryAt := r.now().Add(r.policy.Delay(attempt))
	_, markErr := r.repo.MarkForRetry(job.ID, r.workerID, job.LeaseGeneration, retryAt, attempt, err.Error())
	return markErr
}

func (r *Runner) safeInvokeWithHeartbeat(ctx context.Context, handler Handler, job models.DurableJob) (error, bool, error) {
	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- r.safeInvoke(handlerCtx, handler, job)
	}()

	interval := r.lease / 3
	if interval <= 0 {
		return <-result, true, nil
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
		}
	}()
	return handler(ctx, job)
}

// Start polls the queue until the context is cancelled. It runs once
// immediately, drains active batches without an artificial delay, and backs
// off to at most four times the configured interval while the queue is empty.
// Repository errors retain the base interval so a database recovery is noticed
// promptly. This is the long-running worker loop; call it from a goroutine at
// startup.
func (r *Runner) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	idleCycles := 0
	delay := time.Duration(0)
	for {
		if !waitForNextRun(ctx, delay) {
			return
		}
		processed, err := r.RunOnce(ctx)
		if err != nil {
			// A repository error is transient (e.g. DB blip); retry at the
			// configured base interval instead of extending idle backoff.
			idleCycles = 0
			delay = interval
			continue
		}
		if processed > 0 {
			idleCycles = 0
			delay = 0
			continue
		}
		idleCycles++
		delay = adaptiveIdleDelay(interval, idleCycles)
	}
}

func waitForNextRun(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func adaptiveIdleDelay(base time.Duration, idleCycles int) time.Duration {
	if base <= 0 {
		base = 5 * time.Second
	}
	if idleCycles <= 0 {
		return base
	}
	multiplier := 1 << min(idleCycles-1, 2)
	if multiplier > maxIdlePollMultiplier {
		multiplier = maxIdlePollMultiplier
	}
	return time.Duration(multiplier) * base
}
