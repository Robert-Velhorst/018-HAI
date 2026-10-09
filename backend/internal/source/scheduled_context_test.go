package source

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type scheduledContextKey struct{}

type scheduledContextRepository struct {
	*fakeSourceRepo
	seen   context.Context
	cancel context.CancelFunc
}

func (r *scheduledContextRepository) AcquireSourceSyncLease(ctx context.Context, _ uuid.UUID) (func(), bool, error) {
	r.seen = ctx
	r.cancel()
	return nil, false, ctx.Err()
}

func TestScheduledSourcePathsPreserveCallerContextThroughSyncLease(t *testing.T) {
	for _, path := range []string{"scheduler", "owner-request"} {
		t.Run(path, func(t *testing.T) {
			base := newFakeSourceRepo(&models.ConnectedSource{
				ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "synthetic scheduled folder",
				Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: ".",
			})
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), scheduledContextKey{}, "owned-marker"))
			defer cancel()
			repo := &scheduledContextRepository{fakeSourceRepo: base, cancel: cancel}
			svc := NewService(repo, &fakeSourceMemoryService{})
			var run *ScheduledSyncRun
			var err error
			if path == "scheduler" {
				run, err = runScheduledSyncsWithContext(ctx, svc, time.Now().UTC())
			} else {
				run, err = runOwnerScheduledSyncsWithContext(ctx, svc, time.Now().UTC(), "alice")
				if base.lastVisibleSourceOwner != "alice" {
					t.Fatal("context path lost owner scoping")
				}
			}
			if repo.seen == nil || repo.seen.Value(scheduledContextKey{}) != "owned-marker" {
				t.Fatal("scheduled sync replaced its caller context with Background")
			}
			if !errors.Is(err, context.Canceled) || run == nil || run.Completed != 0 || run.Failed != 0 {
				t.Fatalf("canceled run was classified as success/provider failure: %+v, %v", run, err)
			}
		})
	}
}

func TestScheduledSourceCancellationRefusesRepositoryAccess(t *testing.T) {
	base := newFakeSourceRepo()
	svc := NewService(base, &fakeSourceMemoryService{}).(ContextScheduledSyncService)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.RunDueScheduledSyncsContext(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := svc.RunDueScheduledSyncsForOwnerContext(ctx, time.Now(), "alice"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := svc.RunDueScheduledSyncsContext(nil, time.Now()); err == nil {
		t.Fatal("nil context accepted")
	}
	if base.findSourcesCalls != 0 || base.visibleSourcesCalls != 0 {
		t.Fatal("canceled scheduling touched repository")
	}
}

func TestSourceSyncPreservesExpiredDeadlineWithoutRepositoryAccess(t *testing.T) {
	repo := newFakeSourceRepo()
	svc := NewService(repo, &fakeSourceMemoryService{}).(*service)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if result, err := svc.SyncContext(ctx, uuid.New(), ImportRequest{}); result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline became another outcome: %v, %v", result, err)
	}
	if repo.sourceLeaseCalls != 0 || len(repo.auditLogs) != 0 {
		t.Fatal("expired caller touched source storage")
	}
}

func TestScheduledSourceRuntimeStopDoesNotCreateFailureWork(t *testing.T) {
	for _, path := range []string{"legacy-scheduler", "legacy-owner", "live-caller"} {
		t.Run(path, func(t *testing.T) {
			g := lifecycle.New(context.Background())
			drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := g.StopAndWait(drainCtx); err != nil {
				t.Fatal(err)
			}
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "synthetic scheduled folder",
				Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: ".",
			})
			workflows := &fakeSourceWorkflowService{}
			svc := WithRuntimeContext(NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflows), g.Context())
			var run *ScheduledSyncRun
			var err error
			switch path {
			case "legacy-scheduler":
				run, err = svc.RunDueScheduledSyncs(time.Now())
			case "legacy-owner":
				run, err = svc.RunDueScheduledSyncsForOwner(time.Now(), "alice")
			default:
				run, err = svc.(ContextScheduledSyncService).RunDueScheduledSyncsContext(context.Background(), time.Now())
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown lost cancellation: %+v, %v", run, err)
			}
			if run != nil && (run.Completed != 0 || run.Failed != 0 || len(run.Messages) != 0) {
				t.Fatalf("runtime refusal reported success or provider failure: %+v", run)
			}
			if repo.sourceLeaseCalls != 0 || len(repo.auditLogs) != 0 || len(workflows.requests) != 0 {
				t.Fatal("runtime refusal created source or failure-workflow writes")
			}
			if path != "live-caller" && (repo.findSourcesCalls != 0 || repo.visibleSourcesCalls != 0) {
				t.Fatal("legacy scheduling touched storage after runtime stop")
			}
		})
	}
}

type compatibilityOwnerRepository struct {
	*fakeSourceRepo
	entered  chan struct{}
	release  chan struct{}
	admitted bool
}

type deadlineLeaseRepository struct{ *fakeSourceRepo }

func (r *deadlineLeaseRepository) AcquireSourceSyncLease(context.Context, uuid.UUID) (func(), bool, error) {
	return nil, false, context.DeadlineExceeded
}

func TestScheduledSourceInnerTimeoutRemainsAnOperationalFailure(t *testing.T) {
	repo := &deadlineLeaseRepository{newFakeSourceRepo(&models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "synthetic timed-out source",
		Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: ".",
	})}
	workflows := &fakeSourceWorkflowService{}
	svc := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflows).(ContextScheduledSyncService)
	run, err := svc.RunDueScheduledSyncsContext(context.Background(), time.Now())
	if err != nil || run == nil || run.Failed != 1 || run.Completed != 0 || len(workflows.requests) != 1 {
		t.Fatalf("inner timeout was confused with sweep cancellation: %+v, %v; workflows=%d", run, err, len(workflows.requests))
	}
}

func (r *compatibilityOwnerRepository) AcquireSourceSyncLease(ctx context.Context, _ uuid.UUID) (func(), bool, error) {
	r.admitted = lifecycle.Go(context.WithoutCancel(ctx), "compatibility-refresh", func() { <-r.release })
	close(r.entered)
	return nil, false, errors.New("synthetic sync lease failure")
}

func TestLegacySyncKeepsRuntimeChildOwnershipWithoutCallerContext(t *testing.T) {
	g := lifecycle.New(context.Background())
	r := &compatibilityOwnerRepository{fakeSourceRepo: newFakeSourceRepo(), entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer func() {
		once.Do(func() { close(r.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := g.StopAndWait(ctx); err != nil {
			t.Error(err)
		}
	}()
	svc := WithRuntimeContext(NewService(r, &fakeSourceMemoryService{}), g.Context())
	if _, err := svc.Sync(uuid.New(), ImportRequest{}); err == nil {
		t.Fatal("synthetic sync failure lost")
	}
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("legacy sync did not reach its lease")
	}
	if !r.admitted {
		t.Fatal("legacy Sync lost child ownership")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("legacy child escaped shutdown join: %v", err)
	}
	if _, err := svc.Sync(uuid.New(), ImportRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy Sync reopened after stop: %v", err)
	}
	once.Do(func() { close(r.release) })
}
