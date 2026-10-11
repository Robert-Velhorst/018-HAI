package ambient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type ambientContextScanProbe struct {
	Service
	legacy, contextual int
	ctx                context.Context
	gate               func() bool
	stop               context.CancelFunc
}

func (s *ambientContextScanProbe) Scan(string) (*models.AmbientScan, error) {
	s.legacy++
	s.stop()
	return &models.AmbientScan{Status: "completed"}, nil
}

func (s *ambientContextScanProbe) ScanContext(ctx context.Context, _ string, allowed ...func() bool) (*models.AmbientScan, error) {
	s.contextual++
	s.ctx = ctx
	if len(allowed) > 0 {
		s.gate = allowed[0]
	}
	s.stop()
	return &models.AmbientScan{Status: "failed"}, ctx.Err()
}

func TestAmbientContextTickerPassesExecutionContextAndGate(t *testing.T) {
	t.Setenv("AMBIENT_RUN_ON_STARTUP", "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &ambientContextScanProbe{stop: cancel}
	scheduler := &Scheduler{service: probe, backgroundAllowed: func() bool { return true }}
	scheduler.Start(ctx, time.Minute)
	if probe.legacy != 0 || probe.contextual != 1 || probe.ctx != ctx || probe.gate == nil || !probe.gate() || scheduler.running.Load() {
		t.Fatalf("ticker discarded execution context: legacy=%d contextual=%d gate=%v", probe.legacy, probe.contextual, probe.gate != nil)
	}
}

type ambientContextQueue struct {
	durablejob.Repository
	job       *models.DurableJob
	claimed   bool
	completed int
	deferred  int
	retried   int
	lastError string
	held      int
}

func (r *ambientContextQueue) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	if r.job != nil && r.job.Status == models.DurableJobNeedsReview {
		return false, nil
	}
	copy := *job
	copy.ID = uuid.New()
	r.job = &copy
	return true, nil
}

func (r *ambientContextQueue) EnqueueReviewIfNoActive(job *models.DurableJob) (bool, error) {
	if job.ReplayPolicy != models.DurableJobReviewUnknown {
		return false, errors.New("missing durable replay policy")
	}
	return r.EnqueueIfNoActive(job)
}

func (r *ambientContextQueue) MarkForReview(ctx context.Context, _ uuid.UUID, _ string, _ int64, _ time.Time, attempts int, reason string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	r.held++
	r.lastError = reason
	r.job.Status, r.job.Attempts = models.DurableJobNeedsReview, attempts
	return true, nil
}

func (*ambientContextQueue) ReapExpiredLeases(time.Time, time.Duration) (int, error) { return 0, nil }
func (*ambientContextQueue) ReapExpiredLeasesForQueue(string, time.Time, time.Duration) (int, error) {
	return 0, nil
}
func (*ambientContextQueue) ExtendLease(uuid.UUID, string, int64, time.Time) (bool, error) {
	return true, nil
}
func (r *ambientContextQueue) ClaimDue(worker, _ string, _ time.Time, _ int) ([]models.DurableJob, error) {
	if r.claimed || r.job == nil {
		return nil, nil
	}
	r.claimed = true
	r.job.Status, r.job.LockedBy, r.job.LeaseGeneration = models.DurableJobRunning, worker, 1
	return []models.DurableJob{*r.job}, nil
}
func (r *ambientContextQueue) CompleteRecurring(uuid.UUID, string, int64, time.Time, string, int, string, *models.DurableJob) (bool, bool, error) {
	r.completed++
	return true, true, nil
}

func (r *ambientContextQueue) CompleteReviewRecurring(ctx context.Context, id uuid.UUID, worker string, generation int64, now time.Time, terminal string, attempts int, reason string, next *models.DurableJob) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return r.CompleteRecurring(id, worker, generation, now, terminal, attempts, reason, next)
}

func (r *ambientContextQueue) MarkDeferred(_ uuid.UUID, _ string, _ int64, _ time.Time, reason string) (bool, error) {
	r.deferred++
	r.lastError = reason
	return true, nil
}

func (r *ambientContextQueue) MarkForRetry(_ uuid.UUID, _ string, _ int64, _ time.Time, _ int, reason string) (bool, error) {
	r.retried++
	r.lastError = reason
	return true, nil
}

func TestAmbientContextDurableHandlerPassesExecutionContextAndGate(t *testing.T) {
	group := lifecycle.New(context.Background())
	ctx, cancel := context.WithCancel(group.Context())
	defer cancel()
	defer func() {
		join, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := group.StopAndWait(join); err != nil {
			t.Error(err)
		}
	}()
	probe := &ambientContextScanProbe{stop: cancel}
	queue := &ambientContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "ambient", Batch: 1})
	if err := RegisterDurableScheduling(runner, probe, time.Minute, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	processed, err := runner.RunOnce(ctx)
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if joinErr := group.StopAndWait(join); joinErr != nil {
		t.Fatal(joinErr)
	}
	if processed != 1 || !errors.Is(err, context.Canceled) || probe.legacy != 0 || probe.contextual != 1 || probe.ctx == nil || probe.gate == nil || queue.completed != 0 {
		t.Fatalf("durable handler lost cancellation: processed=%d err=%v legacy=%d contextual=%d completed=%d", processed, err, probe.legacy, probe.contextual, queue.completed)
	}
}

type ambientNilScanProbe struct{ Service }

func (*ambientNilScanProbe) Scan(string) (*models.AmbientScan, error) { return nil, nil }
func (*ambientNilScanProbe) ScanContext(context.Context, string, ...func() bool) (*models.AmbientScan, error) {
	return nil, nil
}

func TestAmbientContextNilScanCannotAcknowledgeSuccess(t *testing.T) {
	if err := runAmbientScan(context.Background(), &ambientNilScanProbe{}, func() bool { return true }); !errors.Is(err, ErrScanOutcomeUnconfirmed) {
		t.Fatal("missing scan acknowledged as successful")
	}
}

func TestAmbientContextActualDurableServiceAcknowledgement(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "false")
	for _, scenario := range []string{"completed", "paused", "unconfirmed", "paused-unconfirmed"} {
		t.Run(scenario, func(t *testing.T) {
			group := lifecycle.New(context.Background())
			defer func() {
				join, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				if err := group.StopAndWait(join); err != nil {
					t.Error(err)
				}
			}()
			allowed := true
			trace := &ambientScanTrace{}
			if scenario == "paused" || scenario == "paused-unconfirmed" {
				trace.boundary, trace.stop = "find", func() { allowed = false }
			}
			if scenario == "unconfirmed" || scenario == "paused-unconfirmed" {
				trace.settlementError = errors.New("storage token=must-not-leak")
			}
			engine, _, _ := ambientContextFixture(trace)
			queue := &ambientContextQueue{}
			runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "ambient", Batch: 1})
			if err := RegisterDurableScheduling(runner, engine, time.Minute, func() bool { return allowed }); err != nil {
				t.Fatal(err)
			}
			processed, err := runner.RunOnce(group.Context())
			join, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if joinErr := group.StopAndWait(join); joinErr != nil {
				t.Fatal(joinErr)
			}
			unconfirmed := scenario == "unconfirmed" || scenario == "paused-unconfirmed"
			if processed != 1 || (!unconfirmed && err != nil) || (unconfirmed && !errors.Is(err, durablejob.ErrManualReviewRequired)) || engine.scanning.Load() || trace.legacy != 0 {
				t.Fatalf("actual service/runner chain failed: processed=%d err=%v calls=%v", processed, err, trace.calls)
			}
			switch scenario {
			case "completed":
				if queue.completed != 1 || queue.deferred != 0 || queue.retried != 0 || trace.prunes != 1 || trace.stored.Status != "completed" {
					t.Fatal("known completion did not reach durable acknowledgement")
				}
			case "paused":
				if queue.completed != 0 || queue.deferred != 1 || queue.retried != 0 || trace.saves != 0 || trace.prunes != 0 || trace.stored.Status != "failed" {
					t.Fatal("policy pause was executed, retried or falsely completed")
				}
			case "unconfirmed", "paused-unconfirmed":
				if queue.completed != 0 || queue.deferred != 0 || queue.retried != 0 || queue.held != 1 || trace.prunes != 0 || !strings.Contains(queue.lastError, ErrScanOutcomeUnconfirmed.Error()) || strings.Contains(queue.lastError, "must-not-leak") || !strings.Contains(queue.lastError, "[REDACTED]") {
					t.Fatalf("durable failure lost uncertainty or leaked credentials: %+v", queue)
				}
			}
		})
	}
}

type ambientWithoutReviewQueue struct{ durablejob.Repository }

func (*ambientWithoutReviewQueue) EnqueueIfNoActive(*models.DurableJob) (bool, error) {
	panic("unsupported review storage admitted recurring work")
}

func TestAmbientContextRefusesSchedulingWithoutReviewStorage(t *testing.T) {
	runner := durablejob.NewRunner(&ambientWithoutReviewQueue{}, durablejob.Options{Queue: "ambient"})
	if err := RegisterDurableScheduling(runner, &ambientNilScanProbe{}, time.Minute, func() bool { return true }); !errors.Is(err, durablejob.ErrReviewPersistenceUnavailable) {
		t.Fatal("ambient scheduler admitted a store that cannot preserve unknown outcomes")
	}
}
