package agentruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func TestDeepSeekHostAdmissionRejectsStopCommittedDuringPreflight(t *testing.T) {
	stop := installAdmissionStopProvider(t)
	dispatcher := &admissionTestDispatcher{
		availabilityEntered: make(chan struct{}),
		availabilityRelease: make(chan struct{}),
	}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
	}
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- adapter.ExecuteTask(context.Background(), approvedRuntimeTask("host-stop-wins", "inspect workspace"))
	}()
	waitAdmissionSignal(t, dispatcher.availabilityEntered, "host preflight")

	commitAdmissionTestStop(t, stop)
	close(dispatcher.availabilityRelease)
	result := waitAdmissionResult(t, resultCh)
	if result.Status != "blocked" || dispatcher.enqueueCalls.Load() != 0 {
		t.Fatalf("result=%#v enqueue calls=%d; stop committed before admission must prevent durable host work", result, dispatcher.enqueueCalls.Load())
	}
}

func TestDeepSeekHostAdmissionLinearizesBeforeConcurrentStop(t *testing.T) {
	stop := installAdmissionStopProvider(t)
	dispatcher := &admissionTestDispatcher{
		enqueueEntered: make(chan struct{}),
		enqueueRelease: make(chan struct{}),
	}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
	}
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- adapter.ExecuteTask(context.Background(), approvedRuntimeTask("host-admit-wins", "inspect workspace"))
	}()
	waitAdmissionSignal(t, dispatcher.enqueueEntered, "durable host enqueue")

	stopAttempted := make(chan struct{})
	stopCommitted := make(chan struct{})
	go func() {
		close(stopAttempted)
		release := safety.AcquireEmergencyStopMutationFence()
		stop.Store(true)
		release()
		close(stopCommitted)
	}()
	waitAdmissionSignal(t, stopAttempted, "stop mutation attempt")
	assertAdmissionSignalBlocked(t, stopCommitted, "stop mutation crossed an in-flight durable admission")
	close(dispatcher.enqueueRelease)
	waitAdmissionSignal(t, stopCommitted, "stop mutation after durable admission")
	result := waitAdmissionResult(t, resultCh)
	if result.Status != "queued" || dispatcher.job == nil || dispatcher.enqueueCalls.Load() != 1 {
		t.Fatalf("result=%#v job=%#v enqueue calls=%d; admission-first ordering must retain the durable job", result, dispatcher.job, dispatcher.enqueueCalls.Load())
	}
}

func TestDeepSeekHostAdmissionAndOwnerStopShareAuthorizedIdentity(t *testing.T) {
	installAdmissionStopProvider(t)
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	task := Task{
		ID:               " padded-task ",
		Prompt:           "inspect workspace",
		OwnerIdentity:    " alice ",
		ProjectKey:       " project-1 ",
		HumanApproved:    true,
		ApprovalSourceID: " task-review:11111111-1111-4111-8111-111111111111 ",
	}
	task = withValidFinalEffectProof(adapter.RuntimeID(), task, adapter.Info())
	var authorized FinalEffectAuthorizationRequest
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			authorized = request
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)

	queued := registry.Execute(context.Background(), adapter.RuntimeID(), task)
	if queued.Status != "queued" {
		t.Fatalf("host admission result = %#v; want queued", queued)
	}
	if authorized.TaskID != dispatcher.task.TaskID || authorized.OwnerIdentity != dispatcher.task.OwnerIdentity ||
		dispatcher.task.TaskID != "padded-task" || dispatcher.task.OwnerIdentity != "alice" {
		t.Fatalf("authorized identity differs from host admission: request=%#v task=%#v", authorized, dispatcher.task)
	}
	stop := registry.StopTask(context.Background(), adapter.RuntimeID(), authorized.TaskID, authorized.OwnerIdentity)
	if stop.Status != "cancelled" || dispatcher.job == nil || dispatcher.job.Status != hostruntime.StatusCancelled {
		t.Fatalf("owner-bound stop did not revoke the admitted task: stop=%#v job=%#v", stop, dispatcher.job)
	}
}

func TestDeepSeekLocalStartRejectsStopCommittedDuringPreflight(t *testing.T) {
	stop := installAdmissionStopProvider(t)
	versionProbeEntered := make(chan struct{})
	versionProbeRelease := make(chan struct{})
	var startCalls atomic.Int32
	adapter := newAdmissionTestLocalAdapter(t, func(context.Context) (string, error) {
		close(versionProbeEntered)
		<-versionProbeRelease
		return "0.1.7-alpha.2", nil
	})
	adapter.processStarter = func(cmd *exec.Cmd) error {
		startCalls.Add(1)
		return cmd.Start()
	}
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- adapter.ExecuteTask(context.Background(), approvedRuntimeTask("local-stop-wins", "inspect workspace"))
	}()
	waitAdmissionSignal(t, versionProbeEntered, "local version preflight")

	commitAdmissionTestStop(t, stop)
	close(versionProbeRelease)
	result := waitAdmissionResult(t, resultCh)
	if result.Status != "blocked" || startCalls.Load() != 0 {
		t.Fatalf("result=%#v process starts=%d; stop committed before local admission must prevent task process start", result, startCalls.Load())
	}
}

func TestDeepSeekLocalStartReleasesFenceBeforeWaitAndKeepsStopCancellation(t *testing.T) {
	stop := installAdmissionStopProvider(t)
	workspace := t.TempDir()
	stateDir := filepath.Join(workspace, ".dsh-state")
	waitForFile := filepath.Join(t.TempDir(), "release-child")
	t.Setenv("HAI_DSH_TEST_WAIT_FOR_FILE", waitForFile)
	adapter := newAdmissionTestLocalAdapter(t, func(context.Context) (string, error) {
		return "0.1.7-alpha.2", nil
	})
	adapter.workspace = workspace
	adapter.workspaceRoot = workspace
	adapter.stateDir = stateDir
	adapter.envAllow = []string{"HAI_DSH_TEST_WAIT_FOR_FILE"}
	processStarted := make(chan struct{})
	adapter.processStarter = func(cmd *exec.Cmd) error {
		err := cmd.Start()
		if err == nil {
			close(processStarted)
		}
		return err
	}

	executionCtx, cancelExecution := safety.WithEmergencyStop(context.Background())
	defer cancelExecution()
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- adapter.ExecuteTask(executionCtx, approvedRuntimeTask("local-admit-wins", "inspect workspace"))
	}()
	waitAdmissionSignal(t, processStarted, "local task process start")

	stopAttempted := make(chan struct{})
	stopCommitted := make(chan struct{})
	go func() {
		close(stopAttempted)
		release := safety.AcquireEmergencyStopMutationFence()
		stop.Store(true)
		release()
		close(stopCommitted)
	}()
	waitAdmissionSignal(t, stopAttempted, "stop mutation attempt")
	select {
	case <-stopCommitted:
	case <-time.After(2 * time.Second):
		_ = os.WriteFile(waitForFile, []byte("release"), 0o600)
		_ = waitAdmissionResult(t, resultCh)
		t.Fatal("stop mutation could not proceed while the admitted child was running; process wait must be outside the fence")
	}
	result := waitAdmissionResult(t, resultCh)
	if !errorsIsStopCancellation(executionCtx) {
		t.Fatalf("execution context cause=%v; stop should still cancel already-admitted work", context.Cause(executionCtx))
	}
	if result.Status == "completed" {
		t.Fatalf("result=%#v; a cancelled process must not be reported as completed", result)
	}
}

func TestBoundedHostAdmissionReleasesStopFenceWhenEnqueueStalls(t *testing.T) {
	installAdmissionStopProvider(t)
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	callbackStarted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, _, err := withExecutionAdmission(parent, "deepseek-harness", func(ctx context.Context) error {
			close(callbackStarted)
			<-ctx.Done()
			return ctx.Err()
		})
		result <- err
	}()
	waitAdmissionSignal(t, callbackStarted, "bounded enqueue callback")

	stopCommitted := make(chan struct{})
	go func() {
		release := safety.AcquireEmergencyStopMutationFence()
		release()
		close(stopCommitted)
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("bounded admission error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled enqueue did not return at its admission deadline")
	}
	select {
	case <-stopCommitted:
	case <-time.After(time.Second):
		t.Fatal("emergency-stop mutation remained blocked after bounded admission ended")
	}
}

type admissionTestDispatcher struct {
	availabilityEntered chan struct{}
	availabilityRelease chan struct{}
	enqueueEntered      chan struct{}
	enqueueRelease      chan struct{}
	enqueueCalls        atomic.Int32
	job                 *hostruntime.Job
}

func (d *admissionTestDispatcher) ExecutionAvailability() (bool, string) {
	if d.availabilityEntered != nil {
		close(d.availabilityEntered)
		<-d.availabilityRelease
	}
	return true, ""
}

func (d *admissionTestDispatcher) EnqueueContext(ctx context.Context, task hostruntime.ApprovedTask) (*hostruntime.Job, error) {
	d.enqueueCalls.Add(1)
	d.job = &hostruntime.Job{
		ID: uuid.New(), OwnerIdentity: task.OwnerIdentity, RuntimeID: task.RuntimeID,
		TaskID: task.TaskID, Status: hostruntime.StatusPending,
	}
	if d.enqueueEntered != nil {
		close(d.enqueueEntered)
		select {
		case <-d.enqueueRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.job, nil
}

func (*admissionTestDispatcher) CancelTask(context.Context, string, string, uuid.UUID) (*hostruntime.Job, bool, error) {
	return nil, false, hostruntime.ErrJobNotFound
}

func installAdmissionStopProvider(t *testing.T) *atomic.Bool {
	t.Helper()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	var stopped atomic.Bool
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return stopped.Load(), "test operator stop", nil
	}))
	t.Cleanup(restore)
	return &stopped
}

func commitAdmissionTestStop(t *testing.T, stopped *atomic.Bool) {
	t.Helper()
	release := safety.AcquireEmergencyStopMutationFence()
	stopped.Store(true)
	release()
}

func newAdmissionTestLocalAdapter(t *testing.T, probe func(context.Context) (string, error)) *deepSeekHarnessAdapter {
	t.Helper()
	workspace := t.TempDir()
	return &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, executable: os.Args[0],
		expectedVersion: "0.1.7-alpha.2", versionProbe: probe,
		workspace: workspace, workspaceRoot: workspace,
		stateDir: filepath.Join(workspace, ".dsh-state"),
		timeout:  5 * time.Second, outputLimit: defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}
}

func waitAdmissionSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func assertAdmissionSignalBlocked(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(message)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitAdmissionResult(t *testing.T, results <-chan Result) Result {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runtime result")
		return Result{}
	}
}

func errorsIsStopCancellation(ctx context.Context) bool {
	return ctx.Err() != nil && strings.Contains(strings.ToLower(context.Cause(ctx).Error()), "emergency stop")
}
