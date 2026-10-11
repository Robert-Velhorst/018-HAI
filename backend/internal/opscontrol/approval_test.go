package opscontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"

	"github.com/google/uuid"
)

type controlConstitutionStub struct{}

func (controlConstitutionStub) EvaluateExecutionPolicy(
	_ string,
	_ []string,
	_ int,
) (executionauth.ConstitutionDecision, error) {
	return executionauth.ConstitutionDecision{
		ID:               "builtin-control-test",
		Version:          1,
		Source:           "builtin-control-test",
		Digest:           strings.Repeat("a", 64),
		AuthorityCeiling: 10,
	}, nil
}

func TestControlApprovalCompletesExactEmergencyStopRecovery(t *testing.T) {
	const actor = "idp-owner-subject"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryControlApprovalRepository()
	service := newTestService(t)
	service.now = func() time.Time { return now }
	service.WithControlApprovalRepository(repository)

	resolver, err := NewControlApprovalResolver(repository)
	if err != nil {
		t.Fatalf("new control approval resolver: %v", err)
	}
	resolver.now = service.now
	authorization, err := executionauth.NewService(
		executionauth.NewMemoryRepository(),
		controlConstitutionStub{},
		nil,
		nil,
		resolver,
		service.now,
	)
	if err != nil {
		t.Fatalf("new execution authorization: %v", err)
	}
	controlAuthorization := authorization.CloneWithEmergencyStopEvaluator(
		func() executionauth.EmergencyStopEvidence {
			return executionauth.EmergencyStopEvidence{Source: "environment"}
		},
	)
	service.WithExecutionAuthorizer(controlAuthorization)

	if _, err := service.EngageEmergencyStop("operator test", actor); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	prepared, err := service.PrepareControlApproval(
		context.Background(), actor, controlApprovalResume, "",
	)
	if err != nil {
		t.Fatalf("prepare control approval: %v", err)
	}
	if prepared.Action != clearEmergencyStopAction ||
		prepared.ResourceID != emergencyStopResourceID(service.Control().EmergencyState().Revision) ||
		len(prepared.BindingDigest) != 64 {
		t.Fatalf("prepared approval is not exact: %#v", prepared)
	}
	decided, err := service.DecideControlApproval(
		context.Background(), actor, prepared.RequestID,
		controlDecisionApprove, "Owner reviewed exact recovery state.",
	)
	if err != nil {
		t.Fatalf("decide control approval: %v", err)
	}
	if decided.ApprovalSourceID != ControlDecisionPrefix+decided.DecisionID.String() ||
		decided.ApprovalBindingDigest != prepared.BindingDigest {
		t.Fatalf("decision references are not exact: %#v", decided)
	}

	state, err := service.DisengageEmergencyStop(
		context.Background(),
		ControlAuthorization{
			ActorIdentity:         actor,
			IdempotencyKey:        decided.IdempotencyKey,
			TaskID:                decided.TaskID,
			ApprovalSourceID:      decided.ApprovalSourceID,
			ApprovalBindingDigest: decided.ApprovalBindingDigest,
		},
	)
	if err != nil {
		t.Fatalf("execute approved recovery: %v", err)
	}
	if state.Engaged || service.Control().EmergencyStop() {
		t.Fatalf("approved exact recovery did not clear stop: %#v", state)
	}
	if _, err := service.DisengageEmergencyStop(
		context.Background(),
		ControlAuthorization{
			ActorIdentity:         actor,
			IdempotencyKey:        decided.IdempotencyKey,
			TaskID:                decided.TaskID,
			ApprovalSourceID:      decided.ApprovalSourceID,
			ApprovalBindingDigest: decided.ApprovalBindingDigest,
		},
	); !errors.Is(err, ErrControlChangeNotRequired) {
		t.Fatalf("replayed recovery error = %v, want ErrControlChangeNotRequired", err)
	}
}

func TestControlApprovalRejectsCrossOwnerDecisionAndStaleRevision(t *testing.T) {
	service := newTestService(t)
	service.WithControlApprovalRepository(NewMemoryControlApprovalRepository())
	if _, err := service.EngageEmergencyStop("original", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	prepared, err := service.PrepareControlApproval(
		context.Background(), service.owner, controlApprovalResume, "",
	)
	if err != nil {
		t.Fatalf("prepare approval: %v", err)
	}
	if _, err := service.DecideControlApproval(
		context.Background(), "other-owner", prepared.RequestID,
		controlDecisionApprove, "",
	); !errors.Is(err, ErrControlApprovalNotFound) {
		t.Fatalf("cross-owner decision error = %v, want ErrControlApprovalNotFound", err)
	}
	if _, err := service.EngageEmergencyStop("new stop", service.owner); err != nil {
		t.Fatalf("replace emergency stop: %v", err)
	}
	_, err = service.DecideControlApproval(
		context.Background(), service.owner, prepared.RequestID,
		controlDecisionApprove, "",
	)
	if !errors.Is(err, ErrControlApprovalStale) {
		t.Fatalf("stale decision error = %v, want ErrControlApprovalStale", err)
	}
}

func TestControlApprovalDecisionIsAppendOnly(t *testing.T) {
	service := newTestService(t)
	service.WithControlApprovalRepository(NewMemoryControlApprovalRepository())
	if _, err := service.EngageEmergencyStop("owner review", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	prepared, err := service.PrepareControlApproval(
		context.Background(), service.owner, controlApprovalResume, "",
	)
	if err != nil {
		t.Fatalf("prepare approval: %v", err)
	}
	rejected, err := service.DecideControlApproval(
		context.Background(), service.owner, prepared.RequestID,
		controlDecisionReject, "Keep the stop engaged.",
	)
	if err != nil {
		t.Fatalf("reject approval: %v", err)
	}
	if rejected.Decision != controlDecisionReject ||
		rejected.ApprovalSourceID != "" ||
		rejected.ApprovalBindingDigest != "" {
		t.Fatalf("rejected decision exposed execution authority: %#v", rejected)
	}
	_, err = service.DecideControlApproval(
		context.Background(), service.owner, prepared.RequestID,
		controlDecisionApprove, "",
	)
	if !errors.Is(err, ErrControlApprovalDecided) {
		t.Fatalf("second decision error = %v, want ErrControlApprovalDecided", err)
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("rejected approval must leave emergency stop engaged")
	}
}

func TestControlApprovalResolverRejectsRejectedExpiredAndMismatchedDecisions(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryControlApprovalRepository()
	request := ControlApprovalRequest{
		ID:             uuid.New(),
		OwnerIdentity:  "owner",
		IdempotencyKey: "opscontrol:test",
		TaskID:         "opscontrol:test",
		Action:         clearEmergencyStopAction,
		ResourceType:   emergencyStopResourceType,
		ResourceID:     emergencyStopResourceID(1),
		Target:         "disengaged",
		BindingDigest:  strings.Repeat("b", 64),
		CreatedBy:      "owner",
		CreatedAt:      now,
		ExpiresAt:      now.Add(controlAuthorizationTTL),
	}
	if err := repository.CreateRequest(context.Background(), request); err != nil {
		t.Fatalf("create request: %v", err)
	}
	decision := ControlApprovalDecision{
		ID:            uuid.New(),
		RequestID:     request.ID,
		OwnerIdentity: request.OwnerIdentity,
		Decision:      controlDecisionReject,
		Actor:         request.OwnerIdentity,
		CreatedAt:     now.Add(time.Minute),
		Request:       request,
	}
	if err := repository.CreateDecision(context.Background(), decision); err != nil {
		t.Fatalf("create decision: %v", err)
	}
	resolver, err := NewControlApprovalResolver(repository)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	resolver.now = func() time.Time { return now.Add(2 * time.Minute) }
	sourceID := ControlDecisionPrefix + decision.ID.String()
	if _, err := resolver.Resolve(
		context.Background(), "owner", sourceID, request.BindingDigest,
	); !errors.Is(err, ErrControlDecisionUnavailable) {
		t.Fatalf("rejected decision error = %v", err)
	}

	approvedRepository := NewMemoryControlApprovalRepository()
	approvedRequest := request
	approvedRequest.ID = uuid.New()
	approvedRequest.IdempotencyKey = "opscontrol:approved"
	approvedRequest.TaskID = "opscontrol:approved"
	if err := approvedRepository.CreateRequest(context.Background(), approvedRequest); err != nil {
		t.Fatalf("create approved request: %v", err)
	}
	approved := decision
	approved.ID = uuid.New()
	approved.RequestID = approvedRequest.ID
	approved.Decision = controlDecisionApprove
	approved.Request = approvedRequest
	if err := approvedRepository.CreateDecision(context.Background(), approved); err != nil {
		t.Fatalf("create approved decision: %v", err)
	}
	approvedResolver, _ := NewControlApprovalResolver(approvedRepository)
	approvedResolver.now = func() time.Time { return now.Add(2 * time.Minute) }
	approvedSource := ControlDecisionPrefix + approved.ID.String()
	if _, err := approvedResolver.Resolve(
		context.Background(), "owner", approvedSource, strings.Repeat("c", 64),
	); !errors.Is(err, ErrControlDecisionBindingMismatch) {
		t.Fatalf("binding mismatch error = %v", err)
	}
	approvedResolver.now = func() time.Time { return approvedRequest.ExpiresAt }
	if _, err := approvedResolver.Resolve(
		context.Background(), "owner", approvedSource, approvedRequest.BindingDigest,
	); !errors.Is(err, ErrControlApprovalExpired) {
		t.Fatalf("expiry error = %v", err)
	}
}

func TestImmutableEnvironmentStopCannotBeClearedByOwnerApproval(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryControlApprovalRepository()
	service := newTestService(t)
	service.now = func() time.Time { return now }
	service.WithControlApprovalRepository(repository)
	resolver, _ := NewControlApprovalResolver(repository)
	resolver.now = service.now
	authorization, err := executionauth.NewService(
		executionauth.NewMemoryRepository(), controlConstitutionStub{},
		nil, nil, resolver, service.now,
	)
	if err != nil {
		t.Fatalf("new authorization: %v", err)
	}
	service.WithExecutionAuthorizer(
		authorization.CloneWithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
			return executionauth.EmergencyStopEvidence{
				Active: true, Source: "environment", Reason: "deployment hard stop",
			}
		}),
	)
	if _, err := service.EngageEmergencyStop("operator stop", service.owner); err != nil {
		t.Fatalf("engage stop: %v", err)
	}
	prepared, err := service.PrepareControlApproval(
		context.Background(), service.owner, controlApprovalResume, "",
	)
	if err != nil {
		t.Fatalf("prepare approval: %v", err)
	}
	decided, err := service.DecideControlApproval(
		context.Background(), service.owner, prepared.RequestID,
		controlDecisionApprove, "",
	)
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	_, err = service.DisengageEmergencyStop(context.Background(), ControlAuthorization{
		ActorIdentity:         service.owner,
		IdempotencyKey:        decided.IdempotencyKey,
		TaskID:                decided.TaskID,
		ApprovalSourceID:      decided.ApprovalSourceID,
		ApprovalBindingDigest: decided.ApprovalBindingDigest,
	})
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("immutable stop error = %v, want authorization denied", err)
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("immutable environment stop must leave persisted stop engaged")
	}
}
