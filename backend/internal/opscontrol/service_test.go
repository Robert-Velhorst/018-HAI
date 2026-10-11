package opscontrol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	broker := newAuthorizedOpsControlTestBroker(
		t,
		t.TempDir(),
		"local-operator",
		"local",
	)
	ops := operations.NewService(operations.NewMemoryRepository())
	return NewService(t.TempDir(), broker, ops, "local-operator", "local")
}

func newAuthorizedOpsControlTestBroker(
	t *testing.T,
	workspace string,
	owner string,
	workspaceID string,
) *executionbroker.Broker {
	t.Helper()
	frameworks, err := frameworkregistry.NewService(
		frameworkregistry.NewMemoryRepository(),
	)
	if err != nil {
		t.Fatalf("new framework registry: %v", err)
	}
	draft, err := frameworks.CreateConstitutionDraft(
		owner,
		frameworkregistry.ConstitutionDraftRequest{
			BaseVersion:   1,
			ChangeSummary: "Activate production-like local execution test policy.",
		},
	)
	if err != nil {
		t.Fatalf("create Constitution draft: %v", err)
	}
	active, err := frameworks.ActivateConstitution(
		owner,
		draft.ID,
		owner,
		frameworkregistry.ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Owner reviewed and approved this test policy.",
		},
	)
	if err != nil {
		t.Fatalf("activate Constitution: %v", err)
	}
	if active.Status != frameworkregistry.ConstitutionActive {
		t.Fatalf("Constitution status = %q, want active", active.Status)
	}
	constitution, err := executionauth.NewConstitutionPolicyAdapter(frameworks)
	if err != nil {
		t.Fatalf("adapt Constitution policy: %v", err)
	}
	authorization, err := executionauth.NewService(
		executionauth.NewMemoryRepository(),
		constitution,
		nil,
		nil,
		nil,
		func() time.Time {
			return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("new execution authorization service: %v", err)
	}
	authorization.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{
			Source: "opscontrol-test",
		}
	})
	broker, err := executionbroker.NewAuthorizedBroker(
		workspace,
		owner,
		workspaceID,
		authorization,
	)
	if err != nil {
		t.Fatalf("new authorized broker: %v", err)
	}
	return broker
}

func TestEmergencyStopEngageDisengageAndControl(t *testing.T) {
	s := newTestService(t)
	if s.Control().EmergencyStop() {
		t.Fatalf("emergency stop must default to disengaged")
	}
	if _, err := s.EngageEmergencyStop("test", "op"); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if !s.Control().EmergencyStop() {
		t.Fatalf("engage must set the stop")
	}
	// Engaged mode is emergency_stopped regardless of stored mode.
	if s.Control().Mode() != autonomypolicy.ModeEmergencyStopped {
		t.Fatalf("engaged control mode must be emergency_stopped, got %s", s.Control().Mode())
	}
	state := s.Control().EmergencyState()
	s.WithExecutionAuthorizer(allowExactControlAuthorization(s.now))
	auth := controlAuthorizationFor(
		t,
		s,
		"op",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)
	if _, err := s.DisengageEmergencyStop(context.Background(), auth); err != nil {
		t.Fatalf("disengage: %v", err)
	}
	if s.Control().EmergencyStop() {
		t.Fatalf("disengage must clear the stop")
	}
}

func TestEmergencyStopPersistenceWaitsForFinalEffectCommitFence(t *testing.T) {
	controller := NewController(t.TempDir())
	if err := controller.SeedInitialState(autonomypolicy.ModeAutonomousSafe, false, "test", time.Now().UTC()); err != nil {
		t.Fatalf("seed controller: %v", err)
	}

	releaseCommit := safety.AcquireExecutionCommitFence()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := controller.Engage("operator stop", "operator", time.Now().UTC())
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		releaseCommit()
		t.Fatalf("emergency stop crossed the active commit fence: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	releaseCommit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("engage emergency stop after commit fence: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("emergency stop persistence did not continue after the commit fence")
	}
	if !controller.EmergencyStop() {
		t.Fatal("emergency stop was not persisted after the final-effect commit fence released")
	}
}

func TestEmergencyStopSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	broker := executionbroker.NewBroker(t.TempDir())
	ops := operations.NewService(operations.NewMemoryRepository())
	s1 := NewService(dir, broker, ops, "u", "local")
	if _, err := s1.EngageEmergencyStop("halt for maintenance", "op"); err != nil {
		t.Fatalf("engage: %v", err)
	}

	// A fresh service rooted at the same dir must load the persisted stop.
	s2 := NewService(dir, broker, ops, "u", "local")
	if !s2.Control().EmergencyStop() {
		t.Fatalf("emergency stop must survive restart (persisted)")
	}
	if s2.Control().EmergencyState().Reason != "halt for maintenance" {
		t.Fatalf("persisted reason must survive restart")
	}
}

func TestPersistedEmergencyStopDrivesSharedSafetyProvider(t *testing.T) {
	dir := t.TempDir()
	broker := executionbroker.NewBroker(t.TempDir())
	ops := operations.NewService(operations.NewMemoryRepository())
	s1 := NewService(dir, broker, ops, "u", "local")
	if _, err := s1.EngageEmergencyStop("operator maintenance", "op"); err != nil {
		t.Fatalf("engage: %v", err)
	}

	s2 := NewService(dir, broker, ops, "u", "local")
	restore := safety.SetEmergencyStopProvider(s2.Control())
	defer restore()

	decision := safety.EvaluateEmergencyStop()
	if !decision.Active || decision.Source != "persisted_control" || decision.Reason != "operator maintenance" {
		t.Fatalf("shared decision = %#v", decision)
	}
}

func TestUnreadablePersistedStateFailsClosed(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(stateDir, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("create invalid state root: %v", err)
	}
	broker := executionbroker.NewBroker(t.TempDir())
	ops := operations.NewService(operations.NewMemoryRepository())
	s := NewService(stateDir, broker, ops, "u", "local")
	restore := safety.SetEmergencyStopProvider(s.Control())
	defer restore()

	if !s.Control().EmergencyStop() {
		t.Fatalf("unreadable persisted state must engage the fail-closed stop")
	}
	decision := safety.EvaluateEmergencyStop()
	if !decision.Active || decision.Source != "persisted_control_error" {
		t.Fatalf("shared decision = %#v, want fail-closed provider error", decision)
	}
	readiness := s.Readiness(context.Background())
	for _, gate := range readiness.Gates {
		if gate.Name == "emergency_stop_works" {
			if gate.Status != GateFail || !strings.Contains(gate.Evidence, "unavailable") {
				t.Fatalf("readiness gate = %#v, want explicit failure", gate)
			}
			return
		}
	}
	t.Fatalf("readiness omitted emergency_stop_works gate")
}

func TestVerifyEmergencyStopHaltsProcessing(t *testing.T) {
	s := newTestService(t)
	processedCalls := 0
	// A runner that would "process" 3 ops if it ran unguarded. The verify wraps
	// it with the emergency stop engaged; a correct system must report the
	// runner's honest processed count — which the caller's worker forces to 0.
	// Here we simulate a compliant worker: it checks the stop and returns 0.
	s.SetBackgroundRunner(func(ctx context.Context) (int, error) {
		processedCalls++
		if s.Control().EmergencyStop() {
			return 0, nil // compliant: halted
		}
		return 3, nil
	})
	v, err := s.VerifyEmergencyStop(context.Background())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !v.Halted || v.ProcessedDuringStop != 0 {
		t.Fatalf("emergency stop must halt processing, got halted=%v processed=%d", v.Halted, v.ProcessedDuringStop)
	}
	if processedCalls != 1 {
		t.Fatalf("verify must run the background pass exactly once")
	}
	// Prior state (disengaged) must be restored.
	if s.Control().EmergencyStop() {
		t.Fatalf("verify must restore the prior disengaged state")
	}
}

func TestVerifyDetectsNonHaltingStop(t *testing.T) {
	s := newTestService(t)
	// A broken worker that ignores the emergency stop.
	s.SetBackgroundRunner(func(ctx context.Context) (int, error) { return 5, nil })
	v, err := s.VerifyEmergencyStop(context.Background())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Halted {
		t.Fatalf("a non-halting stop must be reported as NOT halted")
	}
}

func TestRecoveryReconcilesStuckOperations(t *testing.T) {
	ops := operations.NewService(operations.NewMemoryRepository())
	broker := executionbroker.NewBroker(t.TempDir())
	s := NewService(t.TempDir(), broker, ops, "u", "local")

	// Create an operation stuck in `running` (as if the process crashed).
	in := operations.NewOperationInput{OwnerUserID: "u", WorkspaceID: "local", Title: "stuck", OperationType: "t", SourceType: "runtime_lab", DedupeKey: "d1"}
	ing, _ := ops.Ingest(in)
	op := ing.Operation
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	cl, _ := ops.Transition(op, operations.StatusClassified, "hai", "", "")
	if _, err := ops.Transition(*cl, operations.StatusReady, "hai", "", ""); err != nil {
		t.Fatalf("transition to ready: %v", err)
	}
	claimed, err := ops.ClaimNext(context.Background(), "u", "local", uuid.New(), 5*time.Millisecond)
	if err != nil || claimed == nil {
		t.Fatalf("claim stuck operation: claim=%#v err=%v", claimed, err)
	}
	if _, err := ops.TransitionClaimed(context.Background(), claimed.Claim, claimed.Operation, operations.StatusRunning, "hai", "", ""); err != nil {
		t.Fatalf("mark operation running: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	rep, err := s.Recover(context.Background())
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if rep.ScannedRunning != 1 || rep.Recovered != 1 {
		t.Fatalf("recovery must move the stuck running op to interrupted, got %+v", rep)
	}
	interrupted, _ := ops.List(operations.Filter{OwnerUserID: "u", WorkspaceID: "local", Status: operations.StatusInterrupted})
	if len(interrupted) != 1 {
		t.Fatalf("stuck op must be recovered to interrupted")
	}
}

func TestRecoveryLeavesLiveClaimAloneAndReportsUnleasedExecution(t *testing.T) {
	ops := operations.NewService(operations.NewMemoryRepository())
	s := NewService(t.TempDir(), executionbroker.NewBroker(t.TempDir()), ops, "u", "local")
	makeRunning := func(key string, lease time.Duration) *operations.ClaimedOperation {
		ingested, err := ops.Ingest(operations.NewOperationInput{OwnerUserID: "u", WorkspaceID: "local", Title: key, OperationType: "t", SourceType: "manual", DedupeKey: key})
		if err != nil {
			t.Fatalf("ingest %s: %v", key, err)
		}
		ingested.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
		classified, err := ops.Transition(ingested.Operation, operations.StatusClassified, "hai", "", "")
		if err != nil {
			t.Fatalf("classify %s: %v", key, err)
		}
		ready, err := ops.Transition(*classified, operations.StatusReady, "hai", "", "")
		if err != nil {
			t.Fatalf("ready %s: %v", key, err)
		}
		claimed, err := ops.ClaimNext(context.Background(), "u", "local", uuid.New(), lease)
		if err != nil || claimed == nil || claimed.Operation.ID != ready.ID {
			t.Fatalf("claim %s: claim=%#v err=%v", key, claimed, err)
		}
		if _, err := ops.TransitionClaimed(context.Background(), claimed.Claim, claimed.Operation, operations.StatusRunning, "hai", "", ""); err != nil {
			t.Fatalf("start %s: %v", key, err)
		}
		return claimed
	}

	live := makeRunning("live", time.Minute)
	report, err := s.Recover(context.Background())
	if err != nil {
		t.Fatalf("recover live claim: %v", err)
	}
	if report.Recovered != 0 || report.LiveRunning != 1 {
		t.Fatalf("live claim recovery report = %+v, want one preserved live running operation", report)
	}
	stored, err := ops.Get("u", "local", live.Operation.ID)
	if err != nil || stored.Status != string(operations.StatusRunning) {
		t.Fatalf("live operation after recovery = %#v, %v", stored, err)
	}

	unleased, err := ops.Ingest(operations.NewOperationInput{OwnerUserID: "u", WorkspaceID: "local", Title: "legacy", OperationType: "t", SourceType: "manual", DedupeKey: "legacy"})
	if err != nil {
		t.Fatalf("ingest unleased operation: %v", err)
	}
	unleased.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	classified, err := ops.Transition(unleased.Operation, operations.StatusClassified, "hai", "", "")
	if err != nil {
		t.Fatalf("classify unleased operation: %v", err)
	}
	ready, err := ops.Transition(*classified, operations.StatusReady, "hai", "", "")
	if err != nil {
		t.Fatalf("ready unleased operation: %v", err)
	}
	if _, err := ops.Transition(*ready, operations.StatusRunning, "hai", "", "legacy running state"); err != nil {
		t.Fatalf("create unleased running state: %v", err)
	}
	report, err = s.Recover(context.Background())
	if err != nil {
		t.Fatalf("recover unleased state: %v", err)
	}
	if report.UnleasedRunning != 1 || report.Recovered != 0 {
		t.Fatalf("unleased recovery report = %+v, want one visible unleased operation and no recovery", report)
	}
}

func TestReadinessIsTruthfulOffWindows(t *testing.T) {
	s := newTestService(t)
	r := s.Readiness(context.Background())
	if r.IsWindows {
		if r.OperatingSystem != "windows" {
			t.Fatalf("isWindows must match OS")
		}
	} else {
		// Windows gate must be pending, not passed, off-Windows.
		var winGate *ReadinessGate
		for i := range r.Gates {
			if r.Gates[i].Name == "windows_version_build" {
				winGate = &r.Gates[i]
			}
		}
		if winGate == nil || winGate.Status != GatePending {
			t.Fatalf("windows gate must be pending off-Windows")
		}
		if !r.TargetVerifyPending {
			t.Fatalf("off-Windows readiness must flag target-machine verification pending")
		}
	}
	// Docker is never required.
	if r.Docker.Required {
		t.Fatalf("docker must not be marked required")
	}
	// The safe worker gate should pass (broker has a workspace).
	for _, g := range r.Gates {
		if g.Name == "local_safe_worker_run" && g.Status != GatePass {
			t.Fatalf("safe worker gate should pass with a configured workspace")
		}
	}
}

func TestReadinessFailsClosedWithoutExecutionAuthorization(t *testing.T) {
	broker := executionbroker.NewBroker(t.TempDir())
	ops := operations.NewService(operations.NewMemoryRepository())
	s := NewService(t.TempDir(), broker, ops, "u", "local")

	readiness := s.Readiness(context.Background())
	for _, gate := range readiness.Gates {
		if gate.Name != "local_safe_worker_run" {
			continue
		}
		if gate.Status != GateFail {
			t.Fatalf("unauthorized safe-worker gate = %s, want fail", gate.Status)
		}
		if !strings.Contains(gate.Evidence, "authorization verifier not configured") {
			t.Fatalf("safe-worker evidence hid authorization failure: %q", gate.Evidence)
		}
		return
	}
	t.Fatal("readiness omitted local_safe_worker_run gate")
}

func TestSetModeValidates(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SetMode(context.Background(), "not_a_mode", ControlAuthorization{}); err == nil {
		t.Fatalf("invalid mode must be rejected")
	}
	if m, err := s.SetMode(
		context.Background(),
		string(autonomypolicy.ModeReadOnly),
		ControlAuthorization{},
	); err != nil || m != string(autonomypolicy.ModeReadOnly) {
		t.Fatalf("valid mode must be accepted, got %s err=%v", m, err)
	}
}
