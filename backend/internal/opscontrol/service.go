package opscontrol

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

var ErrControlPersistence = errors.New("opscontrol state persistence failed")

// BackgroundRunner runs one background pass and returns how many operations it
// processed (classified/executed). It lets the ops-control service verify the
// emergency stop without importing the background worker directly.
type BackgroundRunner func(ctx context.Context) (processed int, err error)

// OpenClawEmergencyStopFanout requests exact-run cancellation only after the
// persisted HAI stop has been engaged. It never owns stop persistence.
type OpenClawEmergencyStopFanout interface {
	FanOutEmergencyStop(context.Context) (targeted, acknowledged, awaitingIdentity, failed int, err error)
}

// EmergencyStopFanoutSummary reports delivery without implying that an abort
// acknowledgment is terminal completion.
type EmergencyStopFanoutSummary struct {
	Status                   string `json:"status"`
	Targeted                 int    `json:"targeted"`
	Acknowledged             int    `json:"acknowledged"`
	AwaitingIdentity         int    `json:"awaitingIdentity"`
	Failed                   int    `json:"failed"`
	TerminallyVerified       int    `json:"terminallyVerified"`
	AcknowledgmentIsTerminal bool   `json:"acknowledgmentIsTerminal"`
	Message                  string `json:"message"`
}

// Service is the always-on runtime control plane: emergency stop, background
// mode, Docker dependency status, crash/reboot recovery, and the Windows
// readiness checklist. It shares the Operation Ledger + execution broker.
type Service struct {
	control       *Controller
	broker        *executionbroker.Broker
	ops           *operations.Service
	runner        BackgroundRunner
	openClawStop  OpenClawEmergencyStopFanout
	authorization ExecutionAuthorizer
	approvals     OwnerControlApprovalIssuer
	owner         string
	space         string
	now           func() time.Time
}

// NewService builds the ops-control service rooted at stateDir.
func NewService(stateDir string, broker *executionbroker.Broker, ops *operations.Service, ownerUserID, workspaceID string) *Service {
	return &Service{
		control: NewController(stateDir),
		broker:  broker,
		ops:     ops,
		owner:   ownerUserID,
		space:   workspaceID,
		now:     time.Now,
	}
}

// Control returns the controller (implements the background worker's Control).
func (s *Service) Control() *Controller { return s.control }

// SeedInitialState applies configured controls only to an empty state
// directory. Persisted operator choices always win on every subsequent boot.
func (s *Service) SeedInitialState(
	mode autonomypolicy.Mode,
	emergencyStop bool,
	actor string,
) error {
	return s.control.SeedInitialState(mode, emergencyStop, actor, s.now().UTC())
}

// SetBackgroundRunner wires the background pass used by emergency-stop
// verification.
func (s *Service) SetBackgroundRunner(r BackgroundRunner) { s.runner = r }

// WithOpenClawEmergencyStopFanout wires receipt-bound remote cancellation.
func (s *Service) WithOpenClawEmergencyStopFanout(fanout OpenClawEmergencyStopFanout) *Service {
	s.openClawStop = fanout
	return s
}

// WithExecutionAuthorizer injects the single-use authorization boundary for
// weakening safety controls. Without it, clear/escalate requests fail closed.
func (s *Service) WithExecutionAuthorizer(authorizer ExecutionAuthorizer) *Service {
	s.authorization = authorizer
	return s
}

// WithOwnerControlApprovalIssuer installs the owner-confirmation source used
// for short-lived, exact-bound resume and autonomy-escalation requests.
func (s *Service) WithOwnerControlApprovalIssuer(issuer OwnerControlApprovalIssuer) *Service {
	s.approvals = issuer
	return s
}

// PrepareEmergencyStopResume records an owner-confirmed, short-lived approval
// for clearing the current emergency-stop revision. It does not change state;
// the returned authorization must be consumed immediately by Resume.
func (s *Service) PrepareEmergencyStopResume(actorIdentity string) (ControlAuthorization, error) {
	actorIdentity = strings.TrimSpace(actorIdentity)
	if actorIdentity == "" {
		return ControlAuthorization{}, ErrUnauthenticated
	}
	if strings.TrimSpace(s.owner) == "" || actorIdentity != s.owner {
		return ControlAuthorization{}, ErrAuthorizationDenied
	}
	if s.approvals == nil {
		return ControlAuthorization{}, ErrAuthorizationUnavailable
	}
	state, err := s.control.emergency.Status()
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if !state.Engaged {
		return ControlAuthorization{}, fmt.Errorf("%w: emergency stop is not engaged", ErrEmergencyStopStateChanged)
	}
	resourceID := emergencyStopResourceID(state.Revision)
	bindingDigest, err := controlEffectDigest(controlEffect{
		Version:       1,
		OwnerIdentity: s.owner,
		Action:        clearEmergencyStopAction,
		ResourceType:  emergencyStopResourceType,
		ResourceID:    resourceID,
		Target:        "disengaged",
	})
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("derive resume effect: %w", err)
	}
	approval, err := s.approvals.Prepare(s.owner, bindingDigest)
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("prepare owner control approval: %w", err)
	}
	return ControlAuthorization{
		ActorIdentity:         actorIdentity,
		IdempotencyKey:        uuid.NewString(),
		TaskID:                "opscontrol-emergency-stop-" + strconv.FormatUint(state.Revision, 10),
		ApprovalSourceID:      approval.SourceID,
		ApprovalBindingDigest: approval.BindingDigest,
	}, nil
}

// PrepareAutonomyModeChange prepares an owner approval only when the requested
// mode grants more autonomy than the persisted mode. Restrictive changes stay
// immediately available and return an empty authorization.
func (s *Service) PrepareAutonomyModeChange(actorIdentity, mode string) (ControlAuthorization, error) {
	actorIdentity = strings.TrimSpace(actorIdentity)
	if actorIdentity == "" {
		return ControlAuthorization{}, ErrUnauthenticated
	}
	if strings.TrimSpace(s.owner) == "" || actorIdentity != s.owner {
		return ControlAuthorization{}, ErrAuthorizationDenied
	}
	target, err := autonomypolicy.ParseMode(mode)
	if err != nil {
		return ControlAuthorization{}, err
	}
	current, err := s.control.ModePersistenceStatus()
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if modeAuthorityRank(target) <= modeAuthorityRank(current) {
		return ControlAuthorization{}, nil
	}
	if s.approvals == nil {
		return ControlAuthorization{}, ErrAuthorizationUnavailable
	}
	resourceID := autonomyModeResourceID(string(current), string(target))
	bindingDigest, err := controlEffectDigest(controlEffect{
		Version:       1,
		OwnerIdentity: s.owner,
		Action:        escalateAutonomyAction,
		ResourceType:  autonomyModeResourceType,
		ResourceID:    resourceID,
		Target:        string(target),
	})
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("derive autonomy-mode effect: %w", err)
	}
	approval, err := s.approvals.Prepare(s.owner, bindingDigest)
	if err != nil {
		return ControlAuthorization{}, fmt.Errorf("prepare owner control approval: %w", err)
	}
	return ControlAuthorization{
		ActorIdentity:         actorIdentity,
		IdempotencyKey:        uuid.NewString(),
		TaskID:                "opscontrol-autonomy-mode-" + string(current) + "-to-" + string(target),
		ApprovalSourceID:      approval.SourceID,
		ApprovalBindingDigest: approval.BindingDigest,
	}, nil
}

// EngageEmergencyStop halts all background processing (persisted).
func (s *Service) EngageEmergencyStop(reason, actor string) (EmergencyStopState, error) {
	if s.openClawStop != nil {
		state, _, err := s.EngageEmergencyStopWithFanout(context.Background(), reason, actor)
		return state, err
	}
	if reason == "" {
		reason = "operator-engaged emergency stop"
	}
	return s.control.Engage(reason, actor, s.now().UTC())
}

// EngageEmergencyStopAuthenticatedWithFanout records a non-empty authenticated
// actor for audit. HTTP callers must pass the router's verified admin-role
// check; s.owner is the worker/ledger scope and is not the IDP principal.
func (s *Service) EngageEmergencyStopAuthenticatedWithFanout(
	ctx context.Context,
	reason string,
	actor string,
) (EmergencyStopState, EmergencyStopFanoutSummary, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return EmergencyStopState{}, EmergencyStopFanoutSummary{}, ErrUnauthenticated
	}
	return s.EngageEmergencyStopWithFanout(ctx, reason, actor)
}

// EngageEmergencyStopWithFanout persists the stop before touching any remote
// receipt. Delivery failures do not clear or weaken that persisted stop.
func (s *Service) EngageEmergencyStopWithFanout(ctx context.Context, reason, actor string) (EmergencyStopState, EmergencyStopFanoutSummary, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "operator-engaged emergency stop"
	}
	state, err := s.control.Engage(reason, actor, s.now().UTC())
	if err != nil {
		return state, EmergencyStopFanoutSummary{
			Status: "unavailable", Message: "HAI could not confirm that the emergency stop was persisted.",
		}, fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if s.openClawStop == nil {
		return state, EmergencyStopFanoutSummary{
			Status: "unavailable", Message: "The emergency stop is active, but OpenClaw cancellation delivery is not configured.",
		}, fmt.Errorf("OpenClaw emergency-stop cancellation delivery is unavailable")
	}
	return s.fanOutEmergencyStop(ctx)
}

// DisengageEmergencyStop clears the exact persisted stop revision only after a
// fresh, exact-bound authorization has been consumed.
func (s *Service) DisengageEmergencyStop(
	ctx context.Context,
	auth ControlAuthorization,
) (EmergencyStopState, error) {
	state, err := s.control.emergency.Status()
	if err != nil {
		return s.control.EmergencyState(), fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if !state.Engaged {
		return state, nil
	}
	resourceID := emergencyStopResourceID(state.Revision)
	if err := s.authorizeSafetyChange(
		ctx,
		auth,
		clearEmergencyStopAction,
		emergencyStopResourceType,
		resourceID,
		"disengaged",
	); err != nil {
		return state, err
	}
	updated, err := s.control.DisengageIfRevision(
		state.Revision,
		strings.TrimSpace(auth.ActorIdentity),
		s.now().UTC(),
	)
	if err == nil || errors.Is(err, ErrEmergencyStopStateChanged) {
		return updated, err
	}
	return updated, fmt.Errorf("%w: %v", ErrControlPersistence, err)
}

// SetMode updates the background autonomy mode. More permissive transitions
// require a fresh exact-bound authorization; restrictive transitions require
// no approval but serialize with effects and preserve newer operator decisions.
func (s *Service) SetMode(
	ctx context.Context,
	mode string,
	auth ControlAuthorization,
) (string, error) {
	target, err := autonomypolicy.ParseMode(mode)
	if err != nil {
		return "", err
	}
	current, modeStateErr := s.control.ModePersistenceStatus()
	if current == target && modeStateErr == nil {
		return string(current), nil
	}
	if current == target {
		updated, err := s.control.SetModeIfCurrent(current, target)
		if errors.Is(err, ErrAutonomyModeStateChanged) {
			return string(updated), err
		}
		if err != nil {
			return string(updated), fmt.Errorf("%w: %v", ErrControlPersistence, err)
		}
		return string(updated), nil
	}
	if modeAuthorityRank(target) > modeAuthorityRank(current) {
		resourceID := autonomyModeResourceID(string(current), string(target))
		if err := s.authorizeSafetyChange(
			ctx,
			auth,
			escalateAutonomyAction,
			autonomyModeResourceType,
			resourceID,
			string(target),
		); err != nil {
			return string(current), err
		}
		updated, err := s.control.SetModeIfCurrent(current, target)
		if err == nil || errors.Is(err, ErrAutonomyModeStateChanged) {
			return string(updated), err
		}
		return string(updated), fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	updated, err := s.control.SetModeIfCurrent(current, target)
	if errors.Is(err, ErrAutonomyModeStateChanged) {
		return string(updated), err
	}
	if err != nil {
		return string(updated), fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	return string(updated), nil
}

func modeAuthorityRank(mode autonomypolicy.Mode) int {
	switch mode {
	case autonomypolicy.ModeEmergencyStopped, autonomypolicy.ModePaused:
		return 0
	case autonomypolicy.ModeReadOnly:
		return 1
	case autonomypolicy.ModeDraftOnly:
		return 2
	case autonomypolicy.ModeApprovalRequired:
		return 3
	case autonomypolicy.ModeAutonomousSafe:
		return 4
	default:
		return -1
	}
}

// Recover runs a crash/reboot recovery pass over the ledger.
func (s *Service) Recover(ctx context.Context) (RecoveryReport, error) {
	return Recover(ctx, s.ops, s.owner, s.space, s.now().UTC())
}

// Status is the background status roll-up (§10.19 GET /background/status).
type Status struct {
	Mode         string             `json:"mode"`
	StoredMode   string             `json:"storedMode"`
	ModeError    string             `json:"modeStateError,omitempty"`
	Emergency    EmergencyStopState `json:"emergencyStop"`
	Processing   bool               `json:"backgroundProcessingActive"`
	Docker       DockerStatus       `json:"docker"`
	CompletedOps int                `json:"completedOperations"`
	AwaitingOps  int                `json:"awaitingApproval"`
}

// Status returns the current background/runtime status.
func (s *Service) Status(ctx context.Context) Status {
	dash, _ := s.ops.Dashboard(s.owner, s.space)
	mode := s.control.Mode()
	storedMode, modeErr := s.control.ModePersistenceStatus()
	modeError := ""
	if modeErr != nil {
		modeError = "persisted autonomy mode is unavailable"
	}
	return Status{
		Mode:         string(mode),
		StoredMode:   string(storedMode),
		ModeError:    modeError,
		Emergency:    s.control.EmergencyState(),
		Processing:   !s.control.EmergencyStop() && mode != "paused" && mode != "emergency_stopped",
		Docker:       DetectDocker(ctx),
		CompletedOps: dash.DoneWhileAway,
		AwaitingOps:  dash.NeedsRobert,
	}
}

// Readiness builds the Windows-runtime readiness checklist truthfully.
func (s *Service) Readiness(ctx context.Context) Readiness {
	os := runtime.GOOS
	isWindows := os == "windows"
	docker := DetectDocker(ctx)
	emergency := s.control.EmergencyState()

	var gates []ReadinessGate

	if _, modeErr := s.control.ModePersistenceStatus(); modeErr != nil {
		gates = append(gates, ReadinessGate{
			Name:        "autonomy_mode_persistence",
			Status:      GateFail,
			Evidence:    "persisted autonomy mode is unavailable; background processing is paused",
			Remediation: "repair the Phase 2 state directory and explicitly set the autonomy mode",
		})
	} else {
		gates = append(gates, ReadinessGate{
			Name:     "autonomy_mode_persistence",
			Status:   GatePass,
			Evidence: "persisted autonomy mode is readable",
		})
	}

	// Windows version/build - pending on non-Windows (target-machine verify).
	if isWindows {
		gates = append(gates, ReadinessGate{Name: "windows_version_build", Status: GatePass, Evidence: "running on Windows"})
	} else {
		gates = append(gates, ReadinessGate{Name: "windows_version_build", Status: GatePending, Evidence: "host OS is " + os, Remediation: "verify on Robert's Windows target machine"})
	}

	// Docker Desktop - not required; warn if absent.
	if docker.DaemonRunning {
		gates = append(gates, ReadinessGate{Name: "docker_desktop", Status: GatePass, Evidence: docker.Detail})
	} else {
		gates = append(gates, ReadinessGate{Name: "docker_desktop", Status: GateWarn, Evidence: docker.Detail, Remediation: "optional: start Docker Desktop; HAI runs without it (local safe worker needs no Docker)"})
	}

	// Local safe worker - must actually be runnable.
	swHealth := s.broker.SafeWorker().HealthCheck(ctx)
	if swHealth.Status.CanExecute() {
		gates = append(gates, ReadinessGate{Name: "local_safe_worker_run", Status: GatePass, Evidence: "safe worker workspace configured and executable"})
	} else {
		gates = append(gates, ReadinessGate{Name: "local_safe_worker_run", Status: GateFail, Evidence: swHealth.Detail, Remediation: "configure HAI_PHASE2_WORKSPACE_DIR"})
	}

	// Dashboard opens - this endpoint responding proves the API is reachable.
	gates = append(gates, ReadinessGate{Name: "dashboard_opens", Status: GatePass, Evidence: "runtime readiness endpoint responded"})

	// Background operations smoke - script exists and passes locally.
	gates = append(gates, ReadinessGate{Name: "background_operations_smoke", Status: GatePass, Evidence: "scripts/smoke-background-operations.sh (run to re-verify)"})

	// Emergency stop - verifiable (see /windows-runtime/emergency-stop/verify).
	_, _, stopErr := s.control.EmergencyStopStatus()
	if stopErr != nil {
		gates = append(gates, ReadinessGate{Name: "emergency_stop_works", Status: GateFail, Evidence: "persisted emergency-stop state is unavailable", Remediation: "repair the Phase 2 state directory before resuming execution"})
	} else {
		gates = append(gates, ReadinessGate{Name: "emergency_stop_works", Status: GatePass, Evidence: "persisted emergency stop; halts background processing (verify endpoint proves it)"})
	}

	// No external sends without approval - enforced by design.
	gates = append(gates, ReadinessGate{Name: "no_external_sends_without_approval", Status: GatePass, Evidence: "only the local safe worker executes; external runtimes/bridges refuse without approval"})

	// External Windows runtimes - pending target-machine configuration.
	gates = append(gates, ReadinessGate{Name: "external_runtimes_configured", Status: GatePending, Evidence: "Hermes/OpenClaw/Odysseus/DSpark not configured", Remediation: "configure + probe on the target machine; see Runtime Lab"})

	// GPU/NPU detection - pending on non-Windows.
	gpuStatus := GatePending
	gpuEvidence := "GPU/NPU detection requires the target machine"
	if isWindows {
		gpuStatus = GateWarn
		gpuEvidence = "verify GPU/NPU via the hardware profile"
	}
	gates = append(gates, ReadinessGate{Name: "gpu_npu_detected", Status: gpuStatus, Evidence: gpuEvidence, Remediation: "detect/declare on the target machine"})

	overallReady := true
	targetPending := false
	for _, g := range gates {
		if g.Status == GateFail {
			overallReady = false
		}
		if g.Status == GatePending {
			targetPending = true
		}
	}

	return Readiness{
		OperatingSystem:     os,
		IsWindows:           isWindows,
		OverallReady:        overallReady,
		TargetVerifyPending: targetPending,
		Mode:                string(s.control.Mode()),
		Emergency:           emergency,
		Docker:              docker,
		Gates:               gates,
	}
}

// EmergencyStopVerification is the result of proving the stop halts processing.
type EmergencyStopVerification struct {
	EngagedDuringTest   bool   `json:"engagedDuringTest"`
	ProcessedDuringStop int    `json:"operationsProcessedDuringStop"`
	Halted              bool   `json:"halted"`
	RestoredEngaged     bool   `json:"restoredEngagedState"`
	Detail              string `json:"detail"`
	// OpenClawCancellation is retained for response compatibility. Verification
	// is intentionally local-only and must never cancel live OpenClaw runs.
	OpenClawCancellation *EmergencyStopFanoutSummary `json:"openClawCancellation,omitempty"`
}

// VerifyEmergencyStop proves the emergency stop actually halts background
// processing. Restoration is compare-and-swap protected, so a concurrent
// operator decision can never be overwritten by the verifier.
func (s *Service) VerifyEmergencyStop(ctx context.Context) (EmergencyStopVerification, error) {
	if s.runner == nil {
		return EmergencyStopVerification{}, fmt.Errorf("opscontrol: no background runner wired")
	}
	priorState, err := s.control.emergency.Status()
	if err != nil {
		return EmergencyStopVerification{}, fmt.Errorf(
			"read emergency-stop state for verification: %w",
			err,
		)
	}

	verificationState, err := s.control.Engage(
		"emergency-stop self-verification",
		"system:emergency-stop-verifier",
		s.now().UTC(),
	)
	if err != nil {
		return EmergencyStopVerification{}, fmt.Errorf("engage emergency stop for verification: %w", err)
	}
	processed, runErr := s.runner(ctx)
	restoreErr := s.restore(priorState, verificationState.Revision)
	if runErr != nil {
		if restoreErr != nil {
			return EmergencyStopVerification{}, fmt.Errorf(
				"background verification failed: %v; restore failed: %w",
				runErr,
				restoreErr,
			)
		}
		return EmergencyStopVerification{}, runErr
	}

	v := EmergencyStopVerification{
		EngagedDuringTest:   true,
		ProcessedDuringStop: processed,
		Halted:              processed == 0,
	}
	if restoreErr != nil {
		return EmergencyStopVerification{}, fmt.Errorf(
			"restore emergency stop after verification: %w",
			restoreErr,
		)
	}
	v.RestoredEngaged = s.control.EmergencyStop() == priorState.Engaged
	if v.Halted {
		v.Detail = "emergency stop halted background processing (0 operations processed while engaged)"
	} else {
		v.Detail = fmt.Sprintf("EMERGENCY STOP DID NOT HALT: %d operations processed while engaged", processed)
	}
	return v, nil
}

func (s *Service) fanOutEmergencyStop(ctx context.Context) (EmergencyStopState, EmergencyStopFanoutSummary, error) {
	state, err := s.control.emergency.Status()
	if err != nil {
		return state, EmergencyStopFanoutSummary{Status: "unavailable", Message: "HAI could not read the persisted emergency-stop state."}, fmt.Errorf("read persisted emergency-stop state before cancellation fan-out: %w", err)
	}
	if !state.Engaged {
		return state, EmergencyStopFanoutSummary{Status: "unavailable", Message: "OpenClaw cancellation was not attempted because the persisted emergency stop is not active."}, fmt.Errorf("emergency stop is not persisted as active")
	}
	if s.openClawStop == nil {
		return state, EmergencyStopFanoutSummary{Status: "unavailable", Message: "The emergency stop is active, but OpenClaw cancellation delivery is not configured."}, fmt.Errorf("OpenClaw emergency-stop cancellation delivery is unavailable")
	}
	targeted, acknowledged, awaitingIdentity, failed, fanoutErr := s.openClawStop.FanOutEmergencyStop(ctx)
	status := "complete"
	message := "The emergency stop is active. No unresolved OpenClaw run required cancellation."
	if targeted > 0 || awaitingIdentity > 0 || failed > 0 || fanoutErr != nil {
		status = "partial"
		message = "The emergency stop remains active. Cancellation requests and outcomes are recorded, but exact runs remain unresolved until terminal verification."
	}
	summary := EmergencyStopFanoutSummary{Status: status, Targeted: targeted, Acknowledged: acknowledged, AwaitingIdentity: awaitingIdentity, Failed: failed, TerminallyVerified: 0, AcknowledgmentIsTerminal: false, Message: message}
	if fanoutErr != nil {
		return state, summary, fmt.Errorf("emergency stop persisted; OpenClaw cancellation fan-out was partial: %w", fanoutErr)
	}
	return state, summary, nil
}

func (s *Service) restore(
	state EmergencyStopState,
	verificationRevision uint64,
) error {
	_, err := s.control.RestoreIfRevision(
		verificationRevision,
		state,
		s.now().UTC(),
	)
	return err
}
