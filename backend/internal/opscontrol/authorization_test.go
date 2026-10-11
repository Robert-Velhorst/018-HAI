package opscontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

type controlAuthorizerFunc func(
	context.Context,
	executionauth.Request,
	string,
	string,
) (executionauth.Receipt, error)

func (f controlAuthorizerFunc) AuthorizeAndConsume(
	ctx context.Context,
	request executionauth.Request,
	consumer string,
	target string,
) (executionauth.Receipt, error) {
	return f(ctx, request, consumer, target)
}

func allowExactControlAuthorization(now func() time.Time) ExecutionAuthorizer {
	return controlAuthorizerFunc(func(
		_ context.Context,
		request executionauth.Request,
		_ string,
		_ string,
	) (executionauth.Receipt, error) {
		return exactControlReceipt(request, now()), nil
	})
}

func exactControlReceipt(request executionauth.Request, now time.Time) executionauth.Receipt {
	now = now.UTC()
	return executionauth.Receipt{
		ID:                uuid.New(),
		ContractVersion:   executionauth.ContractVersion,
		OwnerIdentity:     request.OwnerIdentity,
		IdempotencyKey:    request.IdempotencyKey,
		ActorIdentity:     request.ActorIdentity,
		ActorKind:         request.ActorKind,
		TaskID:            request.TaskID,
		Action:            request.Action,
		Stage:             request.Stage,
		ResourceType:      request.ResourceType,
		ResourceID:        request.ResourceID,
		ApprovalSourceID:  request.ApprovalSourceID,
		EffectDigest:      request.EffectDigest,
		Outcome:           executionauth.OutcomeAuthorized,
		RequestDigest:     "request-digest-1",
		DecisionDigest:    "authorization-decision-digest-1",
		RequiredAuthority: request.RequiredAuthority,
		RequestedAutonomy: request.RequestedAutonomy,
		Risk:              request.Risk,
		Reversible:        request.Reversible,
		EvaluatedAt:       now,
		Evidence: executionauth.DecisionEvidence{
			Approval: executionauth.ApprovalEvidence{
				SourceID:       request.ApprovalSourceID,
				DecisionID:     "decision-1",
				DecisionDigest: "decision-digest-1",
				ApprovedBy:     request.ActorIdentity,
				ApprovedAt:     now.Add(-time.Minute),
				ExpiresAt:      now.Add(time.Minute),
			},
		},
	}
}

func controlAuthorizationFor(
	t *testing.T,
	service *Service,
	actor string,
	action string,
	resourceType string,
	resourceID string,
	target string,
) ControlAuthorization {
	t.Helper()
	digest, err := controlEffectDigest(controlEffect{
		Version:       1,
		OwnerIdentity: actor,
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Target:        target,
	})
	if err != nil {
		t.Fatalf("derive control effect: %v", err)
	}
	return ControlAuthorization{
		ActorIdentity:         actor,
		IdempotencyKey:        "control-idempotency-1",
		TaskID:                "control-task-1",
		ApprovalSourceID:      "approval-1",
		ApprovalBindingDigest: digest,
	}
}

func TestDisengageEmergencyStopFailsClosedWithoutAuthorizer(t *testing.T) {
	service := newTestService(t)
	if _, err := service.EngageEmergencyStop("test", "operator"); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	state := service.Control().EmergencyState()
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)

	_, err := service.DisengageEmergencyStop(context.Background(), auth)
	if !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("disengage error = %v, want ErrAuthorizationUnavailable", err)
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("missing authorizer must leave emergency stop engaged")
	}
}

func TestPermissiveModeChangeFailsClosedWithoutAuthorizer(t *testing.T) {
	service := newTestService(t)
	if _, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModePaused),
		ControlAuthorization{},
	); err != nil {
		t.Fatalf("restrict mode: %v", err)
	}

	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		escalateAutonomyAction,
		autonomyModeResourceType,
		autonomyModeResourceID(
			string(autonomypolicy.ModePaused),
			string(autonomypolicy.ModeReadOnly),
		),
		string(autonomypolicy.ModeReadOnly),
	)
	_, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModeReadOnly),
		auth,
	)
	if !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("permissive mode error = %v, want ErrAuthorizationUnavailable", err)
	}
	if got := service.Control().StoredMode(); got != autonomypolicy.ModePaused {
		t.Fatalf("stored mode = %s, want paused", got)
	}
}

func TestDisengageAuthorizationBindsExactControlEffect(t *testing.T) {
	service := newTestService(t)
	if _, err := service.EngageEmergencyStop("test", "operator"); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	state := service.Control().EmergencyState()
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)

	calls := 0
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
		_ context.Context,
		request executionauth.Request,
		consumer string,
		target string,
	) (executionauth.Receipt, error) {
		calls++
		if request.OwnerIdentity != "operator" ||
			request.Action != clearEmergencyStopAction ||
			request.ResourceType != emergencyStopResourceType ||
			request.ResourceID != emergencyStopResourceID(state.Revision) ||
			request.EffectDigest != auth.ApprovalBindingDigest {
			t.Fatalf("authorization request is not exactly bound: %#v", request)
		}
		if consumer != "opscontrol" {
			t.Fatalf("consumer = %q, want opscontrol", consumer)
		}
		wantTarget := clearEmergencyStopAction + ":" + emergencyStopResourceID(state.Revision)
		if target != wantTarget {
			t.Fatalf("execution target = %q, want %q", target, wantTarget)
		}
		return exactControlReceipt(request, service.now()), nil
	}))

	if _, err := service.DisengageEmergencyStop(context.Background(), auth); err != nil {
		t.Fatalf("disengage emergency stop: %v", err)
	}
	if calls != 1 {
		t.Fatalf("authorization calls = %d, want 1", calls)
	}
	if service.Control().EmergencyStop() {
		t.Fatal("valid exact authorization must clear the emergency stop")
	}
}

func TestDisengageRejectsMismatchedAuthorizationReceipts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*executionauth.Receipt)
	}{
		{"receipt id", func(receipt *executionauth.Receipt) { receipt.ID = uuid.Nil }},
		{"contract", func(receipt *executionauth.Receipt) { receipt.ContractVersion++ }},
		{"owner", func(receipt *executionauth.Receipt) { receipt.OwnerIdentity = "other-owner" }},
		{"idempotency", func(receipt *executionauth.Receipt) { receipt.IdempotencyKey = "other-key" }},
		{"actor", func(receipt *executionauth.Receipt) { receipt.ActorIdentity = "other-actor" }},
		{"actor kind", func(receipt *executionauth.Receipt) { receipt.ActorKind = executionauth.ActorAgent }},
		{"task", func(receipt *executionauth.Receipt) { receipt.TaskID = "other-task" }},
		{"action", func(receipt *executionauth.Receipt) { receipt.Action = escalateAutonomyAction }},
		{"resource type", func(receipt *executionauth.Receipt) { receipt.ResourceType = autonomyModeResourceType }},
		{"resource id", func(receipt *executionauth.Receipt) { receipt.ResourceID = "other-resource" }},
		{"effect", func(receipt *executionauth.Receipt) { receipt.EffectDigest = "other-effect" }},
		{"request digest", func(receipt *executionauth.Receipt) { receipt.RequestDigest = "" }},
		{"decision digest", func(receipt *executionauth.Receipt) { receipt.DecisionDigest = "" }},
		{"authority", func(receipt *executionauth.Receipt) { receipt.RequiredAuthority-- }},
		{"autonomy", func(receipt *executionauth.Receipt) { receipt.RequestedAutonomy-- }},
		{"risk", func(receipt *executionauth.Receipt) { receipt.Risk = executionauth.RiskLow }},
		{"reversibility", func(receipt *executionauth.Receipt) { receipt.Reversible = true }},
		{"approval source", func(receipt *executionauth.Receipt) { receipt.ApprovalSourceID = "other-approval" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t)
			if _, err := service.EngageEmergencyStop("test", "operator"); err != nil {
				t.Fatalf("engage emergency stop: %v", err)
			}
			state := service.Control().EmergencyState()
			auth := controlAuthorizationFor(
				t,
				service,
				"operator",
				clearEmergencyStopAction,
				emergencyStopResourceType,
				emergencyStopResourceID(state.Revision),
				"disengaged",
			)
			service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
				_ context.Context,
				request executionauth.Request,
				_ string,
				_ string,
			) (executionauth.Receipt, error) {
				receipt := exactControlReceipt(request, service.now())
				test.mutate(&receipt)
				return receipt, nil
			}))

			_, err := service.DisengageEmergencyStop(context.Background(), auth)
			if !errors.Is(err, ErrAuthorizationMismatch) {
				t.Fatalf("disengage error = %v, want ErrAuthorizationMismatch", err)
			}
			if !service.Control().EmergencyStop() {
				t.Fatal("mismatched receipt must leave emergency stop engaged")
			}
		})
	}
}

func TestDisengageDoesNotClearConcurrentEmergencyStop(t *testing.T) {
	service := newTestService(t)
	if _, err := service.EngageEmergencyStop("original", "operator"); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	state := service.Control().EmergencyState()
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
		_ context.Context,
		request executionauth.Request,
		_ string,
		_ string,
	) (executionauth.Receipt, error) {
		if _, err := service.EngageEmergencyStop("concurrent stop", "other-operator"); err != nil {
			t.Fatalf("engage concurrent emergency stop: %v", err)
		}
		return exactControlReceipt(request, service.now()), nil
	}))

	_, err := service.DisengageEmergencyStop(context.Background(), auth)
	if !errors.Is(err, ErrEmergencyStopStateChanged) {
		t.Fatalf("disengage error = %v, want ErrEmergencyStopStateChanged", err)
	}
	current := service.Control().EmergencyState()
	if !current.Engaged || current.Reason != "concurrent stop" ||
		current.Actor != "other-operator" || current.Revision <= state.Revision {
		t.Fatalf("concurrent stop was not preserved: %#v", current)
	}
}

func TestModeEscalationDoesNotOverwriteConcurrentModeChange(t *testing.T) {
	service := newTestService(t)
	if _, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModeReadOnly),
		ControlAuthorization{},
	); err != nil {
		t.Fatalf("set restrictive mode: %v", err)
	}
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		escalateAutonomyAction,
		autonomyModeResourceType,
		autonomyModeResourceID(
			string(autonomypolicy.ModeReadOnly),
			string(autonomypolicy.ModeAutonomousSafe),
		),
		string(autonomypolicy.ModeAutonomousSafe),
	)
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
		_ context.Context,
		request executionauth.Request,
		_ string,
		_ string,
	) (executionauth.Receipt, error) {
		if _, err := service.control.SetMode(autonomypolicy.ModeDraftOnly); err != nil {
			t.Fatalf("concurrent mode change: %v", err)
		}
		return exactControlReceipt(request, service.now()), nil
	}))

	_, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModeAutonomousSafe),
		auth,
	)
	if !errors.Is(err, ErrAutonomyModeStateChanged) {
		t.Fatalf("mode escalation error = %v, want ErrAutonomyModeStateChanged", err)
	}
	if got := service.Control().StoredMode(); got != autonomypolicy.ModeDraftOnly {
		t.Fatalf("stored mode = %s, want concurrent draft_only mode", got)
	}
}

func TestEmergencyStopPersistenceFailureStaysFailClosed(t *testing.T) {
	stateDir := t.TempDir()
	service := newTestServiceAtStateDir(t, stateDir)
	if _, err := service.EngageEmergencyStop("test", "operator"); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	state := service.Control().EmergencyState()
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)
	service.WithExecutionAuthorizer(allowExactControlAuthorization(service.now))

	statePath := filepath.Join(stateDir, "emergency_stop.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatalf("remove emergency-stop state file: %v", err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatalf("replace emergency-stop state with directory: %v", err)
	}

	_, err := service.DisengageEmergencyStop(context.Background(), auth)
	if !errors.Is(err, ErrControlPersistence) {
		t.Fatalf("disengage error = %v, want ErrControlPersistence", err)
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("persistence failure must keep emergency stop engaged")
	}
}

func TestModePersistenceFailureDoesNotChangeInMemoryMode(t *testing.T) {
	stateDir := t.TempDir()
	service := newTestServiceAtStateDir(t, stateDir)
	if _, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModeReadOnly),
		ControlAuthorization{},
	); err != nil {
		t.Fatalf("set initial restrictive mode: %v", err)
	}
	modePath := filepath.Join(stateDir, "background_mode.json")
	if err := os.Remove(modePath); err != nil {
		t.Fatalf("remove mode state file: %v", err)
	}
	if err := os.Mkdir(modePath, 0o700); err != nil {
		t.Fatalf("replace mode state with directory: %v", err)
	}

	_, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModePaused),
		ControlAuthorization{},
	)
	if !errors.Is(err, ErrControlPersistence) {
		t.Fatalf("mode error = %v, want ErrControlPersistence", err)
	}
	if got := service.Control().StoredMode(); got != autonomypolicy.ModeReadOnly {
		t.Fatalf("stored mode = %s, want unchanged read_only", got)
	}
}

func TestCorruptPersistedModeFailsClosedAndIsVisible(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(stateDir, "background_mode.json"),
		[]byte("{not-json"),
		0o600,
	); err != nil {
		t.Fatalf("write corrupt mode state: %v", err)
	}
	service := newTestServiceAtStateDir(t, stateDir)

	if got := service.Control().Mode(); got != autonomypolicy.ModePaused {
		t.Fatalf("effective mode = %s, want fail-closed paused", got)
	}
	status := service.Status(context.Background())
	if status.Mode != string(autonomypolicy.ModePaused) ||
		status.StoredMode != string(autonomypolicy.ModePaused) ||
		status.ModeError == "" ||
		status.Processing {
		t.Fatalf("status did not expose fail-closed mode state: %#v", status)
	}
	readiness := service.Readiness(context.Background())
	for _, gate := range readiness.Gates {
		if gate.Name == "autonomy_mode_persistence" {
			if gate.Status != GateFail {
				t.Fatalf("mode persistence gate = %#v, want failure", gate)
			}
			if readiness.OverallReady {
				t.Fatal("corrupt persisted mode must make readiness fail")
			}
			return
		}
	}
	t.Fatal("readiness omitted autonomy_mode_persistence gate")
}

func TestRestrictiveSetModeRepairsCorruptPersistedMode(t *testing.T) {
	stateDir := t.TempDir()
	modePath := filepath.Join(stateDir, "background_mode.json")
	if err := os.WriteFile(modePath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt mode state: %v", err)
	}
	service := newTestServiceAtStateDir(t, stateDir)

	mode, err := service.SetMode(
		context.Background(),
		string(autonomypolicy.ModePaused),
		ControlAuthorization{},
	)
	if err != nil {
		t.Fatalf("repair persisted mode: %v", err)
	}
	if mode != string(autonomypolicy.ModePaused) {
		t.Fatalf("repaired mode = %q, want paused", mode)
	}
	if _, modeErr := service.Control().ModePersistenceStatus(); modeErr != nil {
		t.Fatalf("mode persistence remains unhealthy: %v", modeErr)
	}

	restarted := newTestServiceAtStateDir(t, stateDir)
	if got := restarted.Control().StoredMode(); got != autonomypolicy.ModePaused {
		t.Fatalf("restarted mode = %s, want persisted paused", got)
	}
}

func TestVerifyEmergencyStopDoesNotOverwriteConcurrentOperatorStop(t *testing.T) {
	service := newTestService(t)
	service.SetBackgroundRunner(func(context.Context) (int, error) {
		if _, err := service.EngageEmergencyStop(
			"operator stop during verification",
			"operator",
		); err != nil {
			return 0, err
		}
		return 0, nil
	})

	_, err := service.VerifyEmergencyStop(context.Background())
	if !errors.Is(err, ErrEmergencyStopStateChanged) {
		t.Fatalf("verification error = %v, want ErrEmergencyStopStateChanged", err)
	}
	current := service.Control().EmergencyState()
	if !current.Engaged || current.Reason != "operator stop during verification" ||
		current.Actor != "operator" {
		t.Fatalf("verification overwrote concurrent stop: %#v", current)
	}
}

func TestSafetyWeakeningUsesAuthenticatedActorWhenConfiguredOwnerIsEmpty(t *testing.T) {
	service := newTestServiceAtStateDirAndOwner(t, t.TempDir(), "")
	if _, err := service.EngageEmergencyStop("test", "operator"); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	state := service.Control().EmergencyState()
	auth := controlAuthorizationFor(
		t,
		service,
		"operator",
		clearEmergencyStopAction,
		emergencyStopResourceType,
		emergencyStopResourceID(state.Revision),
		"disengaged",
	)
	calls := 0
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
		_ context.Context,
		request executionauth.Request,
		_ string,
		_ string,
	) (executionauth.Receipt, error) {
		calls++
		if request.OwnerIdentity != "operator" || request.ActorIdentity != "operator" {
			t.Fatalf("authorization identity = %q/%q, want operator/operator", request.OwnerIdentity, request.ActorIdentity)
		}
		return exactControlReceipt(request, service.now()), nil
	}))

	_, err := service.DisengageEmergencyStop(context.Background(), auth)
	if err != nil {
		t.Fatalf("authenticated actor recovery failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("authorizer calls = %d, want 1", calls)
	}
	if service.Control().EmergencyStop() {
		t.Fatal("approved recovery must clear emergency stop")
	}
}

func newTestServiceAtStateDir(t *testing.T, stateDir string) *Service {
	t.Helper()
	return newTestServiceAtStateDirAndOwner(t, stateDir, "local-operator")
}

func newTestServiceAtStateDirAndOwner(
	t *testing.T,
	stateDir string,
	owner string,
) *Service {
	t.Helper()
	broker := newAuthorizedOpsControlTestBroker(t, t.TempDir(), "local-operator", "local")
	return NewService(
		stateDir,
		broker,
		operations.NewService(operations.NewMemoryRepository()),
		owner,
		"local",
	)
}
