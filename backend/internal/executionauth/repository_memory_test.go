package executionauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryRepositoryRejectsExpiredApprovalAtConsumption(t *testing.T) {
	repository := NewMemoryRepository()
	request := baseRequest("expired-approval-consumption")
	request.ApprovalSourceID = "approval:expired-at-claim"
	request.ApprovalBindingDigest = request.EffectDigest
	approval := ResolvedApproval{
		SourceID:       request.ApprovalSourceID,
		DecisionID:     "decision-expired-at-claim",
		DecisionDigest: strings.Repeat("a", 64),
		BindingDigest:  request.ApprovalBindingDigest,
		ApprovedBy:     request.OwnerIdentity,
		ApprovedAt:     fixedNow().Add(-time.Minute),
		ExpiresAt:      fixedNow().Add(time.Minute),
	}
	service := newTestService(t, repository, permissiveConstitution(), fakeApprovalResolver{
		values: map[string]ResolvedApproval{
			request.OwnerIdentity + "\x00" + request.ApprovalSourceID: approval,
		},
	}, nil)
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("receipt outcome = %s, want authorized", receipt.Outcome)
	}

	consumption := Consumption{
		ReceiptID:       receipt.ID,
		OwnerIdentity:   receipt.OwnerIdentity,
		Consumer:        "test-consumer",
		ExecutionTarget: "test-target",
		ReceiptDigest:   receipt.DecisionDigest,
		ConsumedAt:      approval.ExpiresAt,
	}
	if err := repository.Consume(context.Background(), consumption); !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("Consume error = %v, want ErrAuthorizationChanged", err)
	}
	if _, err := repository.GetConsumption(context.Background(), receipt.OwnerIdentity, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired receipt has consumption record: err=%v", err)
	}
}

func TestMemoryRepositoryReceiptGovernanceIsImmutableByCopy(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request := selectorV5Request("immutable-governance-copy")
	withMatchingFrameworkSelection(t, service, *request.Governance)

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("receipt = %#v", receipt)
	}
	receipt.Evidence.Governance.EvidenceReferences = append(
		receipt.Evidence.Governance.EvidenceReferences,
		"mutated://reference",
	)
	*receipt.Evidence.Governance.FrameworkMaximumAutonomyLevel = 0
	*receipt.Evidence.Governance.FrameworkRequiresApproval = true
	receipt.Evidence.ReasonCodes = append(receipt.Evidence.ReasonCodes, "mutated")

	stored, err := service.Get(context.Background(), "alice", receipt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if *stored.Evidence.Governance.FrameworkMaximumAutonomyLevel != request.RequestedAutonomy ||
		*stored.Evidence.Governance.FrameworkRequiresApproval ||
		containsFold(stored.Evidence.Governance.EvidenceReferences, "mutated://reference") ||
		containsFold(stored.Evidence.ReasonCodes, "mutated") {
		t.Fatalf("stored receipt was mutated through returned evidence: %#v", stored.Evidence)
	}
	if stored.DecisionDigest == "" || !strings.EqualFold(stored.DecisionDigest, receipt.DecisionDigest) {
		t.Fatalf("stored decision digest changed: stored=%q returned=%q", stored.DecisionDigest, receipt.DecisionDigest)
	}
}
