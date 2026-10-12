package automation

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func TestAPILaunchLinearizesEmergencyStopThroughRequestWrite(t *testing.T) {
	response := newHeldEmergencyStopResponse(t)
	server := httptest.NewServer(response.handler)
	t.Cleanup(server.Close)
	t.Cleanup(response.release)

	store := newPersistedEmergencyStopStore(t)
	service := &service{executionAuth: allowingExecutionAuthorizer{}}
	exerciseHTTPStopAdmissionRace(
		t,
		store,
		response,
		func(ctx context.Context) launchExecution {
			automation := &models.Automation{
				ID: uuid.New(), LaunchType: "api", LaunchTarget: "POST " + server.URL,
				ExpectedHTTPStatus: http.StatusNoContent,
			}
			return service.executeAPILaunch(
				automation,
				launchBindingDispatchFixture(automation, TaskLaunchRequest{OwnerIdentity: "alice", ExecutionContext: ctx}),
				uuid.New(), time.Now().UTC(), nil,
			)
		},
	)
}

func TestDockerLaunchLinearizesEmergencyStopThroughRequestWrite(t *testing.T) {
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "safe-container")
	response := newHeldEmergencyStopResponse(t)
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("Unix sockets are unavailable on this platform: %v", err)
	}
	server := &http.Server{Handler: response.handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		response.release()
		_ = server.Close()
		_ = listener.Close()
	})
	t.Setenv("AUTOMATION_DOCKER_SOCKET", socketPath)

	store := newPersistedEmergencyStopStore(t)
	service := &service{executionAuth: allowingExecutionAuthorizer{}}
	exerciseHTTPStopAdmissionRace(
		t,
		store,
		response,
		func(ctx context.Context) launchExecution {
			automation := &models.Automation{
				ID: uuid.New(), LaunchType: "docker_service", LaunchTarget: "safe-container",
				ServiceName: "safe-container",
			}
			return service.executeDockerLaunch(
				automation,
				launchBindingDispatchFixture(automation, TaskLaunchRequest{OwnerIdentity: "alice", ExecutionContext: ctx}),
				uuid.New(), time.Now().UTC(), nil,
			)
		},
	)
}

func TestScriptStartLinearizesEmergencyStopAndWaitRunsAfterFenceRelease(t *testing.T) {
	store := newPersistedEmergencyStopStore(t)
	var sequence atomic.Int64
	var startOrder atomic.Int64
	startEntered := make(chan struct{})
	allowStart := make(chan struct{})
	startReturned := make(chan struct{})
	releaseChild := filepath.Join(t.TempDir(), "release-child")
	cmd := exec.Command(os.Args[0], "-test.run=^TestEmergencyStopAdmissionChildProcess$")
	cmd.Env = append(os.Environ(),
		"HAI_AUTOMATION_ADMISSION_CHILD=1",
		"HAI_AUTOMATION_ADMISSION_RELEASE="+releaseChild,
	)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	type scriptAdmission struct {
		decision     safety.EmergencyStopDecision
		admissionErr error
		startErr     error
	}
	admissionDone := make(chan scriptAdmission, 1)
	go func() {
		decision, admissionErr, startErr := startScriptWithEmergencyStopAdmission(context.Background(), func() error {
			close(startEntered)
			<-allowStart
			startErr := cmd.Start()
			if startErr == nil {
				startOrder.Store(sequence.Add(1))
				close(startReturned)
			}
			return startErr
		})
		admissionDone <- scriptAdmission{decision: decision, admissionErr: admissionErr, startErr: startErr}
	}()
	waitEmergencyStopSignal(t, startEntered, "script start admission")
	stopDone, stopAttempted, stopOrder := engageEmergencyStopAsync(store, &sequence)
	waitEmergencyStopSignal(t, stopAttempted, "persisted emergency-stop mutation")
	assertEmergencyStopNotPersisted(t, store, stopDone)
	close(allowStart)
	waitEmergencyStopSignal(t, startReturned, "cmd.Start completion")
	stopResult := waitEmergencyStopEngage(t, stopDone)
	if stopResult.err != nil || !stopResult.state.Engaged {
		t.Fatalf("EmergencyStopStore.Engage = %#v, %v; want persisted engaged state", stopResult.state, stopResult.err)
	}
	if startOrder.Load() == 0 || stopOrder.Load() <= startOrder.Load() {
		t.Fatalf("script start order=%d stop persistence order=%d; want cmd.Start before persisted stop", startOrder.Load(), stopOrder.Load())
	}
	admission := <-admissionDone
	if admission.admissionErr != nil || admission.startErr != nil || admission.decision.Active {
		t.Fatalf("script admission = %#v; want successful start admitted before stop", admission)
	}

	var startsAfterStop atomic.Int32
	decision, admissionErr, startErr := startScriptWithEmergencyStopAdmission(context.Background(), func() error {
		startsAfterStop.Add(1)
		return nil
	})
	if admissionErr != nil || !decision.Active || startErr != nil || startsAfterStop.Load() != 0 {
		t.Fatalf("post-stop script admission decision=%#v err=%v startErr=%v starts=%d; want blocked without Start", decision, admissionErr, startErr, startsAfterStop.Load())
	}

	if err := os.WriteFile(releaseChild, []byte("done"), 0o600); err != nil {
		t.Fatalf("release child process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for script process after stop persistence: %v", err)
	}
}

func TestEmergencyStopAdmissionChildProcess(t *testing.T) {
	if os.Getenv("HAI_AUTOMATION_ADMISSION_CHILD") != "1" {
		return
	}
	releasePath := os.Getenv("HAI_AUTOMATION_ADMISSION_RELEASE")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(releasePath); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("script admission child was not released")
}

func exerciseHTTPStopAdmissionRace(
	t *testing.T,
	store *opscontrol.EmergencyStopStore,
	response *heldEmergencyStopResponse,
	launch func(context.Context) launchExecution,
) {
	t.Helper()
	var order atomic.Int64
	barrier := newEmergencyStopHTTPTraceBarrier(&order)
	t.Cleanup(barrier.releaseAll)
	launchDone := make(chan launchExecution, 1)
	requestContext := httptrace.WithClientTrace(context.Background(), barrier.trace())
	go func() { launchDone <- launch(requestContext) }()
	waitEmergencyStopSignal(t, barrier.gotConn, "HTTP connection admission")

	stopDone, stopAttempted, stopOrder := engageEmergencyStopAsync(store, &order)
	waitEmergencyStopSignal(t, stopAttempted, "persisted emergency-stop mutation")
	assertEmergencyStopNotPersisted(t, store, stopDone)
	barrier.allowRequestWrite()
	waitEmergencyStopSignal(t, barrier.wroteRequest, "HTTP request write")
	waitEmergencyStopSignal(t, response.received, "HTTP side effect")
	stopResult := waitEmergencyStopEngage(t, stopDone)
	if stopResult.err != nil || !stopResult.state.Engaged {
		t.Fatalf("EmergencyStopStore.Engage = %#v, %v; want persisted engaged state", stopResult.state, stopResult.err)
	}
	if barrier.writeOrder.Load() == 0 || stopOrder.Load() == 0 {
		t.Fatalf("HTTP request-write marker=%d stop persistence marker=%d; want both events", barrier.writeOrder.Load(), stopOrder.Load())
	}
	// WroteRequest observes a completed transport write; it does not hold the
	// admission fence. The stop may persist while the remote response is held.
	if response.calls.Load() != 1 {
		t.Fatalf("HTTP effects started = %d, want one admitted request", response.calls.Load())
	}

	postStop := launch(context.Background())
	if postStop.Status != "blocked" {
		t.Fatalf("post-stop launch = %#v, want blocked", postStop)
	}
	if response.calls.Load() != 1 {
		t.Fatalf("HTTP effects started after persisted stop = %d, want one", response.calls.Load())
	}

	response.release()
	select {
	case result := <-launchDone:
		if result.Status != "completed" {
			t.Fatalf("admitted HTTP launch = %#v, want completed after response release", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP launch did not finish after releasing the held response")
	}
}

type emergencyStopStoreProvider struct {
	store *opscontrol.EmergencyStopStore
}

func (p emergencyStopStoreProvider) EmergencyStopStatus() (bool, string, error) {
	state, err := p.store.Status()
	if err != nil {
		return true, "", err
	}
	return state.Engaged, state.Reason, nil
}

func newPersistedEmergencyStopStore(t *testing.T) *opscontrol.EmergencyStopStore {
	t.Helper()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	store := opscontrol.NewEmergencyStopStore(t.TempDir())
	if err := store.SeedIfAbsent(false, "test", time.Now().UTC()); err != nil {
		t.Fatalf("seed persisted emergency-stop state: %v", err)
	}
	restore := safety.SetEmergencyStopProvider(emergencyStopStoreProvider{store: store})
	t.Cleanup(restore)
	return store
}

type emergencyStopEngageResult struct {
	state opscontrol.EmergencyStopState
	err   error
}

func engageEmergencyStopAsync(
	store *opscontrol.EmergencyStopStore,
	order *atomic.Int64,
) (<-chan emergencyStopEngageResult, <-chan struct{}, *atomic.Int64) {
	done := make(chan emergencyStopEngageResult, 1)
	attempted := make(chan struct{})
	stopOrder := &atomic.Int64{}
	go func() {
		close(attempted)
		state, err := store.Engage("test stop", "operator", time.Now().UTC())
		stopOrder.Store(order.Add(1))
		done <- emergencyStopEngageResult{state: state, err: err}
	}()
	return done, attempted, stopOrder
}

func assertEmergencyStopNotPersisted(
	t *testing.T,
	store *opscontrol.EmergencyStopStore,
	stopDone <-chan emergencyStopEngageResult,
) {
	t.Helper()
	select {
	case result := <-stopDone:
		t.Fatalf("emergency stop persisted before dispatch admission: %#v, %v", result.state, result.err)
	case <-time.After(75 * time.Millisecond):
	}
	state, err := store.Status()
	if err != nil {
		t.Fatalf("read persisted emergency-stop state: %v", err)
	}
	if state.Engaged {
		t.Fatal("emergency-stop state persisted while dispatch admission was held")
	}
}

func waitEmergencyStopEngage(
	t *testing.T,
	done <-chan emergencyStopEngageResult,
) emergencyStopEngageResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("emergency stop did not persist after dispatch admission released")
		return emergencyStopEngageResult{}
	}
}

func waitEmergencyStopSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

type emergencyStopHTTPTraceBarrier struct {
	gotConn          chan struct{}
	allowWrite       chan struct{}
	wroteRequest     chan struct{}
	gotConnOnce      sync.Once
	wroteRequestOnce sync.Once
	allowWriteOnce   sync.Once
	order            *atomic.Int64
	writeOrder       atomic.Int64
}

func newEmergencyStopHTTPTraceBarrier(order *atomic.Int64) *emergencyStopHTTPTraceBarrier {
	return &emergencyStopHTTPTraceBarrier{
		gotConn:      make(chan struct{}),
		allowWrite:   make(chan struct{}),
		wroteRequest: make(chan struct{}),
		order:        order,
	}
}

func (b *emergencyStopHTTPTraceBarrier) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) {
			b.gotConnOnce.Do(func() {
				close(b.gotConn)
				<-b.allowWrite
			})
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			b.wroteRequestOnce.Do(func() {
				b.writeOrder.Store(b.order.Add(1))
				close(b.wroteRequest)
			})
		},
	}
}

func (b *emergencyStopHTTPTraceBarrier) allowRequestWrite() {
	b.allowWriteOnce.Do(func() { close(b.allowWrite) })
}

func (b *emergencyStopHTTPTraceBarrier) releaseAll() {
	b.allowRequestWrite()
}

type heldEmergencyStopResponse struct {
	handler  http.Handler
	calls    atomic.Int32
	received chan struct{}
	releaseC chan struct{}
	releaseO sync.Once
}

func newHeldEmergencyStopResponse(t *testing.T) *heldEmergencyStopResponse {
	t.Helper()
	response := &heldEmergencyStopResponse{
		received: make(chan struct{}),
		releaseC: make(chan struct{}),
	}
	response.handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if response.calls.Add(1) == 1 {
			close(response.received)
		}
		<-response.releaseC
		writer.WriteHeader(http.StatusNoContent)
	})
	t.Cleanup(response.release)
	return response
}

func (r *heldEmergencyStopResponse) release() {
	r.releaseO.Do(func() { close(r.releaseC) })
}
