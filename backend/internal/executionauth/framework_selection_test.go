package executionauth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeFrameworkSelectionResolver struct {
	mu     sync.Mutex
	values map[string]FrameworkSelectionSnapshot
	err    error
	calls  int
	onCall func(int, FrameworkSelectionSnapshot) (FrameworkSelectionSnapshot, error)
}

func (f *fakeFrameworkSelectionResolver) ResolveFrameworkSelection(
	_ context.Context,
	owner string,
	selectionID string,
) (FrameworkSelectionSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	value, ok := f.values[owner+"\x00"+selectionID]
	if !ok {
		return FrameworkSelectionSnapshot{}, ErrNotFound
	}
	if f.onCall != nil {
		return f.onCall(f.calls, value)
	}
	return value, f.err
}

func snapshotFromGovernance(value GovernanceEvidence) FrameworkSelectionSnapshot {
	result := FrameworkSelectionSnapshot{
		SelectionID:              value.FrameworkSelectionID,
		TaskPlanID:               value.TaskPlanID,
		CatalogVersion:           value.FrameworkCatalogVersion,
		SelectorAlgorithmVersion: value.FrameworkSelectorAlgorithmVersion,
		TaskRiskLevel:            value.FrameworkTaskRiskLevel,
		EffectiveRiskCeiling:     value.FrameworkEffectiveRiskCeiling,
		CatalogDigest:            value.FrameworkCatalogDigest,
		PreferenceDigest:         value.FrameworkPreferenceDigest,
		ConstitutionDigest:       value.FrameworkConstitutionDigest,
		OperatingContractDigest:  value.FrameworkOperatingContractDigest,
	}
	if value.FrameworkMaximumAutonomyLevel != nil {
		result.MaximumAutonomyLevel = *value.FrameworkMaximumAutonomyLevel
	}
	if value.FrameworkRequiresApproval != nil {
		result.RequiresApproval = *value.FrameworkRequiresApproval
	}
	return result
}

func withMatchingFrameworkSelection(
	t *testing.T,
	service *Service,
	governance GovernanceEvidence,
) *fakeFrameworkSelectionResolver {
	t.Helper()
	resolver := &fakeFrameworkSelectionResolver{values: map[string]FrameworkSelectionSnapshot{
		"alice\x00" + governance.FrameworkSelectionID: snapshotFromGovernance(governance),
	}}
	if _, err := service.WithFrameworkSelectionResolver(resolver); err != nil {
		t.Fatalf("WithFrameworkSelectionResolver: %v", err)
	}
	withMatchingFrameworkEvidencePreflight(t, service, governance)
	return resolver
}

func selectorV5Request(key string) Request {
	request := baseRequest(key)
	maximumAutonomy := request.RequestedAutonomy
	requiresApproval := false
	digest := strings.Repeat("a", 64)
	request.Governance = &GovernanceEvidence{
		TaskPlanID:                        "plan-1",
		TaskPlanDigest:                    digest,
		FrameworkEvidencePreflightDigest:  strings.Repeat("f", 64),
		FrameworkSelectionID:              "selection-1",
		FrameworkCatalogVersion:           "framework-catalog-v2",
		FrameworkSelectorAlgorithmVersion: frameworkSelectorV5,
		FrameworkTaskRiskLevel:            RiskLow,
		FrameworkEffectiveRiskCeiling:     RiskHigh,
		FrameworkMaximumAutonomyLevel:     &maximumAutonomy,
		FrameworkRequiresApproval:         &requiresApproval,
		FrameworkCatalogDigest:            digest,
		FrameworkPreferenceDigest:         digest,
		FrameworkConstitutionDigest:       digest,
		FrameworkOperatingContractDigest:  digest,
	}
	return request
}

func TestSelectorV5AuthorizationFailsClosedWithoutSelectionResolver(t *testing.T) {
	constitution := permissiveConstitution()
	service := newTestService(t, NewMemoryRepository(), constitution, nil, nil)
	receipt, err := service.Authorize(context.Background(), selectorV5Request("no-selection-resolver"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied ||
		!containsFold(receipt.Evidence.ReasonCodes, "framework.selection_unverified") {
		t.Fatalf("receipt = %#v", receipt)
	}
	if constitution.calls != 0 {
		t.Fatalf("Constitution calls = %d, want framework denial first", constitution.calls)
	}
}

func TestSelectorV5AuthorizationRejectsForgedSelectionContract(t *testing.T) {
	constitution := permissiveConstitution()
	service := newTestService(t, NewMemoryRepository(), constitution, nil, nil)
	request := selectorV5Request("forged-selection-contract")
	resolved := snapshotFromGovernance(*request.Governance)
	resolved.MaximumAutonomyLevel = 6
	resolved.RequiresApproval = true
	resolver := &fakeFrameworkSelectionResolver{values: map[string]FrameworkSelectionSnapshot{
		"alice\x00selection-1": resolved,
	}}
	if _, err := service.WithFrameworkSelectionResolver(resolver); err != nil {
		t.Fatalf("WithFrameworkSelectionResolver: %v", err)
	}

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied || receipt.Evidence.FrameworkSelection.Verified {
		t.Fatalf("forged selection receipt = %#v", receipt)
	}
	if constitution.calls != 0 {
		t.Fatal("forged framework contract reached Constitution policy")
	}
}

func TestSelectorV5AuthorizationUsesOwnerScopedImmutableSelection(t *testing.T) {
	service := newTestService(t, NewMemoryRepository(), permissiveConstitution(), nil, nil)
	request := selectorV5Request("verified-selection-contract")
	resolver := withMatchingFrameworkSelection(t, service, *request.Governance)

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized ||
		!receipt.Evidence.FrameworkSelection.Verified ||
		!receipt.Evidence.FrameworkSelection.OwnerScoped ||
		receipt.Evidence.FrameworkSelection.SelectionID != "selection-1" {
		t.Fatalf("verified selection receipt = %#v", receipt)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
}

func TestSelectorV5SelectionIsRecheckedBeforeConsumption(t *testing.T) {
	service := newTestService(t, NewMemoryRepository(), permissiveConstitution(), nil, nil)
	request := selectorV5Request("selection-changed-before-consumption")
	resolver := withMatchingFrameworkSelection(t, service, *request.Governance)
	resolver.onCall = func(call int, value FrameworkSelectionSnapshot) (FrameworkSelectionSnapshot, error) {
		if call > 1 {
			return FrameworkSelectionSnapshot{}, ErrNotFound
		}
		return value, nil
	}

	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"test-worker",
		"workspace-file:brief.txt",
	)
	if !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("AuthorizeAndConsume error = %v, want ErrAuthorizationChanged", err)
	}
	if receipt.Outcome != OutcomeAuthorized || resolver.calls != 2 {
		t.Fatalf("receipt = %#v; resolver calls = %d", receipt, resolver.calls)
	}
}

func TestLegacyFrameworkGovernanceCannotAuthorizeNewExecution(t *testing.T) {
	service := newTestService(t, NewMemoryRepository(), permissiveConstitution(), nil, nil)
	request := baseRequest("legacy-selection-compatibility")
	digest := strings.Repeat("b", 64)
	request.Governance = &GovernanceEvidence{
		TaskPlanID:                        "plan-legacy",
		TaskPlanDigest:                    digest,
		FrameworkSelectionID:              "selection-legacy",
		FrameworkCatalogVersion:           "framework-catalog-v1",
		FrameworkSelectorAlgorithmVersion: frameworkSelectorV4,
		FrameworkCatalogDigest:            digest,
		FrameworkPreferenceDigest:         digest,
		FrameworkConstitutionDigest:       digest,
		FrameworkOperatingContractDigest:  digest,
	}
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied || receipt.Evidence.FrameworkSelection.Verified ||
		!containsFold(receipt.Evidence.ReasonCodes, "framework.selection_legacy_execution_denied") {
		t.Fatalf("legacy receipt = %#v", receipt)
	}
}
