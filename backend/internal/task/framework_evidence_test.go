package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

func TestCompileFrameworkEvidenceContractsPreservesFrameworkAndDeterministicPhase(t *testing.T) {
	decision := &frameworkregistry.SelectionDecision{
		Selected: []frameworkregistry.SelectedFramework{
			{
				ID: "legal-evidence",
				EvidenceRequirements: []string{
					"primary case sources",
					"action receipt",
					"deterministic checks where possible",
				},
			},
		},
		EvidenceRequirements: []string{
			"primary case sources",
			"active Constitution",
		},
	}

	first := compileFrameworkEvidenceContracts(decision)
	second := compileFrameworkEvidenceContracts(decision)
	if len(first) != 4 || len(second) != len(first) {
		t.Fatalf("contract counts = %d and %d, want 4", len(first), len(second))
	}

	want := map[string]struct {
		frameworkID string
		phase       EvidencePhase
	}{
		"primary case sources":                {frameworkID: "legal-evidence", phase: EvidencePhasePreAuthorization},
		"action receipt":                      {frameworkID: "legal-evidence", phase: EvidencePhaseExecution},
		"deterministic checks where possible": {frameworkID: "legal-evidence", phase: EvidencePhasePostcondition},
		"active Constitution":                 {frameworkID: "selection-policy", phase: EvidencePhasePreAuthorization},
	}

	for i, contract := range first {
		expected, ok := want[contract.Requirement]
		if !ok {
			t.Fatalf("unexpected contract: %#v", contract)
		}
		if contract.FrameworkID != expected.frameworkID || contract.Phase != expected.phase {
			t.Errorf("contract %q = framework %q phase %q, want %q / %q", contract.Requirement, contract.FrameworkID, contract.Phase, expected.frameworkID, expected.phase)
		}
		wantID := frameworkEvidenceRequirementID(contract.FrameworkID, contract.Requirement)
		if contract.ID != wantID || second[i].ID != contract.ID {
			t.Errorf("contract %q id = %q / %q, want deterministic %q", contract.Requirement, contract.ID, second[i].ID, wantID)
		}
	}
}

func TestCompileFrameworkEvidenceContractsKeepsUnverifiableGuidanceAdvisory(t *testing.T) {
	decision := &frameworkregistry.SelectionDecision{
		Selected: []frameworkregistry.SelectedFramework{{
			ID: "workflow-design",
			EvidenceRequirements: []string{
				"active Constitution",
				"a qualitative operating principle with no machine validator",
			},
		}},
	}

	contracts := compileFrameworkEvidenceContracts(decision)
	if len(contracts) != 2 {
		t.Fatalf("contract count = %d, want 2", len(contracts))
	}
	byRequirement := map[string]FrameworkEvidenceContract{}
	for _, contract := range contracts {
		byRequirement[contract.Requirement] = contract
	}
	if !byRequirement["active Constitution"].Required {
		t.Fatal("active Constitution must remain an enforced pre-authorization requirement")
	}
	unsupported := byRequirement["a qualitative operating principle with no machine validator"]
	if unsupported.Validator != "explicit_evidence" {
		t.Fatalf("unsupported validator = %q, want explicit_evidence", unsupported.Validator)
	}
	if unsupported.Required {
		t.Fatal("unverifiable qualitative guidance must not block controlled execution")
	}
}

func TestReadinessProbeFrameworkRequirementsAreMachineVerifiable(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	decision, err := selector.PlanSelection(frameworkregistry.SelectionRequest{
		OwnerIdentity:       "alice",
		TaskPlanID:          "readiness-probe",
		Request:             "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		ProjectKey:          "018-hai",
		TaskType:            "administrative",
		RiskLevel:           "low",
		NeedsTools:          true,
		NeedsLocalExecution: true,
		ExecuteRequested:    true,
		HumanApproved:       true,
	})
	if err != nil {
		t.Fatalf("PlanSelection: %v", err)
	}

	for _, contract := range compileFrameworkEvidenceContracts(decision) {
		if contract.Required && contract.Phase == EvidencePhasePreAuthorization && contract.Validator == "explicit_evidence" {
			t.Errorf("unverifiable catalog requirement remained required: %s: %q", contract.FrameworkID, contract.Requirement)
		}
		t.Logf("%s: %q phase=%s validator=%s required=%t", contract.FrameworkID, contract.Requirement, contract.Phase, contract.Validator, contract.Required)
	}

	taskService := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		&sequencedVerificationService{},
		&fakeToolExecutor{result: completedToolResult()},
		nil,
		selector,
	)
	plan, err := taskService.Plan(IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		ProjectKey:    "018-hai",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan.RiskAssessment.ApprovalRequired = false
	plan.RiskAssessment.ApprovalGranted = false
	preflight := taskService.(*service).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{OwnerIdentity: "alice"})
	for _, failure := range preflight.Failures {
		t.Logf("preflight failure: %s", failure)
	}
	if !preflight.Passed {
		t.Fatalf("read-only readiness preflight without approval requirement = %#v", preflight)
	}

	approvedAt := time.Now().UTC().Add(-time.Minute)
	plan.RiskAssessment.ApprovalRequired = true
	plan.RiskAssessment.ApprovalGranted = true
	preflight = taskService.(*service).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{
		OwnerIdentity:         "alice",
		WorkflowID:            uuid.NewString(),
		AutomationID:          uuid.NewString(),
		HumanApproved:         true,
		ApprovalSourceID:      "workflow-decision:" + uuid.NewString(),
		ApprovalBindingDigest: strings.Repeat("d", 64),
		ApprovalActorIdentity: "alice",
		ApprovalApprovedAt:    &approvedAt,
	})
	if !preflight.Passed {
		t.Fatalf("readiness preflight with a valid workflow approval = %#v", preflight)
	}

	runPlan, err := taskService.Run(IntakeRequest{
		OwnerIdentity:         "alice",
		WorkflowID:            uuid.NewString(),
		Request:               "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		ProjectKey:            "018-hai",
		AutomationID:          uuid.NewString(),
		ExecuteAllowed:        true,
		HumanApproved:         true,
		ApprovalSourceID:      "workflow-decision:" + uuid.NewString(),
		ApprovalBindingDigest: strings.Repeat("d", 64),
		ApprovalActorIdentity: "alice",
		ApprovalApprovedAt:    &approvedAt,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runPlan.FrameworkEvidencePreflight == nil || !runPlan.FrameworkEvidencePreflight.Passed {
		t.Fatalf("real readiness run failed framework preflight: %#v", runPlan.FrameworkEvidencePreflight)
	}
}

func TestReadinessProbeExecutesWithoutConfiguredLLM(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	approvedAt := time.Now().UTC().Add(-time.Minute)
	executor := &modelPreflightExecutor{fakeToolExecutor: &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}, readOnly: true}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskNoProviderLLMService(t),
		nil,
		&sequencedVerificationService{},
		executor,
		nil,
		selector,
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:         "alice",
		WorkflowID:            uuid.NewString(),
		Request:               "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		ProjectKey:            "018-hai",
		AutomationID:          executor.result.AutomationID,
		ExecuteAllowed:        true,
		HumanApproved:         true,
		ApprovalSourceID:      "workflow-decision:" + uuid.NewString(),
		ApprovalBindingDigest: strings.Repeat("d", 64),
		ApprovalActorIdentity: "alice",
		ApprovalApprovedAt:    &approvedAt,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want one bounded read-only execution; plan=%#v", executor.calls, plan)
	}
	if plan.FrameworkEvidencePreflight == nil || !plan.FrameworkEvidencePreflight.Passed {
		t.Fatalf("model-free readiness preflight = %#v, want passed", plan.FrameworkEvidencePreflight)
	}
	if !plan.ValidationResult.Passed || plan.CompletionStatus != "validated" {
		t.Fatalf("model-free readiness execution was not validated: status=%s failures=%#v criteria=%#v", plan.CompletionStatus, plan.ValidationResult.Failures, plan.ValidationResult.Criteria)
	}
}

func TestLowRiskReadinessProbeExecutesWithoutApproval(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	executor := &modelPreflightExecutor{fakeToolExecutor: &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}, readOnly: true}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskNoProviderLLMService(t),
		nil,
		&sequencedVerificationService{},
		executor,
		nil,
		selector,
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		WorkflowID:     uuid.NewString(),
		Request:        "Run the selected HAI backend readiness probe-12345 for E2E governed pursuit 12345 (e2e-governed-12345) and record its read-only verification result. Do not send anything externally.",
		ProjectKey:     "e2e-governed-12345",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want one bounded read-only execution; risk=%#v plan=%#v", executor.calls, plan.RiskAssessment, plan)
	}
	if plan.RiskAssessment.ApprovalRequired || !plan.RiskAssessment.AllowedNow {
		t.Fatalf("low-risk read-only readiness probe was not eligible without approval: %#v", plan.RiskAssessment)
	}
	if plan.FrameworkEvidencePreflight == nil || !plan.FrameworkEvidencePreflight.Passed {
		t.Fatalf("unapproved readiness preflight = %#v, want passed", plan.FrameworkEvidencePreflight)
	}
	if !plan.ValidationResult.Passed || plan.CompletionStatus != "validated" {
		t.Fatalf("unapproved readiness execution was not validated: status=%s failures=%#v criteria=%#v", plan.CompletionStatus, plan.ValidationResult.Failures, plan.ValidationResult.Criteria)
	}
}

func TestUnknownOwnerCapacityCannotOverrideMissingRequiredParticipants(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	executor := &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}
	service := NewServiceWithDependenciesAndAgentContext(
		&fakeMemoryService{},
		newTaskNoProviderLLMService(t),
		nil,
		&sequencedVerificationService{},
		executor,
		nil,
		selector,
		NewMemoryTaskStateRepository(),
		&operatingContextProviderStub{capacity: &frameworkregistry.CapacitySnapshot{
			Status:      "unknown",
			NeedsReview: true,
			SourceLabel: "lifeops:no_observation",
		}},
		emptyAgentContextProvider{},
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "operator@example.test",
		WorkflowID:     uuid.NewString(),
		Request:        "Run the selected HAI backend readiness probe-12345 for E2E governed pursuit 12345 (e2e-governed-12345) and record its read-only verification result. Do not send anything externally.",
		ProjectKey:     "e2e-governed-12345",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Capacity advice cannot discharge missing independent review participants.
	// The lightweight constructor's positive deterministic-runtime test above
	// still covers execution separately; this production-like empty inventory
	// must not turn that path into a specialist-bypass exception.
	if executor.calls != 0 || plan.RiskAssessment.AllowedNow || len(plan.RiskAssessment.MissingRequiredAgents) == 0 {
		t.Fatalf("unknown capacity overrode missing participants: calls=%d risk=%#v", executor.calls, plan.RiskAssessment)
	}
	if plan.ValidationResult.Passed || plan.CompletionStatus == "validated" || plan.ReviewQueueItem == nil {
		t.Fatalf("blocked work was passed off as verified: status=%s validation=%t", plan.CompletionStatus, plan.ValidationResult.Passed)
	}
}

type emptyAgentContextProvider struct{}

func (emptyAgentContextProvider) LatestAgents(string, time.Time) ([]frameworkregistry.AgentCard, error) {
	return nil, nil
}

func TestE2EReadinessProbeSelectionAllowsAutomaticControlledRuntime(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	request := "Run the selected HAI backend readiness probe-12345 for E2E governed pursuit 12345 (e2e-governed-12345) and record its read-only verification result. Do not send anything externally."
	intake := analyzeIntake(IntakeRequest{Request: request, ProjectKey: "e2e-governed-12345"})

	decision, err := selector.PlanSelection(frameworkregistry.SelectionRequest{
		OwnerIdentity:       "operator@example.com",
		TaskPlanID:          "e2e-readiness-probe",
		Request:             request,
		ProjectKey:          "e2e-governed-12345",
		PursuitID:           "pursuit-12345",
		TaskType:            intake.TaskType,
		RiskLevel:           intake.RiskLevel,
		Difficulty:          intake.Difficulty,
		RequiredReasoning:   intake.RequiredReasoning,
		SuccessCriteria:     intake.SuccessCriteria,
		NeedsMemory:         intake.NeedsMemory,
		NeedsTools:          intake.NeedsTools,
		NeedsDocuments:      intake.NeedsDocuments,
		NeedsWebAccess:      intake.NeedsWebAccess,
		NeedsLocalExecution: intake.NeedsLocalExecution,
		NeedsApproval:       intake.NeedsApproval,
		ExecuteRequested:    true,
	})
	if err != nil {
		t.Fatalf("PlanSelection: %v", err)
	}
	if decision.MaximumAutonomyLevel < 8 || decision.RequiresApproval {
		t.Fatalf("E2E deterministic readiness selection blocks automatic runtime: intake=%#v autonomy=%d requiresApproval=%t selected=%#v", intake, decision.MaximumAutonomyLevel, decision.RequiresApproval, decision.Selected)
	}
}

func newTaskNoProviderLLMService(t *testing.T) *llm.Service {
	t.Helper()
	t.Setenv("LLM_PROVIDERS_JSON", "[]")
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	service, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	return service
}

func TestLegalPrimarySourceRequirementBlocksBeforeExecutorSideEffect(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := frameworkEvidenceRunService(t, executor, frameworkregistry.SelectionDecision{
		ID:                   "legal-selection",
		MaximumAutonomyLevel: 10,
		Selected: []frameworkregistry.SelectedFramework{{
			ID:                   "legal-evidence",
			Version:              "1.0.0",
			EvidenceRequirements: []string{"primary case sources"},
		}},
		EvidenceRequirements: []string{"primary case sources"},
		ConstitutionVersion:  1,
	})

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want zero before primary-source preflight passes", executor.calls)
	}
	if plan.FrameworkEvidencePreflight == nil || plan.FrameworkEvidencePreflight.Passed {
		t.Fatalf("primary-source preflight did not block: preflight=%#v risk=%#v", plan.FrameworkEvidencePreflight, plan.RiskAssessment)
	}
	if plan.ExecutionResult == nil || !strings.Contains(plan.ExecutionResult.BlockedReason, "primary case sources") {
		t.Fatalf("blocked execution lacks the exact legal evidence reason: %#v", plan.ExecutionResult)
	}
}

func TestCommunicationApprovalAndSourceRequirementsBlockBeforeExecutorSideEffect(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := frameworkEvidenceRunService(t, executor, frameworkregistry.SelectionDecision{
		ID:                   "communication-selection",
		MaximumAutonomyLevel: 10,
		RequiresApproval:     true,
		ApprovalReasons:      []string{"consequential communication requires approval"},
		Selected: []frameworkregistry.SelectedFramework{{
			ID:                   "communication",
			Version:              "1.0.0",
			EvidenceRequirements: []string{"approval for consequential send", "primary case sources"},
		}},
		EvidenceRequirements: []string{"approval for consequential send", "primary case sources"},
		ConstitutionVersion:  1,
	})

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests before preparing the external reply",
		ProjectKey:     "communications",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
		HumanApproved:  true,
		ApprovalNote:   "Approved in this request without a durable approval record.",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want zero before approval and source preflight pass", executor.calls)
	}
	if plan.FrameworkEvidencePreflight == nil || plan.FrameworkEvidencePreflight.Passed {
		t.Fatalf("communication preflight did not block: %#v", plan.FrameworkEvidencePreflight)
	}
	failures := strings.Join(plan.FrameworkEvidencePreflight.Failures, "\n")
	for _, requirement := range []string{"approval for consequential send", "primary case sources"} {
		if !strings.Contains(failures, requirement) {
			t.Errorf("preflight failures %q do not identify %q", failures, requirement)
		}
	}
}

func TestFrameworkEvidencePreflightSucceedsWithExactEvidence(t *testing.T) {
	now := time.Now().UTC()
	extraction := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "legal-case",
		ContentType: "document", Summary: "The signed case letter states the requested action.",
		SourceURI: "file:///case/signed-letter.pdf", ContentHash: strings.Repeat("a", 64), UpdatedAt: now,
	}
	plan := &CompletionPlan{
		ID:            "task-with-evidence",
		OwnerIdentity: "alice",
		ContextPlan: ContextPlan{SourceContext: []source.RankedExtraction{{
			Extraction: extraction,
		}}},
		ValidationPlan: ValidationPlan{FrameworkEvidenceContracts: []FrameworkEvidenceContract{
			{
				ID: frameworkEvidenceRequirementID("legal-evidence", "primary case sources"), FrameworkID: "legal-evidence",
				Requirement: "primary case sources", Phase: EvidencePhasePreAuthorization, Validator: "primary_source", Required: true,
			},
			{
				ID: frameworkEvidenceRequirementID("identity", "verified operator identity"), FrameworkID: "identity",
				Requirement: "verified operator identity", Phase: EvidencePhasePreAuthorization, Validator: "owner_identity", Required: true,
			},
		}},
	}

	result := (&service{sourceEvidence: &fakeFrameworkSourceEvidenceResolver{
		ownerIdentity: "alice",
		snapshots: map[string]sourceevidence.Snapshot{
			extraction.ID.String(): frameworkSourceEvidenceSnapshot("alice", extraction, now),
		},
	}}).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{OwnerIdentity: "alice"})
	if !result.Passed || result.Status != "passed" || result.Checked != 2 || result.Verified != 2 || result.Missing != 0 {
		t.Fatalf("exact evidence preflight = %#v, want two verified assertions", result)
	}
	for _, assertion := range result.Assertions {
		if assertion.Status != evidenceStatusVerified || len(assertion.Evidence) == 0 {
			t.Errorf("assertion lacks exact evidence: %#v", assertion)
		}
	}
}

func TestWorkflowApprovalAndEmptyContextSatisfyExactPreflightEvidence(t *testing.T) {
	approvedAt := time.Now().UTC().Add(-time.Minute)
	workflowID := uuid.NewString()
	plan := &CompletionPlan{
		ID:            "workflow-approved-plan",
		OwnerIdentity: "alice",
		RealGoal:      "Run the exact approved readiness probe.",
		ProjectKey:    "018-hai",
		ContextPlan:   ContextPlan{Explanation: "No relevant owner-scoped memory or source context was retrieved."},
		RiskAssessment: RiskAssessment{
			Level: "low", ApprovalRequired: true, ApprovalGranted: true,
			ApprovalSourceID: "workflow-decision:" + uuid.NewString(), ApprovalActorIdentity: "alice",
		},
		ValidationPlan: ValidationPlan{FrameworkEvidenceContracts: []FrameworkEvidenceContract{
			{ID: "approval", FrameworkID: "human-sovereignty", Requirement: "applicable approval record", Phase: EvidencePhasePreAuthorization, Validator: "approval_record", Required: true},
			{ID: "actor", FrameworkID: "approval-control", Requirement: "approver identity", Phase: EvidencePhasePreAuthorization, Validator: "approval_record", Required: true},
			{ID: "scope", FrameworkID: "approval-control", Requirement: "scope and expiry", Phase: EvidencePhasePreAuthorization, Validator: "approval_record", Required: true},
			{ID: "confidence", FrameworkID: "memory-architecture", Requirement: "confidence", Phase: EvidencePhasePreAuthorization, Validator: "confidence_record", Required: true},
		}},
	}
	request := IntakeRequest{
		OwnerIdentity: "alice", WorkflowID: workflowID, AutomationID: uuid.NewString(), HumanApproved: true,
		ApprovalSourceID: plan.RiskAssessment.ApprovalSourceID, ApprovalBindingDigest: strings.Repeat("d", 64),
		ApprovalActorIdentity: "alice", ApprovalApprovedAt: &approvedAt,
	}

	result := (&service{}).evaluateFrameworkEvidencePreflight(plan, request)
	if !result.Passed || result.Missing != 0 || result.Verified != 4 {
		t.Fatalf("workflow approval preflight = %#v, want four verified assertions", result)
	}

	stale := approvedAt.Add(-time.Hour)
	request.ApprovalApprovedAt = &stale
	result = (&service{}).evaluateFrameworkEvidencePreflight(plan, request)
	if result.Passed || result.Missing != 3 {
		t.Fatalf("stale workflow approval was accepted: %#v", result)
	}
}

func TestFrameworkEvidencePreflightRejectsTamperedStaleAndForeignSourceSnapshots(t *testing.T) {
	now := time.Now().UTC()
	base := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "legal-case",
		ContentType: "document", Summary: "Source-backed instruction", SourceURI: "file:///case/source.pdf",
		ContentHash: strings.Repeat("b", 64), UpdatedAt: now,
	}
	contract := FrameworkEvidenceContract{
		ID: frameworkEvidenceRequirementID("legal-evidence", "source authority and freshness"), FrameworkID: "legal-evidence",
		Requirement: "source authority and freshness", Phase: EvidencePhasePreAuthorization, Validator: "fresh_source", Required: true,
		MaxAgeSeconds: 3600,
	}

	tests := []struct {
		name     string
		planItem models.SourceExtraction
		resolver *fakeFrameworkSourceEvidenceResolver
	}{
		{
			name: "tampered task copy",
			planItem: func() models.SourceExtraction {
				value := base
				value.Summary = "Changed after retrieval"
				return value
			}(),
			resolver: &fakeFrameworkSourceEvidenceResolver{ownerIdentity: "alice", snapshots: map[string]sourceevidence.Snapshot{
				base.ID.String(): frameworkSourceEvidenceSnapshot("alice", base, now),
			}},
		},
		{
			name:     "stale durable source",
			planItem: base,
			resolver: &fakeFrameworkSourceEvidenceResolver{ownerIdentity: "alice", snapshots: map[string]sourceevidence.Snapshot{
				base.ID.String(): frameworkSourceEvidenceSnapshot("alice", base, now.Add(-2*time.Hour)),
			}},
		},
		{
			name:     "foreign owner",
			planItem: base,
			resolver: &fakeFrameworkSourceEvidenceResolver{ownerIdentity: "bob", snapshots: map[string]sourceevidence.Snapshot{
				base.ID.String(): frameworkSourceEvidenceSnapshot("bob", base, now),
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := &CompletionPlan{
				ID: "task-source-check", OwnerIdentity: "alice",
				ContextPlan:    ContextPlan{SourceContext: []source.RankedExtraction{{Extraction: test.planItem}}},
				ValidationPlan: ValidationPlan{FrameworkEvidenceContracts: []FrameworkEvidenceContract{contract}},
			}
			result := (&service{sourceEvidence: test.resolver}).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{OwnerIdentity: "alice"})
			if result.Passed || result.Missing != 1 {
				t.Fatalf("preflight = %#v, want missing source evidence", result)
			}
		})
	}
}

type fakeFrameworkSourceEvidenceResolver struct {
	ownerIdentity string
	snapshots     map[string]sourceevidence.Snapshot
	err           error
}

func (r *fakeFrameworkSourceEvidenceResolver) Resolve(_ context.Context, ownerIdentity string, extractionID string) (sourceevidence.Snapshot, error) {
	if r.err != nil {
		return sourceevidence.Snapshot{}, r.err
	}
	if ownerIdentity != r.ownerIdentity {
		return sourceevidence.Snapshot{}, sourceevidence.ErrNotFound
	}
	snapshot, ok := r.snapshots[extractionID]
	if !ok {
		return sourceevidence.Snapshot{}, errors.New("source evidence not found")
	}
	return snapshot, nil
}

func frameworkSourceEvidenceSnapshot(owner string, extraction models.SourceExtraction, observedAt time.Time) sourceevidence.Snapshot {
	snapshot := sourceevidence.Snapshot{
		OwnerIdentity: owner, ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
		RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey,
		RawProjectKey: extraction.ProjectKey,
		ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
		ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
		ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
		FetchedAt:               observedAt, ExtractionAt: extraction.UpdatedAt,
		ConnectorKey: "test-source",
	}
	snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
	return snapshot
}

func TestFrameworkEvidencePreflightDigestBindsOwnerPlanSelectionAndAssertions(t *testing.T) {
	evaluatedAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	plan := &CompletionPlan{
		ID: "plan-1", OwnerIdentity: "alice",
		FrameworkDecision: &frameworkregistry.SelectionDecision{ID: "selection-1"},
	}
	result := FrameworkEvidencePreflightResult{
		Passed: true, Status: "passed", Checked: 1, Verified: 1, EvaluatedAt: evaluatedAt,
		Assertions: []FrameworkEvidenceAssertion{{
			RequirementID: "fer-1", FrameworkID: "human-sovereignty",
			Requirement: "verified operator identity", Phase: EvidencePhasePreAuthorization,
			Validator: "owner_identity", Status: evidenceStatusVerified,
			Evidence: []string{"owner-identity:verified"},
		}},
	}
	first := frameworkEvidencePreflightDigest(plan, result)
	second := frameworkEvidencePreflightDigest(plan, result)
	if len(first) != 64 || first != second {
		t.Fatalf("preflight digest is not deterministic: %q / %q", first, second)
	}
	result.Assertions[0].Status = evidenceStatusMissing
	if changed := frameworkEvidencePreflightDigest(plan, result); changed == first {
		t.Fatal("preflight digest did not bind the assertion status")
	}
	result.Assertions[0].Status = evidenceStatusVerified
	plan.OwnerIdentity = "bob"
	if changed := frameworkEvidencePreflightDigest(plan, result); changed == first {
		t.Fatal("preflight digest did not bind the owner")
	}
}

func TestDeterministicChecksArePostconditionEvidence(t *testing.T) {
	contract := compileFrameworkEvidenceContracts(&frameworkregistry.SelectionDecision{
		Selected: []frameworkregistry.SelectedFramework{{
			ID:                   "verification",
			EvidenceRequirements: []string{"deterministic checks where possible"},
		}},
	})
	if len(contract) != 1 {
		t.Fatalf("contracts = %#v, want one", contract)
	}
	if contract[0].Phase != EvidencePhasePostcondition || contract[0].Validator != "verified_postcondition" {
		t.Fatalf("deterministic check contract = %#v, want postcondition validator", contract[0])
	}

	plan := validStructuredValidationPlan()
	plan.ValidationPlan.FrameworkEvidenceContracts = contract
	preflight := (&service{}).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{})
	if !preflight.Passed || preflight.Checked != 0 {
		t.Fatalf("postcondition was incorrectly enforced before execution: %#v", preflight)
	}
}

func TestActionReceiptRequiresRealCompletedLaunch(t *testing.T) {
	newPlan := func(launchEventID string) *CompletionPlan {
		plan := validStructuredValidationPlan()
		plan.Intake.NeedsTools = true
		plan.ValidationPlan.FrameworkEvidenceRequirements = []string{"action receipt"}
		plan.ValidationPlan.FrameworkEvidenceContracts = []FrameworkEvidenceContract{{
			ID: frameworkEvidenceRequirementID("controlled-execution", "action receipt"), FrameworkID: "controlled-execution",
			Requirement: "action receipt", Phase: EvidencePhaseExecution, Validator: "execution_receipt", Required: true,
		}}
		plan.ExecutionResult.ToolExecution = &ToolExecutionResult{
			AutomationID: uuid.NewString(), LaunchEventID: launchEventID, RuntimeType: "script", LaunchType: "script",
			Target: "verify-project.sh", Status: "completed", Message: "script completed", ExecutedAt: time.Now().UTC(),
		}
		return plan
	}

	t.Run("missing launch receipt", func(t *testing.T) {
		result := validatePlan(newPlan(""), 1)
		criterion := validationCriterionByText(result.Criteria, "action receipt")
		if criterion == nil || criterion.Status != validationCriterionFailed {
			t.Fatalf("action receipt without launch event was accepted: %#v", criterion)
		}
	})

	t.Run("completed launch receipt", func(t *testing.T) {
		result := validatePlan(newPlan(uuid.NewString()), 1)
		criterion := validationCriterionByText(result.Criteria, "action receipt")
		if criterion == nil || criterion.Status != validationCriterionPassed || len(criterion.Evidence) == 0 {
			t.Fatalf("completed launch receipt was not accepted: %#v", criterion)
		}
	})
}

func TestLegacyStringOnlyFrameworkEvidenceRetainsValidationBehavior(t *testing.T) {
	plan := validStructuredValidationPlan()
	if len(plan.ValidationPlan.FrameworkEvidenceContracts) != 0 {
		t.Fatalf("legacy plan unexpectedly has typed contracts: %#v", plan.ValidationPlan.FrameworkEvidenceContracts)
	}

	result := validatePlan(plan, 1)
	criterion := validationCriterionByText(result.Criteria, "source reference is retained")
	if !result.Passed || criterion == nil || criterion.Status != validationCriterionPassed || len(criterion.Evidence) == 0 {
		t.Fatalf("legacy string-only framework validation changed: result=%#v criterion=%#v", result, criterion)
	}
}

func TestVerifiedPreAuthorizationCriterionSurvivesSeparatePostconditionFailure(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ID = "typed-framework-plan"
	plan.FrameworkDecision.ConstitutionVersion = 3
	plan.FrameworkDecision.ConstitutionDigest = strings.Repeat("b", 64)
	plan.ValidationPlan.FrameworkEvidenceRequirements = []string{"active Constitution", "postcondition verification"}
	plan.ValidationPlan.FrameworkEvidenceContracts = []FrameworkEvidenceContract{
		{
			ID: frameworkEvidenceRequirementID("constitution", "active Constitution"), FrameworkID: "constitution",
			Requirement: "active Constitution", Phase: EvidencePhasePreAuthorization, Validator: "constitution_record", Required: true,
		},
		{
			ID: frameworkEvidenceRequirementID("controlled-execution", "postcondition verification"), FrameworkID: "controlled-execution",
			Requirement: "postcondition verification", Phase: EvidencePhasePostcondition, Validator: "verified_postcondition", Required: true,
		},
	}
	preflight := (&service{}).evaluateFrameworkEvidencePreflight(plan, IntakeRequest{})
	if !preflight.Passed || preflight.Verified != 1 {
		t.Fatalf("constitution preflight was not verified: %#v", preflight)
	}
	plan.FrameworkEvidencePreflight = &preflight
	plan.Intake.NeedsTools = true
	plan.ExecutionResult.ToolExecution = completedToolResult()
	plan.ExecutionResult.VerificationStatus = verification.StatusUnsupported
	plan.ExecutionResult.UnsupportedClaims = 1

	result := validatePlan(plan, 1)
	if result.Passed {
		t.Fatalf("separate failed postcondition did not fail overall validation: %#v", result)
	}
	preAuthorization := validationCriterionByText(result.Criteria, "active Constitution")
	if preAuthorization == nil || preAuthorization.Status != validationCriterionPassed || len(preAuthorization.Evidence) == 0 {
		t.Fatalf("verified pre-authorization criterion was invalidated by a postcondition: %#v", preAuthorization)
	}
	postcondition := validationCriterionByText(result.Criteria, "postcondition verification")
	if postcondition == nil || postcondition.Status != validationCriterionFailed {
		t.Fatalf("failed postcondition was not isolated: %#v", postcondition)
	}
}

func frameworkEvidenceRunService(
	t *testing.T,
	executor *fakeToolExecutor,
	decision frameworkregistry.SelectionDecision,
) Service {
	t.Helper()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	selector := &fakeFrameworkSelector{decision: &decision}
	return NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		&sequencedVerificationService{},
		executor,
		nil,
		selector,
	)
}
