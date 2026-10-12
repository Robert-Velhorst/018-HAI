package temporalbridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func waitForStartupBoundary(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("%s did not finish", name)
	}
}

func heldDialStartup(t *testing.T) (*Service, chan struct{}, chan struct{}, chan struct{}, func()) {
	t.Helper()
	s := NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.dial = func(context.Context, client.Options) (client.Client, error) {
		close(entered)
		<-release
		return nil, errors.New("synthetic dial failure")
	}
	cleanup := func() {
		once.Do(func() { close(release) })
		waitForStartupBoundary(t, done, "owned startup")
	}
	return s, entered, release, done, cleanup
}

func TestStartupBoundaryStatusRemainsResponsiveDuringDial(t *testing.T) {
	s, entered, _, done, cleanup := heldDialStartup(t)
	defer cleanup()
	go func() { defer close(done); s.StartWorker() }()
	waitForStartupBoundary(t, entered, "dial entry")
	statusDone := make(chan struct{})
	go func() { defer close(statusDone); s.Status() }()
	select {
	case <-statusDone:
	case <-time.After(100 * time.Millisecond):
		t.Error("dial held the service mutex and blocked status")
	}
	cleanup()
	waitForStartupBoundary(t, statusDone, "status")
}

func TestStartupBoundaryDialHasAnExplicitDeadline(t *testing.T) {
	s := NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	s.dial = func(ctx context.Context, _ client.Options) (client.Client, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second {
			t.Error("dial did not receive a positive bounded ten-second startup deadline")
		}
		return nil, errors.New("synthetic dial failure")
	}
	s.StartWorker()
}

func TestStartupBoundaryStopCanCancelADialWithoutWaitingForMutex(t *testing.T) {
	s := NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	entered, release, done, canceled, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.dial = func(ctx context.Context, _ client.Options) (client.Client, error) {
		close(entered)
		select {
		case <-ctx.Done():
			close(canceled)
		case <-release:
		}
		return nil, errors.New("synthetic dial failure")
	}
	defer func() {
		once.Do(func() { close(release) })
		waitForStartupBoundary(t, done, "startup")
		waitForStartupBoundary(t, stopped, "stop")
	}()
	go func() { defer close(done); s.StartWorker() }()
	waitForStartupBoundary(t, entered, "dial")
	go func() { defer close(stopped); s.stopWorker() }()
	select {
	case <-canceled:
	case <-time.After(100 * time.Millisecond):
		t.Error("stop could not reach/cancel the owned pending dial")
	}
}

func TestStartupBoundaryManualRetryIsJoinedByHostLifecycle(t *testing.T) {
	s, entered, _, done, cleanup := heldDialStartup(t)
	g := lifecycle.New(context.Background())
	s.workerContext = g.Context()
	defer func() { cleanup(); finishTemporalGroup(t, g) }()
	go func() { defer close(done); s.StartWorker() }()
	waitForStartupBoundary(t, entered, "dial")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("host shutdown missed the unreturned manual startup: %v", err)
	}
}

func TestStartupBoundaryConcurrentRetryDoesNotQueueAnotherDial(t *testing.T) {
	s := NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	entered, release, firstDone, secondDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	s.dial = func(context.Context, client.Options) (client.Client, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil, errors.New("synthetic dial failure")
	}
	defer func() {
		once.Do(func() { close(release) })
		waitForStartupBoundary(t, firstDone, "first start")
		waitForStartupBoundary(t, secondDone, "second start")
	}()
	go func() { defer close(firstDone); s.StartWorker() }()
	waitForStartupBoundary(t, entered, "dial")
	go func() { defer close(secondDone); s.StartWorker() }()
	select {
	case <-secondDone:
	case <-time.After(100 * time.Millisecond):
		t.Error("concurrent retry waited on the service lock instead of the active attempt")
	}
	once.Do(func() { close(release) })
	waitForStartupBoundary(t, firstDone, "first start")
	waitForStartupBoundary(t, secondDone, "second start")
	if calls.Load() != 1 {
		t.Errorf("one active attempt produced %d dials", calls.Load())
	}
}

type startupBoundaryClient struct {
	client.Client
	health func(context.Context) error
	closes atomic.Int32
}

func (c *startupBoundaryClient) CheckHealth(ctx context.Context, _ *client.CheckHealthRequest) (*client.CheckHealthResponse, error) {
	if c.health != nil {
		if err := c.health(ctx); err != nil {
			return nil, err
		}
	}
	return &client.CheckHealthResponse{}, nil
}

func (c *startupBoundaryClient) Close() { c.closes.Add(1) }

type startupBoundaryWorker struct {
	worker.Worker
	start                 func() error
	stop                  func()
	starts, stops         atomic.Int32
	workflows, activities int
}

func (w *startupBoundaryWorker) RegisterWorkflow(any) { w.workflows++ }
func (w *startupBoundaryWorker) RegisterActivity(any) { w.activities++ }
func (w *startupBoundaryWorker) Start() error {
	w.starts.Add(1)
	if w.start != nil {
		return w.start()
	}
	return nil
}
func (w *startupBoundaryWorker) Stop() {
	w.stops.Add(1)
	if w.stop != nil {
		w.stop()
	}
}

type startupBoundaryHarness struct {
	service          *Service
	client           *startupBoundaryClient
	worker           *startupBoundaryWorker
	options          worker.Options
	dials, factories atomic.Int32
}

func newStartupBoundaryHarness() *startupBoundaryHarness {
	h := &startupBoundaryHarness{client: &startupBoundaryClient{}, worker: &startupBoundaryWorker{}}
	h.service = NewService(newMemoryTemporalRepository(), &countingTemporalWorkflowService{}, true, "127.0.0.1:7233", "default", "hai-test")
	h.service.dial = func(context.Context, client.Options) (client.Client, error) { h.dials.Add(1); return h.client, nil }
	h.service.newWorker = func(c client.Client, queue string, options worker.Options) worker.Worker {
		h.factories.Add(1)
		h.options = options
		return h.worker
	}
	return h
}

func TestStartupBoundaryLateSDKStartCannotResurrectShutdownAndCleanupIsJoined(t *testing.T) {
	h := newStartupBoundaryHarness()
	g := lifecycle.New(context.Background())
	h.service.workerContext = g.Context()
	entered, release, cleanupEntered, cleanupRelease, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var onceStart, onceCleanup sync.Once
	h.worker.start = func() error { close(entered); <-release; return nil }
	h.worker.stop = func() { close(cleanupEntered); <-cleanupRelease }
	defer func() {
		onceStart.Do(func() { close(release) })
		onceCleanup.Do(func() { close(cleanupRelease) })
		waitForStartupBoundary(t, done, "late SDK startup/cleanup")
		finishTemporalGroup(t, g)
	}()
	go func() { defer close(done); h.service.StartWorker() }()
	waitForStartupBoundary(t, entered, "SDK start")
	state := h.service.Status()
	if !state.WorkerStarting || state.WorkerStarted {
		t.Fatalf("unconfirmed SDK start became ready: %+v", state)
	}
	h.service.stopWorker()
	onceStart.Do(func() { close(release) })
	waitForStartupBoundary(t, cleanupEntered, "late SDK cleanup")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown missed unfinished SDK cleanup: %v", err)
	}
	if state := h.service.Status(); state.WorkerStarted || !state.WorkerStarting || state.WorkerError != "backend is shutting down" {
		t.Fatalf("late SDK success resurrected service: %+v", state)
	}
	if h.client.closes.Load() != 0 {
		t.Fatal("client closed before SDK worker cleanup finished")
	}
	onceCleanup.Do(func() { close(cleanupRelease) })
	waitForStartupBoundary(t, done, "startup cleanup")
	finishTemporalGroup(t, g)
	h.service.StartWorker()
	h.service.stopWorker()
	if h.worker.starts.Load() != 1 || h.worker.stops.Load() != 1 || h.client.closes.Load() != 1 || h.dials.Load() != 1 {
		t.Fatal("stopped startup reopened or duplicated SDK cleanup")
	}
	if state := h.service.Status(); state.WorkerStarting || state.WorkerStarted {
		t.Fatalf("finished cleanup still claimed active work: %+v", state)
	}
}

func TestStartupBoundaryHealthyWorkerOutlivesRequestAndIsStoppedBySupervisor(t *testing.T) {
	h := newStartupBoundaryHarness()
	g := lifecycle.New(context.Background())
	h.service.workerContext = g.Context()
	defer func() { finishTemporalGroup(t, g); h.service.stopWorker() }()
	request, cancel := context.WithCancel(context.Background())
	h.service.StartWorkerContext(request)
	cancel()
	if state := h.service.Status(); !state.WorkerStarted || state.WorkerStarting || state.WorkerError != "" {
		t.Fatalf("successful startup did not publish actual readiness: %+v", state)
	}
	if h.options.BackgroundActivityContext == nil || h.options.BackgroundActivityContext.Err() != nil {
		t.Fatal("request completion canceled the published worker's activity context")
	}
	if h.worker.workflows != 1 || h.worker.activities != 1 {
		t.Fatal("governed workflow/activity was not registered exactly once")
	}
	h.service.StartWorkerEventually(g.Context())
	h.service.StartWorker()
	finishTemporalGroup(t, g)
	if h.options.BackgroundActivityContext.Err() == nil || h.worker.stops.Load() != 1 || h.client.closes.Load() != 1 || h.dials.Load() != 1 {
		t.Fatal("supervisor did not own the published worker or retry duplicated startup")
	}
}

func TestStartupBoundaryCanceledOrUnconfirmedClientIsClosedWithoutWorkerCreation(t *testing.T) {
	for _, tc := range []string{"late client", "error with client", "nil client", "nil worker"} {
		t.Run(tc, func(t *testing.T) {
			h := newStartupBoundaryHarness()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.service.dial = func(context.Context, client.Options) (client.Client, error) {
				switch tc {
				case "late client":
					cancel()
				case "error with client":
					return h.client, errors.New("password=synthetic-startup-secret")
				case "nil client":
					return nil, nil
				}
				return h.client, nil
			}
			if tc == "nil worker" {
				h.service.newWorker = func(client.Client, string, worker.Options) worker.Worker { return nil }
			}
			h.service.StartWorkerContext(ctx)
			if state := h.service.Status(); state.WorkerStarted || state.WorkerStarting || state.WorkerError == "" {
				t.Fatalf("unconfirmed start was accepted: %+v", state)
			}
			wantCloses := int32(1)
			if tc == "nil client" {
				wantCloses = 0
			}
			if h.client.closes.Load() != wantCloses || h.worker.starts.Load() != 0 || h.factories.Load() != 0 {
				t.Fatal("failed/unconfirmed client was leaked or reached SDK worker startup")
			}
		})
	}
}

func TestStartupBoundaryHealthUsesEarlierRequestDeadlineAndClosesClient(t *testing.T) {
	h := newStartupBoundaryHarness()
	request, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	deadline, _ := request.Deadline()
	h.client.health = func(ctx context.Context) error {
		got, ok := ctx.Deadline()
		if !ok || !got.Equal(deadline) {
			t.Error("health timeout extended the earlier request deadline")
		}
		if state := h.service.Status(); !state.WorkerStarting || state.WorkerStarted {
			t.Error("health check blocked or falsely reported ready")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	h.service.StartWorkerContext(request)
	if h.client.closes.Load() != 1 || h.factories.Load() != 0 || h.worker.starts.Load() != 0 {
		t.Fatal("timed out health probe created a worker or leaked the client")
	}
}

func TestStartupBoundaryFailedSDKStartStopsPartialWorkerAndClosesClient(t *testing.T) {
	h := newStartupBoundaryHarness()
	h.worker.start = func() error { return errors.New("password=synthetic-startup-secret") }
	h.service.StartWorker()
	if h.worker.starts.Load() != 1 || h.worker.stops.Load() != 1 || h.client.closes.Load() != 1 {
		t.Fatal("failed SDK startup did not clean up partial worker/client")
	}
	if state := h.service.Status(); state.WorkerStarted || state.WorkerStarting || state.WorkerError != "could not start the governed follow-up worker" {
		t.Fatalf("failed startup was ready or leaked SDK error details: %+v", state)
	}
	// A failed attempt may be explicitly retried after its cleanup, unlike shutdown.
	h.worker = &startupBoundaryWorker{}
	h.client = &startupBoundaryClient{}
	h.service.StartWorker()
	if state := h.service.Status(); !state.WorkerStarted || state.WorkerStarting {
		t.Fatalf("cleaned-up failure sealed an allowed explicit retry: %+v", state)
	}
	h.service.stopWorker()
}

func TestStartupBoundaryFailedCleanupShowsFailureBeforeSDKResourcesClose(t *testing.T) {
	h := newStartupBoundaryHarness()
	cleanupEntered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.worker.start = func() error { return errors.New("synthetic SDK startup failure") }
	h.worker.stop = func() { close(cleanupEntered); <-release }
	defer func() { once.Do(func() { close(release) }); waitForStartupBoundary(t, done, "failure cleanup") }()
	go func() { defer close(done); h.service.StartWorker() }()
	waitForStartupBoundary(t, cleanupEntered, "SDK cleanup")
	if state := h.service.Status(); !state.WorkerStarting || state.WorkerStarted || state.WorkerError != "could not start the governed follow-up worker" {
		t.Fatalf("known failure was hidden until SDK cleanup: %+v", state)
	}
	h.service.StartWorker()
	if h.dials.Load() != 1 || h.factories.Load() != 1 || h.client.closes.Load() != 0 {
		t.Fatal("retry overlapped a still-owned failure cleanup")
	}
}

func TestStartupBoundaryRequiresOperationalDependenciesBeforeConnecting(t *testing.T) {
	for _, missing := range []string{"repository", "workflow service"} {
		t.Run(missing, func(t *testing.T) {
			h := newStartupBoundaryHarness()
			if missing == "repository" {
				h.service.repo = nil
			} else {
				h.service.workflows = nil
			}
			h.service.StartWorker()
			if state := h.service.Status(); state.WorkerStarted || state.WorkerStarting || state.WorkerError != "governed follow-up dependencies are unavailable" {
				t.Errorf("missing dependency was reported ready: %+v", state)
			}
			if h.dials.Load() != 0 || h.factories.Load() != 0 || h.worker.starts.Load() != 0 {
				t.Error("uninitialized activity dependencies reached the SDK")
			}
			h.service.stopWorker()
		})
	}
}
