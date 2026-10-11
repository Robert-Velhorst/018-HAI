package temporalbridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

type lifecycleTemporalWorker struct {
	worker.Worker
	stop func()
}

func (w *lifecycleTemporalWorker) Stop() { w.stop() }

type lifecycleTemporalClient struct {
	client.Client
	close func()
}

func (c *lifecycleTemporalClient) Close() { c.close() }

func finishTemporalGroup(t *testing.T, g *lifecycle.Group) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.StopAndWait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTemporalSupervisorJoinsSDKStopOutsideMutexAndSealsRestart(t *testing.T) {
	g := lifecycle.New(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var workerStops, clientCloses atomic.Int32
	defer func() { once.Do(func() { close(release) }); finishTemporalGroup(t, g) }()
	s := NewService(nil, nil, true, "127.0.0.1:7233", "default", "hai-test")
	s.worker = &lifecycleTemporalWorker{stop: func() {
		workerStops.Add(1)
		close(entered)
		<-release
	}}
	s.client = &lifecycleTemporalClient{close: func() { clientCloses.Add(1) }}
	s.dial = func(context.Context, client.Options) (client.Client, error) {
		t.Error("existing or stopped worker tried to dial")
		return nil, errors.New("must not dial")
	}
	s.StartWorkerEventually(g.Context())
	s.StartWorkerEventually(g.Context())
	g.Stop()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop its SDK worker")
	}
	statusDone := make(chan Status, 1)
	go func() { s.StartWorker(); statusDone <- s.Status() }()
	select {
	case state := <-statusDone:
		if state.WorkerStarted || state.WorkerError != "backend is shutting down" {
			t.Fatalf("stopped state = %+v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("SDK Stop held the service mutex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) || clientCloses.Load() != 0 {
		t.Fatalf("supervisor escaped SDK stop join: %v, client closes=%d", err, clientCloses.Load())
	}
	once.Do(func() { close(release) })
	finishTemporalGroup(t, g)
	s.stopWorker()
	if workerStops.Load() != 1 || clientCloses.Load() != 1 {
		t.Fatal("SDK cleanup was not exactly once")
	}
}

func TestTemporalSupervisorCancelsPendingDial(t *testing.T) {
	g := lifecycle.New(context.Background())
	defer finishTemporalGroup(t, g)
	entered := make(chan struct{})
	s := NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	var calls atomic.Int32
	s.dial = func(ctx context.Context, _ client.Options) (client.Client, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.StartWorkerEventually(g.Context())
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("dial fixture did not start")
	}
	finishTemporalGroup(t, g)
	s.StartWorker()
	if calls.Load() != 1 {
		t.Fatal("canceled supervisor retried or reopened")
	}
}

type heldTemporalRepository struct {
	Repository
	entered chan struct{}
	release chan struct{}
}

func (r *heldTemporalRepository) FindByID(id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	close(r.entered)
	<-r.release
	at := time.Now().UTC()
	return &models.TemporalWorkflowRun{ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "test-workflow", WorkflowType: followUpWorkflowType,
		Status: "completed", StartedAt: &at, CompletedAt: &at, ResultJSON: "{}"}, nil
}

func (r *heldTemporalRepository) FindByIDContext(_ context.Context, id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	return r.FindByID(id)
}

func TestTemporalActivityRemainsOwnedAfterSDKSupervisorReturns(t *testing.T) {
	g := lifecycle.New(context.Background())
	repo := &heldTemporalRepository{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer func() { once.Do(func() { close(repo.release) }); finishTemporalGroup(t, g) }()
	a := &followUpActivity{repo: repo, workflows: lifecycleWorkflowService{}, now: time.Now}
	done := make(chan error, 1)
	go func() { _, err := a.Run(g.Context(), FollowUpInput{RunID: uuid.NewString()}); done <- err }()
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("activity did not enter repository")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unreturned activity escaped group: %v", err)
	}
	if _, err := a.Run(g.WithContext(context.Background()), FollowUpInput{RunID: uuid.NewString()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("activity admitted after stop: %v", err)
	}
	once.Do(func() { close(repo.release) })
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	finishTemporalGroup(t, g)
}

type lifecycleWorkflowService struct{ workflow.Service }
