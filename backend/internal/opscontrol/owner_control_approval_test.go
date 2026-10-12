package opscontrol

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
)

type allowExactControlConstitution struct{}

func (allowExactControlConstitution) EvaluateExecutionPolicy(
	_ context.Context,
	_ string,
	_ []string,
	_ int,
) (executionauth.ConstitutionDecision, error) {
	return executionauth.ConstitutionDecision{
		ID: "opscontrol-test-policy", Version: 1, Source: "test", Digest: strings.Repeat("c", 64), AuthorityCeiling: 10,
	}, nil
}

func TestOwnerControlApprovalBindsOwnerEffectAndFreshness(t *testing.T) {
	service, err := NewOwnerControlApprovalService([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("NewOwnerControlApprovalService: %v", err)
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	binding := strings.Repeat("a", 64)

	prepared, err := service.Prepare("owner", binding)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !strings.HasPrefix(prepared.SourceID, OwnerControlApprovalPrefix) ||
		prepared.BindingDigest != binding || prepared.ApprovedBy != "owner" ||
		prepared.ExpiresAt.Sub(prepared.ApprovedAt) != ownerControlApprovalTTL {
		t.Fatalf("prepared approval = %#v", prepared)
	}

	resolved, err := service.Resolve(nil, "owner", prepared.SourceID, binding)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.DecisionDigest != prepared.DecisionDigest || resolved.DecisionID != prepared.DecisionID {
		t.Fatalf("resolved approval changed: %#v", resolved)
	}

	for name, value := range map[string]struct {
		owner  string
		digest string
		source string
	}{
		"different owner":  {owner: "other", digest: binding, source: prepared.SourceID},
		"different effect": {owner: "owner", digest: strings.Repeat("b", 64), source: prepared.SourceID},
		"malformed source": {owner: "owner", digest: binding, source: OwnerControlApprovalPrefix + "bad"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Resolve(nil, value.owner, value.source, value.digest)
			if !errors.Is(err, ErrOwnerControlApprovalInvalid) {
				t.Fatalf("Resolve error = %v, want ErrOwnerControlApprovalInvalid", err)
			}
		})
	}

	now = now.Add(ownerControlApprovalTTL)
	if _, err := service.Resolve(nil, "owner", prepared.SourceID, binding); !errors.Is(err, ErrOwnerControlApprovalInvalid) {
		t.Fatalf("expired approval error = %v, want ErrOwnerControlApprovalInvalid", err)
	}
}

func TestPrepareEmergencyStopResumeRequiresOwnerExactStopAndIssuer(t *testing.T) {
	service := newTestService(t)
	if _, err := service.PrepareEmergencyStopResume(service.owner); !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("missing issuer error = %v, want ErrAuthorizationUnavailable", err)
	}
	if _, err := service.EngageEmergencyStop("test", service.owner); err != nil {
		t.Fatalf("EngageEmergencyStop: %v", err)
	}
	issuer, err := NewOwnerControlApprovalService([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("NewOwnerControlApprovalService: %v", err)
	}
	issuer.now = service.now
	service.WithOwnerControlApprovalIssuer(issuer)

	if _, err := service.PrepareEmergencyStopResume("other"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("non-owner preparation error = %v, want ErrAuthorizationDenied", err)
	}
	auth, err := service.PrepareEmergencyStopResume(service.owner)
	if err != nil {
		t.Fatalf("PrepareEmergencyStopResume: %v", err)
	}
	state := service.Control().EmergencyState()
	if auth.ActorIdentity != service.owner || auth.TaskID != "opscontrol-emergency-stop-"+strconv.FormatUint(state.Revision, 10) ||
		!strings.HasPrefix(auth.ApprovalSourceID, OwnerControlApprovalPrefix) || !isSHA256Digest(auth.ApprovalBindingDigest) {
		t.Fatalf("prepared control authorization = %#v", auth)
	}

	// Exercise the same direct owner approval and live persisted-stop evidence
	// used by the production composition. This prevents a shape-only test from
	// masking a resume path that cannot actually consume its approval.
	authorization, err := executionauth.NewService(
		executionauth.NewMemoryRepository(),
		allowExactControlConstitution{},
		nil,
		nil,
		issuer,
		service.now,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	authorization.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		current := service.Control().EmergencyState()
		return executionauth.EmergencyStopEvidence{
			Active: current.Engaged, Source: "persisted_control", Reason: current.Reason,
		}
	})
	service.WithExecutionAuthorizer(authorization)
	if _, err := service.DisengageEmergencyStop(context.Background(), auth); err != nil {
		t.Fatalf("DisengageEmergencyStop with owner approval: %v", err)
	}
	if service.Control().EmergencyStop() {
		t.Fatal("consumed owner approval must clear the exact stop revision")
	}
}
