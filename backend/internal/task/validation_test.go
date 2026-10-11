package task

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

func TestValidatePlanAllowsModelFreeDeterministicReadOnlyRuntime(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ModelDecision.SelectedModelID = ""
	plan.ModelDecision.Reason = "no local or free model is configured"
	plan.Intake.NeedsTools = true
	plan.ExecutionResult.ToolExecution = deterministicReadOnlyToolExecution()

	result := validatePlan(plan, 1)
	if !result.Passed {
		t.Fatalf("deterministic read-only runtime was rejected without a model: %#v", result)
	}
	criterion := validationCriterionByText(result.Criteria, "a capable model was selected")
	if criterion == nil || criterion.Status != validationCriterionPassed ||
		!containsString(criterion.Evidence, "model:not-required:deterministic-read-only-runtime") {
		t.Fatalf("model-free deterministic evidence missing: %#v", criterion)
	}
}

func TestValidatePlanMapsVerifiedRuntimeApprovalProvenance(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.OwnerIdentity = "alice"
	plan.RiskAssessment = RiskAssessment{
		Level:                 "low",
		ApprovalRequired:      true,
		ApprovalGranted:       true,
		ApprovalSourceID:      "workflow-decision:" + uuid.NewString(),
		ApprovalActorIdentity: "alice",
	}
	plan.Intake.NeedsTools = true
	plan.ExecutionResult.ToolExecution = deterministicReadOnlyToolExecution()
	plan.ValidationPlan.FrameworkEvidenceRequirements = []string{
		"applicable approval record",
		"approver identity",
		"exact proposed action",
		"risk and consequences",
		"scope and expiry",
		"standing mandate or case approval",
	}

	result := validatePlan(plan, 1)
	if !result.Passed {
		t.Fatalf("verified runtime approval provenance was not accepted: %#v", result)
	}
	for _, expected := range plan.ValidationPlan.FrameworkEvidenceRequirements {
		criterion := validationCriterionByText(result.Criteria, expected)
		if criterion == nil || criterion.Status != validationCriterionPassed || len(criterion.Evidence) == 0 {
			t.Fatalf("approval criterion %q lacks exact evidence: %#v", expected, criterion)
		}
	}
}

func TestDeterministicReadOnlyRuntimeRejectsMutableOrUnevidencedExecution(t *testing.T) {
	valid := deterministicReadOnlyToolExecution()
	if !deterministicReadOnlyRuntimeCompleted(valid) {
		t.Fatal("valid read-only runtime evidence was rejected")
	}

	mutable := *valid
	mutable.Message = "POST http://backend/api/items returned HTTP 200"
	if deterministicReadOnlyRuntimeCompleted(&mutable) {
		t.Fatal("mutable API execution entered the deterministic read-only path")
	}

	missingReceipt := *valid
	missingReceipt.AuditEvents = []string{"api request executed", "response captured with bounded output"}
	if deterministicReadOnlyRuntimeCompleted(&missingReceipt) {
		t.Fatal("runtime execution without an authorization receipt was accepted")
	}
}

func deterministicReadOnlyToolExecution() *ToolExecutionResult {
	return &ToolExecutionResult{
		AutomationID:  uuid.NewString(),
		LaunchEventID: uuid.NewString(),
		RuntimeType:   "api",
		LaunchType:    "api",
		Target:        "http://backend/readyz",
		Status:        "completed",
		Message:       "GET http://backend/readyz returned HTTP 200",
		ExitCode:      http.StatusOK,
		ExecutedAt:    time.Now().UTC(),
		AuditEvents: []string{
			"unified execution authorization receipt receipt-1 consumed",
			"api request executed",
			"response captured with bounded output",
		},
	}
}

func TestInitialValidationResultDoesNotClaimChecksRan(t *testing.T) {
	result := initialValidationResult(ValidationPlan{
		Steps:                         []string{"check output"},
		SuccessCriteria:               []string{"deliverable exists"},
		FrameworkEvidenceRequirements: []string{"source reference is retained"},
		FrameworkCompletionCriteria:   []string{"review gate is satisfied"},
	})

	if result.Status != "not_run" || result.Passed {
		t.Fatalf("unexpected initial result: %#v", result)
	}
	if len(result.Checked) != 0 {
		t.Fatalf("initial validation claimed checks ran: %#v", result.Checked)
	}
	if len(result.Criteria) != 3 {
		t.Fatalf("criteria count = %d, want 3", len(result.Criteria))
	}
	for _, criterion := range result.Criteria {
		if criterion.Status != validationCriterionNotRun || len(criterion.Evidence) != 0 {
			t.Fatalf("initial criterion claimed evaluation: %#v", criterion)
		}
	}
}

func TestValidatePlanRecordsCriterionEvidenceAfterVerifiedExecution(t *testing.T) {
	plan := validStructuredValidationPlan()

	result := validatePlan(plan, 1)

	if !result.Passed {
		t.Fatalf("expected validation to pass: %#v", result)
	}
	for _, kind := range []string{"task_success", "framework_evidence", "framework_completion"} {
		criterion := validationCriterionByKind(result.Criteria, kind)
		if criterion == nil {
			t.Fatalf("missing %s criterion: %#v", kind, result.Criteria)
		}
		if criterion.Status != validationCriterionPassed || len(criterion.Evidence) == 0 {
			t.Fatalf("%s criterion lacks evaluated evidence: %#v", kind, criterion)
		}
	}
	if !containsString(result.Checked, "framework_evidence: source reference is retained") {
		t.Fatalf("checked list does not identify evaluated framework evidence: %#v", result.Checked)
	}
}

func TestValidatePlanRejectsUnevidencedFrameworkRequirement(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ExecutionResult.EvidenceCount = 0

	result := validatePlan(plan, 1)

	if result.Passed {
		t.Fatalf("framework evidence requirement passed without evidence: %#v", result)
	}
	criterion := validationCriterionByKind(result.Criteria, "framework_evidence")
	if criterion == nil || criterion.Status != validationCriterionFailed {
		t.Fatalf("framework evidence criterion was not failed: %#v", result.Criteria)
	}
	if !strings.Contains(criterion.Failure, "no source or runtime evidence") {
		t.Fatalf("unexpected criterion failure: %#v", criterion)
	}
}

func TestFrameworkEvaluationMethodsRemainVisibleWithoutBecomingPerTaskGates(t *testing.T) {
	plan := applyFrameworkValidation(
		ValidationPlan{
			SuccessCriteria: []string{"deliverable exists"},
		},
		&frameworkregistry.SelectionDecision{
			Selected: []frameworkregistry.SelectedFramework{{
				ID:               "security-zero-trust",
				Version:          "1.0.0",
				EvaluationMethod: []string{"known attack paths are tested"},
			}},
			EvidenceRequirements: []string{"active Constitution"},
			CompletionCriteria: []string{
				"deliverable exists",
				"known attack paths are tested",
				"the verified result meets the explicit success criteria before completion is recorded",
			},
		},
	)

	if containsString(plan.FrameworkCompletionCriteria, "known attack paths are tested") {
		t.Fatalf("framework-level assurance was promoted to a task gate: %#v", plan)
	}
	if !containsString(plan.FrameworkAssuranceCriteria, "known attack paths are tested") {
		t.Fatalf("framework-level assurance disappeared from the plan: %#v", plan)
	}
	if !containsString(plan.FrameworkCompletionCriteria, "the verified result meets the explicit success criteria before completion is recorded") {
		t.Fatalf("universal task completion gate was removed: %#v", plan)
	}
}

func TestValidatePlanFailsClosedWhenApprovalBoundIdentityEvidenceIsMissing(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.FrameworkEvidenceRequirements = []string{"verified operator identity"}
	plan.RiskAssessment = RiskAssessment{
		Level:            "high",
		ApprovalRequired: true,
		ApprovalGranted:  true,
	}

	result := validatePlan(plan, 1)

	if result.Passed {
		t.Fatalf("approval-bound task passed without verified operator identity: %#v", result)
	}
	criterion := validationCriterionByText(result.Criteria, "verified operator identity")
	if criterion == nil ||
		criterion.Status != validationCriterionFailed ||
		criterion.ApplicabilityReason != "" {
		t.Fatalf("identity requirement was incorrectly waived: %#v", criterion)
	}
}

func TestValidatePlanRejectsUnrelatedAggregateEvidenceForCriterion(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.SuccessCriteria = []string{"invoice total equals EUR 125"}
	plan.Intake.SuccessCriteria = append([]string{}, plan.ValidationPlan.SuccessCriteria...)
	plan.ExecutionResult.EvidenceCount = 9
	plan.ExecutionResult.Claims = []models.VerificationClaim{{
		ID:                 uuid.New(),
		ClaimText:          "The client email was sent.",
		Status:             verification.StatusSourceSupported,
		SourceRefs:         "email://sent-message",
		SupportExplanation: "The sent-message record supports the email claim.",
	}}

	result := validatePlan(plan, 1)

	if result.Passed {
		t.Fatalf("unrelated aggregate evidence satisfied invoice criterion: %#v", result)
	}
	criterion := validationCriterionByKind(result.Criteria, "task_success")
	if criterion == nil || criterion.Status != validationCriterionFailed {
		t.Fatalf("invoice criterion was not failed: %#v", result.Criteria)
	}
	if len(criterion.Evidence) != 0 ||
		!strings.Contains(criterion.Failure, "no directly related verified evidence") {
		t.Fatalf("unexpected unsupported criterion result: %#v", criterion)
	}
}

func TestValidatePlanMapsOnlyDirectlyRelatedClaimEvidence(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.SuccessCriteria = []string{
		"invoice total equals EUR 125",
		"client email is sent",
	}
	plan.Intake.SuccessCriteria = append([]string{}, plan.ValidationPlan.SuccessCriteria...)
	invoiceClaimID := uuid.New()
	plan.ExecutionResult.Claims = []models.VerificationClaim{{
		ID:                 invoiceClaimID,
		ClaimText:          "The invoice total equals EUR 125.",
		Status:             verification.StatusSourceSupported,
		SourceRefs:         "invoice://125",
		SupportExplanation: "The invoice record supports the calculated total.",
	}}

	result := validatePlan(plan, 1)

	invoice := validationCriterionByText(result.Criteria, "invoice total equals EUR 125")
	if invoice == nil || invoice.Status != validationCriterionPassed ||
		!containsString(invoice.Evidence, "claim:"+invoiceClaimID.String()) {
		t.Fatalf("invoice criterion lacks its direct claim: %#v", invoice)
	}
	email := validationCriterionByText(result.Criteria, "client email is sent")
	if email == nil || email.Status != validationCriterionFailed || len(email.Evidence) != 0 {
		t.Fatalf("unrelated email criterion reused invoice evidence: %#v", email)
	}
}

func TestCriterionEvidenceMatchesRejectsConflictingProtectedValues(t *testing.T) {
	tests := []struct {
		name        string
		criterion   string
		description string
	}{
		{
			name:        "currency amount",
			criterion:   "invoice total equals EUR 100",
			description: "invoice total equals EUR 125",
		},
		{
			name:        "date",
			criterion:   "release is published on 2026-07-30",
			description: "release is published on 2026-07-31",
		},
		{
			name:        "version",
			criterion:   "release version 1.2.3 is deployed",
			description: "release version 1.2.4 is deployed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if criterionEvidenceMatches(test.criterion, test.description) {
				t.Fatalf("conflicting values matched: criterion=%q description=%q",
					test.criterion, test.description)
			}
		})
	}
}

func TestValidatePlanRejectsNegatedClaimForPositiveCriterion(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.SuccessCriteria = []string{"client email is sent"}
	plan.Intake.SuccessCriteria = append([]string{}, plan.ValidationPlan.SuccessCriteria...)
	plan.ExecutionResult.Claims = []models.VerificationClaim{{
		ID:                 uuid.New(),
		ClaimText:          "The client email is not sent.",
		Status:             verification.StatusSourceSupported,
		SourceRefs:         "email://draft",
		SupportExplanation: "The draft record shows the email is not sent.",
	}}

	result := validatePlan(plan, 1)

	criterion := validationCriterionByText(result.Criteria, "client email is sent")
	if criterion == nil || criterion.Status != validationCriterionFailed ||
		len(criterion.Evidence) != 0 {
		t.Fatalf("negated claim satisfied positive criterion: %#v", criterion)
	}
}

func TestValidatePlanRejectsGenericProvenanceForUnrelatedRetention(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.SuccessCriteria = []string{"audit package is retained as evidence"}
	plan.Intake.SuccessCriteria = append([]string{}, plan.ValidationPlan.SuccessCriteria...)
	plan.ExecutionResult.Claims = []models.VerificationClaim{{
		ID:                 uuid.New(),
		ClaimText:          "The invoice total equals EUR 125.",
		Status:             verification.StatusSourceSupported,
		SourceRefs:         "invoice://125",
		SupportExplanation: "The invoice record supports the calculated total.",
	}}

	result := validatePlan(plan, 1)

	criterion := validationCriterionByText(result.Criteria, "audit package is retained as evidence")
	if criterion == nil || criterion.Status != validationCriterionFailed ||
		len(criterion.Evidence) != 0 {
		t.Fatalf("generic provenance satisfied unrelated retention criterion: %#v", criterion)
	}
}

func TestValidatePlanAcceptsMatchingValueAndPolarityEvidence(t *testing.T) {
	plan := validStructuredValidationPlan()
	plan.ValidationPlan.SuccessCriteria = []string{"invoice total equals EUR 125"}
	plan.Intake.SuccessCriteria = append([]string{}, plan.ValidationPlan.SuccessCriteria...)
	claimID := uuid.New()
	plan.ExecutionResult.Claims = []models.VerificationClaim{{
		ID:                 claimID,
		ClaimText:          "The invoice total equals EUR 125.",
		Status:             verification.StatusSourceSupported,
		SourceRefs:         "invoice://125",
		SupportExplanation: "The invoice record supports the calculated total.",
	}}

	result := validatePlan(plan, 1)

	criterion := validationCriterionByText(result.Criteria, "invoice total equals EUR 125")
	if criterion == nil || criterion.Status != validationCriterionPassed ||
		!containsString(criterion.Evidence, "claim:"+claimID.String()) {
		t.Fatalf("legitimate matching evidence was rejected: %#v", criterion)
	}
}

func TestRequiredFrameworkAutonomyMatchesConstitutionAuthorityLadder(t *testing.T) {
	intake := IntakeAnalysis{NeedsTools: true, NeedsLocalExecution: true}
	if got := requiredFrameworkAutonomy(intake, IntakeRequest{}); got != 4 {
		t.Fatalf("planning autonomy = %d, want level 4", got)
	}
	if got := requiredFrameworkAutonomy(intake, IntakeRequest{ExecuteAllowed: true}); got != 8 {
		t.Fatalf("automatic reversible execution autonomy = %d, want level 8", got)
	}
	if got := requiredFrameworkAutonomy(intake, IntakeRequest{
		ExecuteAllowed: true,
		HumanApproved:  true,
	}); got != 6 {
		t.Fatalf("case-approved execution autonomy = %d, want level 6", got)
	}
}

func TestPlanDoesNotPremarkExecutionValidationOrMemory(t *testing.T) {
	service := NewService(&fakeMemoryService{}, newTaskTestLLMService(t))
	plan, err := service.Plan(IntakeRequest{
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   uuid.NewString(),
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	for _, id := range []string{"execute", "verify", "memory"} {
		status := taskStepStatus(plan, id)
		if status != "planned" {
			t.Fatalf("%s status = %q, want planned before execution", id, status)
		}
	}
	for _, id := range []string{"understand", "criteria", "framework", "context", "routing", "plan", "risk"} {
		if status := taskStepStatus(plan, id); status != "completed" {
			t.Fatalf("%s status = %q, want completed planning work", id, status)
		}
	}
}

func TestVerificationQuestionIncludesAllCompletionGates(t *testing.T) {
	plan := validStructuredValidationPlan()

	question := verificationQuestion(plan)

	for _, expected := range []string{
		"deliverable exists",
		"source reference is retained",
		"review gate is satisfied",
	} {
		if !strings.Contains(question, expected) {
			t.Fatalf("verification question omits %q: %q", expected, question)
		}
	}
}

func validStructuredValidationPlan() *CompletionPlan {
	return &CompletionPlan{
		RealGoal: "produce a source-grounded deliverable",
		Intake: IntakeAnalysis{
			SuccessCriteria: []string{"deliverable exists"},
		},
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			Selected: []frameworkregistry.SelectedFramework{{
				ID:      "test-framework",
				Version: "1.0.0",
				Name:    "Test framework",
			}},
		},
		ResourceDecision: &resourceplanner.Decision{
			PlanID: "test-plan", DecisionDigest: strings.Repeat("a", 64),
			Feasibility: resourceplanner.Feasible, Authority: "advisory_only",
		},
		ModelDecision: llm.RouteDecision{
			SelectedModelID: "local-test-model",
		},
		ToolDecision: ToolRouteDecision{
			SelectedTools: []string{"validator.criteria"},
		},
		RiskAssessment: RiskAssessment{
			ApprovalRequired: false,
		},
		ValidationPlan: ValidationPlan{
			Steps:                         []string{"check explicit criteria"},
			SuccessCriteria:               []string{"deliverable exists"},
			FrameworkEvidenceRequirements: []string{"source reference is retained"},
			FrameworkCompletionCriteria:   []string{"review gate is satisfied"},
		},
		ExecutionResult: &ExecutionResult{
			Output:             "The source-grounded deliverable is ready.",
			VerificationStatus: verification.StatusSourceSupported,
			EvidenceCount:      1,
			UnsupportedClaims:  0,
			Claims: []models.VerificationClaim{{
				ID:         uuid.New(),
				ClaimText:  "The deliverable is source-grounded.",
				Status:     verification.StatusSourceSupported,
				SourceRefs: "memory://test-source",
			}},
		},
	}
}

func validationCriterionByKind(criteria []ValidationCriterionResult, kind string) *ValidationCriterionResult {
	for i := range criteria {
		if criteria[i].Kind == kind {
			return &criteria[i]
		}
	}
	return nil
}

func validationCriterionByText(criteria []ValidationCriterionResult, text string) *ValidationCriterionResult {
	for i := range criteria {
		if criteria[i].Criterion == text {
			return &criteria[i]
		}
	}
	return nil
}

func taskStepStatus(plan *CompletionPlan, id string) string {
	for _, step := range plan.Steps {
		if step.ID == id {
			return step.Status
		}
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
