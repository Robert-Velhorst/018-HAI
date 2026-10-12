package ambientmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type schedulerStub struct {
	scopes               []Scope
	dueScopeCalls        int
	pendingScopeCalls    int
	recovered            int
	compositionRecovered int
	processed            int
	processError         error
}

type cancelingDiscoveryScheduler struct {
	*schedulerStub
	cancel   context.CancelFunc
	cancelOn string
}

func (s *cancelingDiscoveryScheduler) DueScopes(ctx context.Context, now time.Time, limit int) ([]Scope, error) {
	s.schedulerStub.dueScopeCalls++
	if s.cancelOn == "due" {
		s.cancel()
	}
	return nil, nil
}

func (s *cancelingDiscoveryScheduler) PendingCompositionScopes(ctx context.Context, now time.Time, limit int) ([]Scope, error) {
	s.schedulerStub.pendingScopeCalls++
	if s.cancelOn == "composition" {
		s.cancel()
	}
	return nil, nil
}

func (s *schedulerStub) DueScopes(context.Context, time.Time, int) ([]Scope, error) {
	s.dueScopeCalls++
	return append([]Scope(nil), s.scopes...), nil
}
func (s *schedulerStub) PendingCompositionScopes(context.Context, time.Time, int) ([]Scope, error) {
	s.pendingScopeCalls++
	return nil, nil
}
func (s *schedulerStub) RecoverExpiredLeases(context.Context, Scope, time.Time) (int, error) {
	s.recovered++
	return 0, nil
}
func (s *schedulerStub) RecoverExpiredCompositionLeases(context.Context, Scope, time.Time) (int, error) {
	s.compositionRecovered++
	return 0, nil
}
func (s *schedulerStub) ProcessDue(context.Context, ProcessDueRequest) (ProcessDueResult, error) {
	s.processed++
	return ProcessDueResult{Authority: advisoryAuthority()}, s.processError
}

type schedulerDurableRepo struct {
	durablejob.Repository
	jobs map[uuid.UUID]*models.DurableJob
}

func (r *schedulerDurableRepo) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	for _, existing := range r.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind &&
			(existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return false, nil
		}
	}
	copyJob := *job
	if copyJob.ID == uuid.Nil {
		copyJob.ID = uuid.New()
	}
	r.jobs[copyJob.ID] = &copyJob
	return true, nil
}

func (r *schedulerDurableRepo) ReapExpiredLeases(time.Time, time.Duration) (int, error) {
	return 0, nil
}

func (r *schedulerDurableRepo) ReapExpiredLeasesForQueue(_ string, _ time.Time, _ time.Duration) (int, error) {
	return 0, nil
}

func (r *schedulerDurableRepo) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	claimed := make([]models.DurableJob, 0, limit)
	for _, job := range r.jobs {
		if len(claimed) >= limit {
			break
		}
		if job.Queue != queue || job.Status != models.DurableJobPending || job.RunAt.After(now) {
			continue
		}
		job.Status = models.DurableJobRunning
		job.LockedBy = workerID
		job.LeaseGeneration++
		lockedAt := now
		job.LockedAt = &lockedAt
		claimed = append(claimed, *job)
	}
	return claimed, nil
}

func (r *schedulerDurableRepo) MarkDeferred(id uuid.UUID, workerID string, generation int64, runAt time.Time, reason string) (bool, error) {
	job := r.jobs[id]
	if job == nil || job.Status != models.DurableJobRunning || job.LockedBy != workerID || job.LeaseGeneration != generation {
		return false, nil
	}
	job.Status = models.DurableJobPending
	job.RunAt = runAt
	job.LastError = reason
	job.LockedBy = ""
	job.LockedAt = nil
	return true, nil
}

func TestRunMonitorSweepProcessesEveryDueScope(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{{OwnerID: "owner-a", WorkspaceID: "workspace-a"}, {OwnerID: "owner-b", WorkspaceID: "workspace-b"}}}
	if err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if stub.recovered != 2 || stub.compositionRecovered != 2 || stub.processed != 2 {
		t.Fatalf("recovered=%d compositionRecovered=%d processed=%d, want 2/2/2", stub.recovered, stub.compositionRecovered, stub.processed)
	}
}

func TestRunMonitorSweepReturnsSanitizedAggregateFailure(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{{OwnerID: "owner-a", WorkspaceID: "workspace-a"}}, processError: errors.New("Authorization: Bearer secret")}
	err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), func() bool { return true })
	if err == nil || err.Error() != "ambient outcome sweep failed for 1 scoped batch(es)" {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMonitorSweepDoesNotAcknowledgeCancellationDuringEmptyDiscovery(t *testing.T) {
	for _, cancelOn := range []string{"due", "composition"} {
		t.Run(cancelOn, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := &schedulerStub{}
			service := &cancelingDiscoveryScheduler{schedulerStub: base, cancel: cancel, cancelOn: cancelOn}

			err := runMonitorSweep(ctx, service, time.Now().UTC(), func() bool { return true })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("runMonitorSweep error = %v, want context.Canceled", err)
			}
			if cancelOn == "due" && base.pendingScopeCalls != 0 {
				t.Fatalf("composition discovery calls = %d after cancellation, want 0", base.pendingScopeCalls)
			}
			if base.recovered != 0 || base.compositionRecovered != 0 || base.processed != 0 {
				t.Fatalf("canceled empty sweep reached work: recovered=%d compositionRecovered=%d processed=%d", base.recovered, base.compositionRecovered, base.processed)
			}
		})
	}
}

func TestRunMonitorSweepStopsWhenPermissionIsWithdrawnMidSweep(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{
		{OwnerID: "owner-a", WorkspaceID: "workspace-a"},
		{OwnerID: "owner-b", WorkspaceID: "workspace-b"},
	}}
	allowedChecks := 0
	allowed := func() bool {
		allowedChecks++
		return stub.processed == 0
	}
	err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), allowed)
	var deferred *durablejob.DeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("error = %v, want deferred pause", err)
	}
	if stub.processed != 1 || stub.recovered != 1 || stub.compositionRecovered != 1 {
		t.Fatalf("processed=%d recovered=%d compositionRecovered=%d, want 1/1/1 after permission withdrawal", stub.processed, stub.recovered, stub.compositionRecovered)
	}
	if allowedChecks < 3 {
		t.Fatalf("allowed checks = %d, want checks before and during scoped work", allowedChecks)
	}
}

func TestMonitorSafetyGateFailsClosedWhenUnavailable(t *testing.T) {
	if monitorSafetyGate(nil)() {
		t.Fatal("missing safety gate allowed scheduled processing")
	}
}

func TestMonitorSafetyGateReadsCurrentPolicy(t *testing.T) {
	allowed := true
	gate := monitorSafetyGate(func() bool { return allowed })
	if !gate() {
		t.Fatal("enabled safety gate denied scheduled processing")
	}
	allowed = false
	if gate() {
		t.Fatal("safety gate kept allowing work after policy withdrawal")
	}
}

func TestRegisterDurableSchedulingDefersWithoutSafetyGate(t *testing.T) {
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	repository := &schedulerDurableRepo{jobs: make(map[uuid.UUID]*models.DurableJob)}
	runner := durablejob.NewRunner(repository, durablejob.Options{
		WorkerID: "outcome-monitor-test", Queue: "outcome-monitor", Now: func() time.Time { return now },
	})
	service := &schedulerStub{scopes: []Scope{{OwnerID: "owner-a", WorkspaceID: "workspace-a"}}}
	if err := RegisterDurableScheduling(runner, service, nil, time.Hour); err != nil {
		t.Fatalf("RegisterDurableScheduling: %v", err)
	}
	if processed, err := runner.RunOnce(t.Context()); err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v; want one deferred durable occurrence", processed, err)
	}
	if service.dueScopeCalls != 0 || service.pendingScopeCalls != 0 || service.recovered != 0 || service.compositionRecovered != 0 || service.processed != 0 {
		t.Fatalf("safety gate absence reached service work: due=%d pending=%d recovered=%d compositionRecovered=%d processed=%d", service.dueScopeCalls, service.pendingScopeCalls, service.recovered, service.compositionRecovered, service.processed)
	}
	if len(repository.jobs) != 1 {
		t.Fatalf("durable jobs = %d, want the deferred occurrence retained", len(repository.jobs))
	}
	for _, job := range repository.jobs {
		if job.Status != models.DurableJobPending || job.Attempts != 0 || !job.RunAt.Equal(now.Add(durablejob.DefaultDeferDelay)) {
			t.Fatalf("deferred occurrence = status %q, attempts %d, runAt %s; want pending/0/%s", job.Status, job.Attempts, job.RunAt, now.Add(durablejob.DefaultDeferDelay))
		}
	}
}

func TestMonitorSchedulerEnvironmentBounds(t *testing.T) {
	t.Setenv("OUTCOME_MONITOR_BATCH_LIMIT", "999")
	t.Setenv("OUTCOME_MONITOR_POLL_SECONDS", "0")
	t.Setenv("OUTCOME_MONITOR_SCHEDULER_ENABLED", "off")
	if monitorBatchLimit() != 20 || monitorPollInterval() != 5*time.Minute || DurableSchedulerEnabled() {
		t.Fatal("invalid scheduler environment did not fall back safely")
	}
}
