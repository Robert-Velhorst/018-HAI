package task

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/resourceplanner"

	"github.com/google/uuid"
)

func TestExecutionGovernanceEvidenceBindsFrameworkDomainAndPlan(t *testing.T) {
	digest := func(value string) string { return strings.Repeat(value, 64) }
	plan := &CompletionPlan{
		ID:            "plan-1",
		OwnerIdentity: "alice",
		Request:       "Prepare a legal evidence response",
		ProjectKey:    "case-1",
		RealGoal:      "Prepare a grounded response for review",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID:                        "selection-1",
			CatalogVersion:            "framework-catalog-v2",
			SelectorAlgorithmVersion:  "selector-v5",
			TaskRiskLevel:             "high",
			EffectiveRiskCeiling:      "high",
			MaximumAutonomyLevel:      6,
			RequiresApproval:          true,
			CatalogDigest:             digest("a"),
			EffectivePreferenceDigest: digest("b"),
			ConstitutionDigest:        digest("c"),
			OperatingContractDigest:   digest("d"),
		},
		DomainPackDecision: &DomainPackDecision{
			ID:                        "domain-pack-decision-1",
			Digest:                    "sha256:" + digest("e"),
			CatalogVersion:            "domain-pack-catalog-v1",
			CatalogDigest:             "sha256:" + digest("f"),
			AdvisoryOnly:              true,
			ExecutionAuthorityGranted: false,
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "plan-1", DecisionDigest: digest("1"), Feasibility: resourceplanner.Feasible,
			Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
		},
		RiskAssessment: RiskAssessment{
			Level: "high", ApprovalRequired: true, ApprovalGranted: true, AllowedNow: true,
		},
		ExecutionPlan: ExecutionPlan{ControlledExecutionMode: "approved-controlled-runtime"},
	}

	first, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("executionGovernanceEvidence: %v", err)
	}
	second, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("second executionGovernanceEvidence: %v", err)
	}
	if len(first.TaskPlanDigest) != 64 || first.TaskPlanDigest != second.TaskPlanDigest {
		t.Fatalf("task plan digest is not deterministic: %q / %q", first.TaskPlanDigest, second.TaskPlanDigest)
	}
	if first.FrameworkOperatingContractDigest != digest("d") ||
		first.FrameworkSelectorAlgorithmVersion != "selector-v5" ||
		first.FrameworkTaskRiskLevel != "high" ||
		first.FrameworkEffectiveRiskCeiling != "high" ||
		first.FrameworkMaximumAutonomyLevel == nil ||
		*first.FrameworkMaximumAutonomyLevel != 6 ||
		first.FrameworkRequiresApproval == nil ||
		!*first.FrameworkRequiresApproval ||
		first.DomainPackDecisionDigest != digest("e") ||
		first.ResourceDecisionDigest != digest("1") ||
		first.ResourceFeasibility != string(resourceplanner.Feasible) ||
		len(first.EvidenceReferences) != 4 {
		t.Fatalf("governance evidence = %#v", first)
	}
	plan.FrameworkDecision.TaskRiskLevel = "medium"
	plan.RiskAssessment.Level = "medium"
	riskChanged, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("risk-changed executionGovernanceEvidence: %v", err)
	}
	if riskChanged.TaskPlanDigest == first.TaskPlanDigest {
		t.Fatal("task plan digest did not bind the framework risk contract")
	}
	plan.FrameworkDecision.TaskRiskLevel = "high"
	plan.RiskAssessment.Level = "high"
	plan.FrameworkDecision.MaximumAutonomyLevel = 5
	autonomyChanged, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("autonomy-changed executionGovernanceEvidence: %v", err)
	}
	if autonomyChanged.TaskPlanDigest == first.TaskPlanDigest {
		t.Fatal("task plan digest did not bind the framework autonomy contract")
	}
	plan.FrameworkDecision.MaximumAutonomyLevel = 6

	plan.RiskAssessment.AllowedNow = false
	changed, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("changed executionGovernanceEvidence: %v", err)
	}
	if changed.TaskPlanDigest == first.TaskPlanDigest {
		t.Fatal("task plan digest did not bind the execution gate")
	}
}

func TestExecutionGovernanceEvidenceBindsAcceptedCoordinationRevisionWithoutGrantingAuthority(t *testing.T) {
	plan := validGovernancePlanForCoordinationTest()
	first, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("governance evidence: %v", err)
	}
	wantReference := "plan-graph://aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/revisions/2/nodes/task-node#sha256:" + strings.Repeat("a", 64)
	if !containsString(first.EvidenceReferences, wantReference) {
		t.Fatalf("coordination reference missing: %#v", first.EvidenceReferences)
	}
	plan.CoordinationPlan.Digest = strings.Repeat("b", 64)
	second, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("changed governance evidence: %v", err)
	}
	if first.TaskPlanDigest == second.TaskPlanDigest {
		t.Fatal("task governance digest did not bind coordination digest")
	}
	plan.CoordinationPlan.CanExecute = true
	if _, err := executionGovernanceEvidence(plan); err == nil {
		t.Fatal("governance evidence accepted executable coordination authority")
	}
}

func validGovernancePlanForCoordinationTest() *CompletionPlan {
	digest := strings.Repeat("a", 64)
	return &CompletionPlan{
		ID: "plan-1", OwnerIdentity: "alice", Request: "do safe work", ProjectKey: "project", RealGoal: "verified result",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-1", CatalogVersion: "framework-catalog-v2", SelectorAlgorithmVersion: "selector-v5",
			TaskRiskLevel: "low", EffectiveRiskCeiling: "low", MaximumAutonomyLevel: 8,
			CatalogDigest: digest, EffectivePreferenceDigest: digest, ConstitutionDigest: digest,
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "plan-1", DecisionDigest: digest, Feasibility: resourceplanner.Feasible,
			Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
		},
		RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true},
		ExecutionPlan:  ExecutionPlan{ControlledExecutionMode: "approved-controlled-runtime"},
		CoordinationPlan: &plangraph.AcceptedRevisionBinding{
			PlanID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), Revision: 2,
			Digest: digest, NodeID: "task-node", Node: plangraph.Node{ID: "task-node"}, CanExecute: false,
		},
	}
}

func TestExecutionGovernanceEvidenceRejectsInvalidV5RiskContract(t *testing.T) {
	digest := strings.Repeat("a", 64)
	basePlan := func() *CompletionPlan {
		return &CompletionPlan{
			ID: "plan-1",
			FrameworkDecision: &frameworkregistry.SelectionDecision{
				ID: "selection-1", CatalogVersion: "framework-catalog-v2",
				SelectorAlgorithmVersion: "selector-v5", TaskRiskLevel: "medium",
				EffectiveRiskCeiling: "high", CatalogDigest: digest,
				EffectivePreferenceDigest: digest, ConstitutionDigest: digest,
				OperatingContractDigest: digest,
			},
			ResourceDecision: &resourceplanner.Decision{
				PlanID: "plan-1", DecisionDigest: digest, Feasibility: resourceplanner.Feasible,
				Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
			},
			RiskAssessment: RiskAssessment{Level: "medium"},
		}
	}

	for name, mutate := range map[string]func(*CompletionPlan){
		"missing task risk": func(plan *CompletionPlan) {
			plan.FrameworkDecision.TaskRiskLevel = ""
		},
		"missing ceiling": func(plan *CompletionPlan) {
			plan.FrameworkDecision.EffectiveRiskCeiling = ""
		},
		"invalid task risk": func(plan *CompletionPlan) {
			plan.FrameworkDecision.TaskRiskLevel = "critical"
		},
		"task exceeds ceiling": func(plan *CompletionPlan) {
			plan.FrameworkDecision.TaskRiskLevel = "high"
			plan.FrameworkDecision.EffectiveRiskCeiling = "medium"
			plan.RiskAssessment.Level = "high"
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan := basePlan()
			mutate(plan)
			if _, err := executionGovernanceEvidence(plan); err == nil {
				t.Fatal("executionGovernanceEvidence accepted an invalid v5 risk contract")
			}
		})
	}
}

func TestExecutionGovernanceEvidencePreservesLegacyV4WithoutInferredRisk(t *testing.T) {
	digest := strings.Repeat("a", 64)
	plan := &CompletionPlan{
		ID: "plan-legacy",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-v4", CatalogVersion: "framework-catalog-v1",
			SelectorAlgorithmVersion: "selector-v4", CatalogDigest: digest,
			EffectivePreferenceDigest: digest, ConstitutionDigest: digest,
			OperatingContractDigest: digest,
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "plan-legacy", DecisionDigest: digest, Feasibility: resourceplanner.Feasible,
			Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
		},
		RiskAssessment: RiskAssessment{Level: "high"},
	}

	evidence, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("executionGovernanceEvidence: %v", err)
	}
	if evidence.FrameworkTaskRiskLevel != "" || evidence.FrameworkEffectiveRiskCeiling != "" {
		t.Fatalf("legacy risk evidence was inferred: %#v", evidence)
	}
}

func TestExecutionGovernanceEvidenceBindsPassingFrameworkPreflight(t *testing.T) {
	digest := strings.Repeat("a", 64)
	plan := &CompletionPlan{
		ID: "plan-preflight", OwnerIdentity: "alice",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-preflight", CatalogVersion: "framework-catalog-v2",
			SelectorAlgorithmVersion: "selector-v5", TaskRiskLevel: "low",
			EffectiveRiskCeiling: "high", MaximumAutonomyLevel: 4,
			CatalogDigest: digest, EffectivePreferenceDigest: digest,
			ConstitutionDigest: digest, OperatingContractDigest: digest,
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "plan-preflight", DecisionDigest: digest, Feasibility: resourceplanner.Feasible,
			Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
		},
		ValidationPlan: ValidationPlan{FrameworkEvidenceContracts: []FrameworkEvidenceContract{{
			ID: "fer-owner", FrameworkID: "human-sovereignty", Requirement: "verified operator identity",
			Phase: EvidencePhasePreAuthorization, Validator: "owner_identity", Required: true,
		}}},
		FrameworkEvidencePreflight: &FrameworkEvidencePreflightResult{
			Passed: true, Status: "passed", Digest: strings.Repeat("b", 64), Checked: 1, Verified: 1,
		},
		RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true},
	}

	evidence, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("executionGovernanceEvidence: %v", err)
	}
	if evidence.FrameworkEvidencePreflightDigest != strings.Repeat("b", 64) {
		t.Fatalf("preflight digest = %q", evidence.FrameworkEvidencePreflightDigest)
	}
	wantReference := "framework-evidence-preflight://sha256:" + strings.Repeat("b", 64)
	if !containsString(evidence.EvidenceReferences, wantReference) {
		t.Fatalf("preflight reference is missing: %#v", evidence.EvidenceReferences)
	}
	firstPlanDigest := evidence.TaskPlanDigest
	plan.FrameworkEvidencePreflight.Digest = strings.Repeat("c", 64)
	changed, err := executionGovernanceEvidence(plan)
	if err != nil {
		t.Fatalf("changed executionGovernanceEvidence: %v", err)
	}
	if changed.TaskPlanDigest == firstPlanDigest {
		t.Fatal("task plan digest did not bind the framework evidence preflight")
	}
}

func TestExecutionGovernanceEvidenceRejectsMissingOrFailedRequiredPreflight(t *testing.T) {
	digest := strings.Repeat("a", 64)
	plan := &CompletionPlan{
		ID: "plan-preflight",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-preflight", CatalogVersion: "framework-catalog-v2",
			SelectorAlgorithmVersion: "selector-v5", TaskRiskLevel: "low",
			EffectiveRiskCeiling: "high", MaximumAutonomyLevel: 4,
			CatalogDigest: digest, EffectivePreferenceDigest: digest,
			ConstitutionDigest: digest, OperatingContractDigest: digest,
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "plan-preflight", DecisionDigest: digest, Feasibility: resourceplanner.Feasible,
			Authority: "advisory_only", CanExecute: false, GrantsAuthority: false,
		},
		ValidationPlan: ValidationPlan{FrameworkEvidenceContracts: []FrameworkEvidenceContract{{
			ID: "fer-owner", FrameworkID: "human-sovereignty", Requirement: "verified operator identity",
			Phase: EvidencePhasePreAuthorization, Validator: "owner_identity", Required: true,
		}}},
		RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true},
	}
	if _, err := executionGovernanceEvidence(plan); err == nil {
		t.Fatal("missing required preflight was accepted")
	}
	plan.FrameworkEvidencePreflight = &FrameworkEvidencePreflightResult{
		Passed: false, Status: "blocked", Digest: strings.Repeat("b", 64), Missing: 1,
	}
	if _, err := executionGovernanceEvidence(plan); err == nil {
		t.Fatal("failed preflight was accepted")
	}
}

func TestExecutionGovernanceEvidenceRejectsAuthorityBearingDomainDecision(t *testing.T) {
	digest := strings.Repeat("a", 64)
	plan := &CompletionPlan{
		ID: "plan-1",
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-1", CatalogVersion: "v1", CatalogDigest: digest,
			EffectivePreferenceDigest: digest, ConstitutionDigest: digest,
			OperatingContractDigest: digest,
		},
		DomainPackDecision: &DomainPackDecision{
			ID: "domain-1", Digest: "sha256:" + digest, CatalogVersion: "v1",
			CatalogDigest: "sha256:" + digest, AdvisoryOnly: true,
			ExecutionAuthorityGranted: true,
		},
	}
	if _, err := executionGovernanceEvidence(plan); err == nil {
		t.Fatal("authority-bearing domain pack decision was accepted")
	}
}

func TestExecuteAllowedStepsRejectsInvalidGovernanceBeforeToolEffect(t *testing.T) {
	digest := strings.Repeat("a", 64)
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := &service{toolExecutor: executor}
	plan := &CompletionPlan{
		ID:            "plan-1",
		OwnerIdentity: "alice",
		RealGoal:      "Run a controlled task",
		Intake:        IntakeAnalysis{NeedsTools: true},
		ModelDecision: llm.RouteDecision{SelectedModelID: "governance-test-model"},
		RiskAssessment: RiskAssessment{
			AllowedNow: true,
		},
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID: "selection-1", CatalogVersion: "v1", CatalogDigest: digest,
			EffectivePreferenceDigest: digest, ConstitutionDigest: digest,
			OperatingContractDigest: digest,
		},
		DomainPackDecision: &DomainPackDecision{
			ID: "domain-1", Digest: "sha256:" + digest, CatalogVersion: "v1",
			CatalogDigest: "sha256:" + digest, AdvisoryOnly: false,
		},
	}

	result := service.executeAllowedSteps(plan, IntakeRequest{
		AutomationID:   "5ea57862-0116-45e4-bb8a-bb3c3d3c4617",
		ExecuteAllowed: true,
	})
	if executor.calls != 0 {
		t.Fatalf("tool effects = %d, want zero before governance validation", executor.calls)
	}
	if result == nil || !strings.Contains(result.BlockedReason, "governance evidence") {
		t.Fatalf("execution result = %#v, want governance block", result)
	}
}
