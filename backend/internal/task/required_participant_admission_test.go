package task

import (
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/frameworkregistry"

	"github.com/google/uuid"
)

func TestFrameworkRequiredParticipantCannotBeBypassedByAutomaticOrApprovedRuntime(t *testing.T) {
	for _, approved := range []bool{false, true} {
		name := "automatic level 8"
		if approved {
			name = "approved level 6"
		}
		t.Run(name, func(t *testing.T) {
			decision := frameworkregistry.SelectionDecision{
				MaximumAutonomyLevel: 8,
				RequiredAgents:       []string{"evidence_reviewer", "red_team_reviewer"},
				Coordination:         frameworkregistry.CoordinationPlan{Mode: "single_engine"},
				Delegations: []frameworkregistry.DelegationContract{
					{Delegatee: "hai_task_engine", State: "ready"},
					{Delegatee: "evidence_reviewer", State: "required_unassigned"},
					{Delegatee: "red_team_reviewer", State: "requires_assignment"},
					{Delegatee: "evidence_reviewer", State: "required_unassigned"},
				},
			}
			risk := applyFrameworkRisk(RiskAssessment{Level: "low", AllowedNow: true,
				ApprovalGranted: approved}, &decision, IntakeAnalysis{NeedsTools: true},
				IntakeRequest{ExecuteAllowed: true, HumanApproved: approved,
					AutomationID: uuid.NewString(), agentInventoryEvaluated: true})
			if risk.AllowedNow {
				t.Fatal("runtime/approval bypassed independent participants")
			}
			if !reflect.DeepEqual(risk.MissingRequiredAgents, []string{"evidence_reviewer", "red_team_reviewer"}) {
				t.Fatalf("missing participants lost or duplicated: %v", risk.MissingRequiredAgents)
			}
			if reason := taskReviewReason(risk); !strings.Contains(reason, "evidence_reviewer, red_team_reviewer") {
				t.Fatalf("recovery queue hid actual missing participants: %s", reason)
			}
		})
	}
}

func TestApprovedReadinessProbeDoesNotFabricateSpecialistEvidence(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}
	service := NewServiceWithDependenciesAndAgentContext(
		&fakeMemoryService{}, newTaskNoProviderLLMService(t), nil,
		&sequencedVerificationService{}, executor, nil, selector,
		NewMemoryTaskStateRepository(), nil, emptyAgentContextProvider{},
	)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity: "operator@example.test", WorkflowID: uuid.NewString(),
		Request:      "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		AutomationID: executor.result.AutomationID, ExecuteAllowed: true, HumanApproved: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 || plan.RiskAssessment.AllowedNow || len(plan.RiskAssessment.MissingRequiredAgents) == 0 {
		t.Fatalf("unimplemented specialist work was passed off as complete: calls=%d risk=%#v", executor.calls, plan.RiskAssessment)
	}
	if !strings.Contains(strings.Join(plan.RiskAssessment.Reasons, "\n"), "fresh verified agent card") {
		t.Fatal("actual recovery prerequisite was hidden")
	}
}

func TestReadyRequiredParticipantsRemainEligible(t *testing.T) {
	decision := frameworkregistry.SelectionDecision{
		MaximumAutonomyLevel: 8, RequiredAgents: []string{"evidence_reviewer"},
		Delegations: []frameworkregistry.DelegationContract{{Delegatee: "evidence_reviewer", State: "ready"}},
	}
	risk := applyFrameworkRisk(RiskAssessment{Level: "low", AllowedNow: true}, &decision,
		IntakeAnalysis{NeedsTools: true}, IntakeRequest{ExecuteAllowed: true,
			AutomationID: uuid.NewString(), agentInventoryEvaluated: true})
	if !risk.AllowedNow || len(risk.MissingRequiredAgents) != 0 {
		t.Fatalf("ready participants were spuriously blocked: %#v", risk)
	}
}
