package executionauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/standingmandate"

	"github.com/google/uuid"
)

type fakeConstitutionEvaluator struct {
	mu       sync.Mutex
	decision ConstitutionDecision
	err      error
	calls    int
	onCall   func(int, ConstitutionDecision) ConstitutionDecision
}

func (f *fakeConstitutionEvaluator) EvaluateExecutionPolicy(
	_ context.Context,
	_ string,
	_ []string,
	_ int,
) (ConstitutionDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	decision := f.decision
	if f.onCall != nil {
		decision = f.onCall(f.calls, decision)
	}
	return decision, f.err
}

type fakeApprovalResolver struct {
	values map[string]ResolvedApproval
}

type rotatingApprovalResolver struct {
	values []ResolvedApproval
	calls  int
}

func (r *rotatingApprovalResolver) Resolve(
	_ context.Context,
	_, _ string,
	_ string,
) (ResolvedApproval, error) {
	if len(r.values) == 0 {
		return ResolvedApproval{}, ErrNotFound
	}
	index := r.calls
	r.calls++
	if index >= len(r.values) {
		index = len(r.values) - 1
	}
	return r.values[index], nil
}

func (f fakeApprovalResolver) Resolve(
	_ context.Context,
	owner string,
	sourceID string,
	bindingDigest string,
) (ResolvedApproval, error) {
	value, ok := f.values[owner+"\x00"+sourceID]
	if !ok || value.BindingDigest != bindingDigest {
		return ResolvedApproval{}, ErrNotFound
	}
	return value, nil
}

type fakeAgentResolver struct {
	agent      agentregistry.Agent
	assignment agentregistry.Assignment
}

func (f fakeAgentResolver) Get(
	_ context.Context,
	owner string,
	id string,
) (agentregistry.Agent, error) {
	if f.agent.OwnerIdentity != owner || f.agent.ID != id {
		return agentregistry.Agent{}, ErrNotFound
	}
	return f.agent, nil
}

func (f fakeAgentResolver) GetAssignment(
	_ context.Context,
	owner string,
	id string,
) (agentregistry.Assignment, error) {
	if f.assignment.OwnerIdentity != owner || f.assignment.ID != id {
		return agentregistry.Assignment{}, ErrNotFound
	}
	return f.assignment, nil
}

func permissiveConstitution() *fakeConstitutionEvaluator {
	return &fakeConstitutionEvaluator{
		decision: ConstitutionDecision{
			ID:               "builtin-robert-constitution-v1",
			Version:          1,
			Source:           "builtin-robert-constitution-v1:v1",
			Digest:           strings.Repeat("c", 64),
			AuthorityCeiling: 10,
		},
	}
}

func baseRequest(key string) Request {
	return Request{
		OwnerIdentity:     "alice",
		IdempotencyKey:    key,
		ActorIdentity:     "alice",
		ActorKind:         ActorHuman,
		TaskID:            "task-1",
		Action:            "workspace.safe_worker.execute",
		Stage:             StageExecution,
		ResourceType:      "workspace-file",
		ResourceID:        "brief.txt",
		ToolID:            "local-safe-worker",
		FolderPaths:       []string{"C:/HAI/workspace"},
		RequiredAuthority: 8,
		RequestedAutonomy: 8,
		Risk:              RiskLow,
		Reversible:        true,
		EffectDigest:      strings.Repeat("e", 64),
	}
}

func TestUnknownSystemActorIsDeniedBeforePolicyEvaluation(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request := baseRequest("unknown-system-workload")
	request.ActorIdentity = "system:invented-worker"
	request.ActorKind = ActorSystem

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied || !containsFold(receipt.Evidence.ReasonCodes, "system_workload.denied") {
		t.Fatalf("unknown system workload receipt = %#v", receipt)
	}
	if receipt.Evidence.SystemWorkload.Matched || receipt.Evidence.SystemWorkload.ActorIdentity != request.ActorIdentity {
		t.Fatalf("unknown system workload evidence = %#v", receipt.Evidence.SystemWorkload)
	}
}

func TestSystemWorkloadCannotSelfDowngradeRisk(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request := baseRequest("system-risk-downgrade")
	request.ActorIdentity = "hai-task-engine"
	request.ActorKind = ActorSystem
	request.Action = AgentRuntimeExecuteAction
	request.ResourceType = AgentRuntimeResourceType
	request.ToolID = "automation-agent-runtime"
	request.RuntimeID = "openclaw"
	request.RequiredAuthority = 6
	request.RequestedAutonomy = 6
	request.Risk = RiskLow
	request.Reversible = false

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied || receipt.Evidence.SystemWorkload.Matched {
		t.Fatalf("risk-downgraded system workload receipt = %#v", receipt)
	}
}

func TestRegisteredSystemWorkloadUsesExactServerPolicy(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request := baseRequest("registered-safe-worker")
	request.ActorIdentity = "system:phase2-safe-worker"
	request.ActorKind = ActorSystem
	request.Action = "executionbroker.local-safe-worker.write"
	request.ResourceType = "executionbroker.final-effect"
	request.ToolID = "phase2-local-safe-worker"
	request.RuntimeID = "hai-local-safe-worker"
	request.RequiredAuthority = 1
	request.RequestedAutonomy = 8
	request.Risk = RiskLow
	request.Reversible = true

	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized || !receipt.Evidence.SystemWorkload.Matched ||
		receipt.Evidence.SystemWorkload.PolicyID != "phase2-local-safe-worker-v1" {
		t.Fatalf("registered system workload receipt = %#v", receipt)
	}
}

func TestAuthorizeAndConsumeRejectsSystemWorkloadPolicyChange(t *testing.T) {
	originalPolicies := append([]systemWorkloadPolicy(nil), builtInSystemWorkloadPolicies...)
	defer func() { builtInSystemWorkloadPolicies = originalPolicies }()

	constitution := permissiveConstitution()
	constitution.onCall = func(call int, decision ConstitutionDecision) ConstitutionDecision {
		if call == 1 {
			builtInSystemWorkloadPolicies = []systemWorkloadPolicy{}
		}
		return decision
	}
	service := newTestService(t, NewMemoryRepository(), constitution, nil, nil)
	request := baseRequest("system-policy-changed-before-consumption")
	request.ActorIdentity = "system:phase2-safe-worker"
	request.ActorKind = ActorSystem
	request.Action = "executionbroker.local-safe-worker.write"
	request.ResourceType = "executionbroker.final-effect"
	request.ToolID = "phase2-local-safe-worker"
	request.RuntimeID = "hai-local-safe-worker"
	request.RequiredAuthority = 1
	request.RequestedAutonomy = 8
	request.Risk = RiskLow
	request.Reversible = true

	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"safe-worker",
		"C:/HAI/workspace/brief.txt",
	)
	if !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("AuthorizeAndConsume error = %v, want ErrAuthorizationChanged", err)
	}
	if receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("receipt outcome = %q, want authorized pre-consumption decision", receipt.Outcome)
	}
}

func TestDeriveCapabilitiesMarksNetworkAPIAccess(t *testing.T) {
	capabilities := deriveCapabilities(Request{
		Action:       "automation.api.read",
		Stage:        StageDataAccess,
		ResourceType: "network-api",
		ToolID:       "automation-api-client",
	})
	for _, required := range []string{
		"document-read",
		"execution",
		"tool-execution",
		"web-access",
	} {
		if !containsFold(capabilities, required) {
			t.Fatalf("capabilities %v do not contain %q", capabilities, required)
		}
	}
}

func TestTrustedMandateFactsOverrideCallerControlledPolicyInputs(t *testing.T) {
	request := baseRequest("trusted-facts")
	request.EstimatedCostEUR = 12.5
	request.RequestedAt = time.Date(2026, 7, 31, 14, 30, 0, 0, time.UTC)
	request.Facts = map[string]string{
		"estimated_cost_eur":       "0",
		"requested_at_utc_hour":    "00",
		"requested_at_utc_weekday": "sunday",
		"custom_fact":              "preserved",
	}

	facts := trustedMandateFacts(request)
	if facts["estimated_cost_eur"] != "12.500000" ||
		facts["requested_at_utc_hour"] != "14" ||
		facts["requested_at_utc_weekday"] != "friday" ||
		facts["requested_at_utc_date"] != "2026-07-31" ||
		facts["custom_fact"] != "preserved" {
		t.Fatalf("trusted mandate facts = %#v", facts)
	}
}

func TestStandingMandateReceiptBindsExactPolicyAndDecision(t *testing.T) {
	mandates, active := activeExecutionMandate(t)
	service, err := NewService(
		NewMemoryRepository(),
		permissiveConstitution(),
		mandates,
		nil,
		nil,
		fixedNow,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() EmergencyStopEvidence {
		return EmergencyStopEvidence{Source: "test"}
	})
	request := baseRequest("mandate-bound")
	request.MandateID = active.ID.String()
	request.RequestedAutonomy = 7

	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"test-consumer",
		"workspace:brief.txt",
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	evidence := receipt.Evidence.Mandate
	if receipt.Outcome != OutcomeAuthorized || evidence.ID != active.ID.String() ||
		evidence.Revision != active.Revision || len(evidence.RequestDigest) != 64 ||
		len(evidence.MandateDigest) != 64 || len(evidence.DecisionDigest) != 64 {
		t.Fatalf("mandate-bound receipt = %#v", receipt)
	}
	decisionID, parseErr := uuid.Parse(evidence.DecisionID)
	if parseErr != nil {
		t.Fatalf("decision id = %q: %v", evidence.DecisionID, parseErr)
	}
	decision, err := mandates.GetDecision(context.Background(), "alice", decisionID)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if decision.Evidence.RequestDigest != evidence.RequestDigest ||
		decision.Evidence.MandateDigest != evidence.MandateDigest ||
		decision.Evidence.DecisionDigest != evidence.DecisionDigest {
		t.Fatalf("decision = %#v, receipt evidence = %#v", decision, evidence)
	}
}

func TestStandingMandateRevocationBeforeConsumptionFailsClosed(t *testing.T) {
	mandates, active := activeExecutionMandate(t)
	constitution := permissiveConstitution()
	constitution.onCall = func(call int, decision ConstitutionDecision) ConstitutionDecision {
		if call == 2 {
			if _, err := mandates.Revoke(
				context.Background(),
				"alice",
				active.ID,
				active.Revision,
				"alice",
				"operator revoked before effect",
			); err != nil {
				t.Fatalf("Revoke: %v", err)
			}
		}
		return decision
	}
	service, err := NewService(
		NewMemoryRepository(),
		constitution,
		mandates,
		nil,
		nil,
		fixedNow,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() EmergencyStopEvidence {
		return EmergencyStopEvidence{Source: "test"}
	})
	request := baseRequest("mandate-revoked-before-consumption")
	request.MandateID = active.ID.String()
	request.RequestedAutonomy = 7

	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"test-consumer",
		"workspace:brief.txt",
	)
	if !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("AuthorizeAndConsume error = %v, want ErrAuthorizationChanged", err)
	}
	if receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("pre-consumption receipt = %#v", receipt)
	}
}

func activeExecutionMandate(t *testing.T) (*standingmandate.Service, *standingmandate.StandingMandate) {
	t.Helper()
	service, err := standingmandate.NewService(
		standingmandate.NewMemoryRepository(),
		fixedNow,
	)
	if err != nil {
		t.Fatalf("standingmandate.NewService: %v", err)
	}
	expires := fixedNow().Add(time.Hour)
	mandate, err := service.Create(context.Background(), standingmandate.CreateRequest{
		OwnerIdentity:   "alice",
		Name:            "Bounded workspace update",
		Purpose:         "Authorize one safe workspace action.",
		Version:         "1.0.0",
		AutonomyCeiling: 7,
		Scopes: []standingmandate.Scope{{
			ID:          "workspace-brief",
			Actions:     []string{"workspace.safe_worker.execute"},
			Resources:   []standingmandate.ResourceScope{{Type: "workspace-file", IDs: []string{"brief.txt"}}},
			Tools:       []string{"local-safe-worker"},
			MaximumRisk: standingmandate.RiskLow,
		}},
		ApprovalPolicy: standingmandate.ApprovalPolicy{Mode: standingmandate.ApprovalNever},
		CreatedBy:      "alice",
		ExpiresAt:      &expires,
	})
	if err != nil {
		t.Fatalf("Create mandate: %v", err)
	}
	active, err := service.Activate(
		context.Background(),
		"alice",
		mandate.ID,
		mandate.Revision,
	)
	if err != nil {
		t.Fatalf("Activate mandate: %v", err)
	}
	return service, active
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
}

func newTestService(
	t *testing.T,
	repository Repository,
	constitution ConstitutionEvaluator,
	approvals ApprovalResolver,
	agents AgentAuthorityResolver,
) *Service {
	t.Helper()
	service, err := NewService(
		repository,
		constitution,
		nil,
		agents,
		approvals,
		fixedNow,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() EmergencyStopEvidence {
		return EmergencyStopEvidence{Source: "test"}
	})
	return service
}

func TestAuthorizePersistsEmergencyStopDenial(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		nil,
		nil,
	)
	service.WithEmergencyStopEvaluator(func() EmergencyStopEvidence {
		return EmergencyStopEvidence{
			Active: true,
			Source: "persisted-emergency-stop",
			Reason: "operator stop",
		}
	})

	receipt, err := service.Authorize(context.Background(), baseRequest("stop-1"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied ||
		receipt.Evidence.EmergencyStop.Source != "persisted-emergency-stop" {
		t.Fatalf("unexpected emergency-stop receipt: %#v", receipt)
	}
	stored, err := service.Get(context.Background(), "alice", receipt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.DecisionDigest == "" || stored.Outcome != OutcomeDenied {
		t.Fatalf("denial was not durably auditable: %#v", stored)
	}
}

func TestEmergencyStopPermitsOnlyExactApprovedClearance(t *testing.T) {
	binding := strings.Repeat("a", 64)
	approval := ResolvedApproval{
		SourceID:       "opscontrol-owner:test-approval",
		DecisionID:     "owner-confirmed-clearance",
		DecisionDigest: strings.Repeat("b", 64),
		BindingDigest:  binding,
		ApprovedBy:     "alice",
		ApproverRoles:  []string{"owner"},
		ApprovedAt:     fixedNow().Add(-time.Minute),
		ExpiresAt:      fixedNow().Add(time.Minute),
	}
	service := newTestService(
		t,
		NewMemoryRepository(),
		permissiveConstitution(),
		fakeApprovalResolver{values: map[string]ResolvedApproval{
			"alice\x00" + approval.SourceID: approval,
		}},
		nil,
	)
	service.WithEmergencyStopEvaluator(func() EmergencyStopEvidence {
		return EmergencyStopEvidence{Active: true, Source: "persisted-emergency-stop", Reason: "operator stop"}
	})

	clearance := baseRequest("emergency-stop-clear")
	clearance.Action = "opscontrol.emergency-stop.clear"
	clearance.Stage = StagePrivilegeEscalation
	clearance.Domain = "safety-control"
	clearance.ResourceType = "opscontrol-emergency-stop"
	clearance.ResourceID = "emergency-stop:revision-1"
	clearance.RequiredAuthority = 10
	clearance.RequestedAutonomy = 6
	clearance.Risk = RiskCritical
	clearance.Reversible = false
	clearance.ApprovalSourceID = approval.SourceID
	clearance.ApprovalBindingDigest = binding
	clearance.EffectDigest = binding

	receipt, err := service.AuthorizeAndConsume(context.Background(), clearance, "opscontrol", clearance.Action+":"+clearance.ResourceID)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume exact emergency-stop clearance: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized || !receipt.Evidence.EmergencyStop.Active || receipt.Evidence.Approval.SourceID != approval.SourceID {
		t.Fatalf("clearance receipt = %#v", receipt)
	}

	denied := clearance
	denied.IdempotencyKey = "emergency-stop-clear-not-exact"
	denied.ResourceType = "workspace-file"
	deniedReceipt, err := service.Authorize(context.Background(), denied)
	if err != nil {
		t.Fatalf("Authorize non-clearance request: %v", err)
	}
	if deniedReceipt.Outcome != OutcomeDenied || !containsFold(deniedReceipt.Evidence.ReasonCodes, "emergency_stop.active") {
		t.Fatalf("non-clearance receipt = %#v", deniedReceipt)
	}
}

func TestAuthorizeAndConsumeIsSingleUseUnderConcurrency(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		nil,
		nil,
	)
	request := baseRequest("concurrent-1")

	var successes atomic.Int32
	var alreadyConsumed atomic.Int32
	var unexpectedMu sync.Mutex
	var unexpected []error
	var wg sync.WaitGroup
	for index := 0; index < 16; index++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			_, err := service.AuthorizeAndConsume(
				context.Background(),
				request,
				fmt.Sprintf("worker-%d", worker),
				"C:/HAI/workspace/brief.txt",
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrAlreadyConsumed):
				alreadyConsumed.Add(1)
			default:
				unexpectedMu.Lock()
				unexpected = append(unexpected, err)
				unexpectedMu.Unlock()
			}
		}(index)
	}
	wg.Wait()

	if len(unexpected) != 0 {
		t.Fatalf("unexpected errors: %v", unexpected)
	}
	if successes.Load() != 1 || alreadyConsumed.Load() != 15 {
		t.Fatalf(
			"single-use result successes=%d alreadyConsumed=%d",
			successes.Load(),
			alreadyConsumed.Load(),
		)
	}
}

func TestAuthorizeRejectsIdempotencyKeyReuseForDifferentAction(t *testing.T) {
	service := newTestService(
		t,
		NewMemoryRepository(),
		permissiveConstitution(),
		nil,
		nil,
	)
	request := baseRequest("same-key")
	if _, err := service.Authorize(context.Background(), request); err != nil {
		t.Fatalf("first Authorize: %v", err)
	}
	request.Action = "workspace.safe_worker.delete"
	request.Stage = StageDeletion
	request.Risk = RiskHigh
	request.Reversible = false
	if _, err := service.Authorize(context.Background(), request); !errors.Is(
		err,
		ErrIdempotencyConflict,
	) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestAuthorizePersistsGovernanceEvidenceWithoutTreatingItAsAuthority(t *testing.T) {
	service := newTestService(
		t,
		NewMemoryRepository(),
		permissiveConstitution(),
		nil,
		nil,
	)
	request := baseRequest("governance-evidence")
	maximumAutonomy := request.RequestedAutonomy
	requiresApproval := false
	request.Governance = &GovernanceEvidence{
		TaskPlanID:                        "plan-1",
		TaskPlanDigest:                    strings.Repeat("a", 64),
		FrameworkEvidencePreflightDigest:  strings.Repeat("f", 64),
		FrameworkSelectionID:              "selection-1",
		FrameworkCatalogVersion:           "framework-catalog-v2",
		FrameworkSelectorAlgorithmVersion: "selector-v5",
		FrameworkTaskRiskLevel:            RiskLow,
		FrameworkEffectiveRiskCeiling:     RiskHigh,
		FrameworkMaximumAutonomyLevel:     &maximumAutonomy,
		FrameworkRequiresApproval:         &requiresApproval,
		FrameworkCatalogDigest:            strings.Repeat("b", 64),
		FrameworkPreferenceDigest:         strings.Repeat("c", 64),
		FrameworkConstitutionDigest:       strings.Repeat("d", 64),
		FrameworkOperatingContractDigest:  strings.Repeat("e", 64),
		EvidenceReferences:                []string{"task-plan://plan-1"},
	}
	withMatchingFrameworkSelection(t, service, *request.Governance)
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Evidence.Governance.TaskPlanDigest != request.Governance.TaskPlanDigest ||
		receipt.Evidence.Governance.FrameworkSelectorAlgorithmVersion != "selector-v5" ||
		receipt.Evidence.Governance.FrameworkTaskRiskLevel != RiskLow ||
		receipt.Evidence.Governance.FrameworkEffectiveRiskCeiling != RiskHigh ||
		receipt.Evidence.Governance.FrameworkMaximumAutonomyLevel == nil ||
		*receipt.Evidence.Governance.FrameworkMaximumAutonomyLevel != maximumAutonomy ||
		receipt.Evidence.Governance.FrameworkRequiresApproval == nil ||
		*receipt.Evidence.Governance.FrameworkRequiresApproval ||
		receipt.Evidence.Governance.FrameworkOperatingContractDigest !=
			request.Governance.FrameworkOperatingContractDigest {
		t.Fatalf("governance evidence was not retained: %#v", receipt.Evidence.Governance)
	}
	if receipt.Evidence.Constitution.Digest == "" {
		t.Fatal("governance evidence displaced the independent Constitution decision")
	}
}

func TestAuthorizeEnforcesFrameworkAutonomyAndCaseApproval(t *testing.T) {
	maximumAutonomy := 6
	requiresApproval := true
	digest := strings.Repeat("a", 64)
	request := baseRequest("framework-execution-contract")
	request.RequestedAutonomy = maximumAutonomy
	request.Governance = &GovernanceEvidence{
		TaskPlanID: "plan-1", TaskPlanDigest: digest,
		FrameworkEvidencePreflightDigest: strings.Repeat("f", 64),
		FrameworkSelectionID:             "selection-1", FrameworkCatalogVersion: "framework-catalog-v2",
		FrameworkSelectorAlgorithmVersion: "selector-v5",
		FrameworkTaskRiskLevel:            RiskLow, FrameworkEffectiveRiskCeiling: RiskHigh,
		FrameworkMaximumAutonomyLevel: &maximumAutonomy,
		FrameworkRequiresApproval:     &requiresApproval,
		FrameworkCatalogDigest:        digest, FrameworkPreferenceDigest: digest,
		FrameworkConstitutionDigest: digest, FrameworkOperatingContractDigest: digest,
	}
	service := newTestService(t, NewMemoryRepository(), permissiveConstitution(), nil, nil)
	withMatchingFrameworkSelection(t, service, *request.Governance)
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeRequiresApproval ||
		!strings.Contains(strings.Join(receipt.Evidence.ReasonCodes, ","), "framework.approval_required") {
		t.Fatalf("framework approval decision = %#v", receipt)
	}

	request.IdempotencyKey = "framework-autonomy-overreach"
	request.RequestedAutonomy = maximumAutonomy + 1
	if _, err := service.Authorize(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "exceeds framework maximum autonomy") {
		t.Fatalf("autonomy overreach error = %v", err)
	}
}

func TestAuthorizeRejectsInvalidSelectorV5RiskBeforePolicyEvaluation(t *testing.T) {
	constitution := permissiveConstitution()
	service := newTestService(t, NewMemoryRepository(), constitution, nil, nil)
	digest := strings.Repeat("a", 64)
	request := baseRequest("invalid-selector-v5-risk")
	request.Risk = RiskHigh
	request.Governance = &GovernanceEvidence{
		TaskPlanID: "plan-1", TaskPlanDigest: digest,
		FrameworkSelectionID: "selection-1", FrameworkCatalogVersion: "framework-catalog-v2",
		FrameworkSelectorAlgorithmVersion: "selector-v5",
		FrameworkTaskRiskLevel:            RiskHigh, FrameworkEffectiveRiskCeiling: RiskMedium,
		FrameworkCatalogDigest: digest, FrameworkPreferenceDigest: digest,
		FrameworkConstitutionDigest: digest, FrameworkOperatingContractDigest: digest,
	}

	if _, err := service.Authorize(context.Background(), request); err == nil {
		t.Fatal("Authorize accepted task risk above the selected framework ceiling")
	}
	constitution.mu.Lock()
	calls := constitution.calls
	constitution.mu.Unlock()
	if calls != 0 {
		t.Fatalf("Constitution calls = %d, want zero before v5 governance validation", calls)
	}
}

func TestAuthorizeRequiresServerResolvedExactCaseApproval(t *testing.T) {
	now := fixedNow()
	sourceID := "task-review:216967e4-d62e-4a73-ae3f-c62efcbf78f5"
	binding := strings.Repeat("a", 64)
	approvals := fakeApprovalResolver{values: map[string]ResolvedApproval{
		"alice\x00" + sourceID: {
			SourceID:       sourceID,
			DecisionID:     "a981bcb2-c57d-4f08-9504-91be0f20d287",
			DecisionDigest: strings.Repeat("d", 64),
			BindingDigest:  binding,
			ApprovedBy:     "alice",
			ApprovedAt:     now.Add(-time.Minute),
			ExpiresAt:      now.Add(10 * time.Minute),
		},
	}}
	service := newTestService(
		t,
		NewMemoryRepository(),
		permissiveConstitution(),
		approvals,
		nil,
	)

	unapproved := baseRequest("case-unapproved")
	unapproved.RequiredAuthority = 6
	unapproved.RequestedAutonomy = 6
	receipt, err := service.Authorize(context.Background(), unapproved)
	if err != nil {
		t.Fatalf("unapproved Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeRequiresApproval {
		t.Fatalf("unapproved outcome = %q", receipt.Outcome)
	}

	invented := unapproved
	invented.IdempotencyKey = "case-invented"
	invented.ApprovalSourceID = "task-review:00000000-0000-4000-8000-000000000001"
	invented.ApprovalBindingDigest = binding
	receipt, err = service.Authorize(context.Background(), invented)
	if err != nil {
		t.Fatalf("invented Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeDenied ||
		receipt.Evidence.Approval.DecisionID != "" {
		t.Fatalf("invented approval was accepted: %#v", receipt)
	}

	approved := unapproved
	approved.IdempotencyKey = "case-approved"
	approved.ApprovalSourceID = sourceID
	approved.ApprovalBindingDigest = binding
	receipt, err = service.Authorize(context.Background(), approved)
	if err != nil {
		t.Fatalf("approved Authorize: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized ||
		receipt.Evidence.Approval.SourceID != sourceID {
		t.Fatalf("approved outcome = %#v", receipt)
	}
}

func TestAuthorizeAndConsumeRejectsApprovalDecisionRotation(t *testing.T) {
	now := fixedNow()
	sourceID := "opscontrol-owner:approval-nonce"
	binding := strings.Repeat("a", 64)
	first := ResolvedApproval{
		SourceID: sourceID, DecisionID: "decision-one",
		DecisionDigest: strings.Repeat("b", 64), BindingDigest: binding,
		ApprovedBy: "alice", ApprovedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
	rotated := first
	rotated.DecisionID = "decision-two"
	rotated.DecisionDigest = strings.Repeat("c", 64)
	approvals := &rotatingApprovalResolver{values: []ResolvedApproval{first, rotated}}
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), approvals, nil)
	request := baseRequest("approval-rotation")
	request.ApprovalSourceID = sourceID
	request.ApprovalBindingDigest = binding
	target := "workspace:" + request.ResourceID

	receipt, err := service.AuthorizeAndConsume(context.Background(), request, "workspace-worker", target)
	if !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("AuthorizeAndConsume error = %v, want ErrAuthorizationChanged", err)
	}
	if receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("initial authorization outcome = %q, want authorized receipt before recheck", receipt.Outcome)
	}
	if _, err := repository.GetConsumption(context.Background(), request.OwnerIdentity, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotated approval created consumption: %v", err)
	}
}

func TestMemoryRepositoryClaimsTaskReviewDecisionAcrossReceiptsAtomically(t *testing.T) {
	now := fixedNow()
	sourceID := "task-review:216967e4-d62e-4a73-ae3f-c62efcbf78f5"
	binding := strings.Repeat("a", 64)
	decisionID := "a981bcb2-c57d-4f08-9504-91be0f20d287"
	repository := NewMemoryRepository()
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		fakeApprovalResolver{values: map[string]ResolvedApproval{
			"alice\x00" + sourceID: {
				SourceID: sourceID, DecisionID: decisionID,
				DecisionDigest: strings.Repeat("d", 64), BindingDigest: binding,
				ApprovedBy: "alice", ApprovedAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute),
			},
		}},
		nil,
	)

	request := baseRequest("task-review-claim-first")
	request.ApprovalSourceID = sourceID
	request.ApprovalBindingDigest = binding
	first, err := service.Authorize(context.Background(), request)
	if err != nil || first.Outcome != OutcomeAuthorized {
		t.Fatalf("first authorization = (%#v, %v), want authorized", first, err)
	}
	request.IdempotencyKey = "task-review-claim-second"
	second, err := service.Authorize(context.Background(), request)
	if err != nil || second.Outcome != OutcomeAuthorized {
		t.Fatalf("second authorization = (%#v, %v), want authorized before claim", second, err)
	}

	consumption := func(receipt Receipt, consumer string) Consumption {
		return Consumption{
			ReceiptID: receipt.ID, OwnerIdentity: receipt.OwnerIdentity,
			Consumer: consumer, ExecutionTarget: "workspace:" + receipt.ResourceID,
			ReceiptDigest: receipt.DecisionDigest, ConsumedAt: now.Add(time.Second),
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- repository.Consume(context.Background(), consumption(first, "worker-one"))
	}()
	go func() {
		<-start
		results <- repository.Consume(context.Background(), consumption(second, "worker-two"))
	}()
	close(start)

	successes, claimed := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrApprovalAlreadyClaimed):
			claimed++
		default:
			t.Fatalf("unexpected concurrent consume result: %v", err)
		}
	}
	if successes != 1 || claimed != 1 {
		t.Fatalf("consumption successes=%d approval-claimed=%d, want 1 and 1", successes, claimed)
	}
}

func TestAuthorizeDeniesConstitutionCapabilityAndAuthority(t *testing.T) {
	tests := []struct {
		name     string
		decision ConstitutionDecision
	}{
		{
			name: "capability denied",
			decision: ConstitutionDecision{
				ID:                 "constitution-1",
				Version:            2,
				Source:             "constitution-1:v2",
				Digest:             strings.Repeat("e", 64),
				AuthorityCeiling:   10,
				DeniedCapabilities: []string{"local-execution"},
			},
		},
		{
			name: "authority ceiling",
			decision: ConstitutionDecision{
				ID:               "constitution-1",
				Version:          2,
				Source:           "constitution-1:v2",
				Digest:           strings.Repeat("e", 64),
				AuthorityCeiling: 4,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(
				t,
				NewMemoryRepository(),
				&fakeConstitutionEvaluator{decision: test.decision},
				nil,
				nil,
			)
			receipt, err := service.Authorize(
				context.Background(),
				baseRequest(
					"constitution-"+strings.ReplaceAll(test.name, " ", "-"),
				),
			)
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if receipt.Outcome != OutcomeDenied {
				t.Fatalf("outcome = %q, want denied", receipt.Outcome)
			}
		})
	}
}

func TestAuthorizeAndConsumeRejectsConstitutionChange(t *testing.T) {
	constitution := permissiveConstitution()
	constitution.onCall = func(call int, value ConstitutionDecision) ConstitutionDecision {
		if call > 1 {
			value.Digest = strings.Repeat("f", 64)
		}
		return value
	}
	repository := NewMemoryRepository()
	service := newTestService(t, repository, constitution, nil, nil)

	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		baseRequest("constitution-change"),
		"worker",
		"C:/HAI/workspace/brief.txt",
	)
	if !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("error = %v, want ErrAuthorizationChanged", err)
	}
	if _, err := repository.GetConsumption(
		context.Background(),
		"alice",
		receipt.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consumption error = %v, want ErrNotFound", err)
	}
}

func TestAuthorizeEnforcesAgentAssignmentAndAllowlists(t *testing.T) {
	now := fixedNow()
	resolver := fakeAgentResolver{
		agent: agentregistry.Agent{
			ID:               "agent-1",
			OwnerIdentity:    "alice",
			Runtime:          agentregistry.RuntimeAdapter{ID: "runtime-1"},
			AuthorityCeiling: 8,
			AutonomyCeiling:  8,
			ToolAllowlist:    []string{"local-safe-worker"},
			DataAllowlist:    []string{"project-files"},
			FolderAllowlist:  []string{"C:/HAI/workspace"},
			Health: agentregistry.HealthEvidence{
				Status:    agentregistry.HealthHealthy,
				Ready:     true,
				CheckedAt: now.Add(-time.Minute),
				FreshFor:  5 * time.Minute,
			},
			State:    agentregistry.StateEnabled,
			Revision: 7,
		},
		assignment: agentregistry.Assignment{
			ID:               "assignment-1",
			OwnerIdentity:    "alice",
			TaskID:           "task-1",
			AgentID:          "agent-1",
			AgentRevision:    7,
			GrantedAuthority: 8,
			GrantedAutonomy:  8,
		},
	}
	service := newTestService(
		t,
		NewMemoryRepository(),
		permissiveConstitution(),
		nil,
		resolver,
	)
	request := baseRequest("agent-allowed")
	request.ActorKind = ActorAgent
	request.ActorIdentity = "agent-1"
	request.AgentID = "agent-1"
	request.AssignmentID = "assignment-1"
	request.RuntimeID = "runtime-1"
	request.DataScopes = []string{"project-files"}
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize allowed agent: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized ||
		receipt.Evidence.Agent.AgentRevision != 7 {
		t.Fatalf("allowed agent receipt = %#v", receipt)
	}

	request.IdempotencyKey = "agent-denied"
	request.DataScopes = []string{"private-mailbox"}
	receipt, err = service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize denied agent: %v", err)
	}
	if receipt.Outcome != OutcomeDenied {
		t.Fatalf("unallowlisted data outcome = %q", receipt.Outcome)
	}
}
