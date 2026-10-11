package workflowtask

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

type fakeWorkflowTaskRunner struct {
	seen workflow.TaskRunRequest
}

type capturingTaskService struct {
	request        task.IntakeRequest
	previewRequest task.IntakeRequest
	plan           *task.CompletionPlan
	previewPlan    *task.CompletionPlan
	err            error
	previewErr     error
	runs           int
	previews       int
}

type capturingApprovalBindingPreparer struct {
	automationID uuid.UUID
	request      automation.TaskLaunchRequest
	binding      string
	err          error
}

type catalogApprovalBindingPreparer struct {
	capturingApprovalBindingPreparer
	automations []*models.Automation
	catalogErr  error
}

func (p *catalogApprovalBindingPreparer) FindAll() ([]*models.Automation, error) {
	return p.automations, p.catalogErr
}

func (p *capturingApprovalBindingPreparer) PrepareWorkflowApprovalBinding(id uuid.UUID, request automation.TaskLaunchRequest) (string, error) {
	p.automationID = id
	p.request = request
	return p.binding, p.err
}

func (s *capturingTaskService) Plan(task.IntakeRequest) (*task.CompletionPlan, error) {
	return nil, nil
}

func (s *capturingTaskService) Preview(request task.IntakeRequest) (*task.CompletionPlan, error) {
	s.previews++
	s.previewRequest = request
	if s.previewErr != nil {
		return nil, s.previewErr
	}
	if s.previewPlan != nil {
		return s.previewPlan, nil
	}
	if s.plan != nil {
		return s.plan, nil
	}
	return validWorkflowCompletionPlan("task-plan-1"), nil
}

func (s *capturingTaskService) Run(request task.IntakeRequest) (*task.CompletionPlan, error) {
	s.runs++
	s.request = request
	if s.plan != nil {
		return s.plan, s.err
	}
	if s.err != nil {
		return nil, s.err
	}
	return validWorkflowCompletionPlan("task-plan-1"), nil
}

func (s *capturingTaskService) Logs() []task.CompletionPlan { return nil }

func (s *capturingTaskService) ReviewQueue() []task.ReviewQueueItem { return nil }

func (s *capturingTaskService) ResolveReviewItem(string, task.ApprovalDecision) (*task.ReviewResolutionResult, error) {
	return nil, nil
}

func (r *fakeWorkflowTaskRunner) RunWorkflowTask(request workflow.TaskRunRequest) (*workflow.TaskRunResult, error) {
	r.seen = request
	return &workflow.TaskRunResult{
		PlanID:             "plan-1",
		CompletionStatus:   "completed",
		VerificationStatus: "verified",
		Passed:             true,
	}, nil
}

func TestDeferredRunnerRequiresInitializedDelegate(t *testing.T) {
	runner := NewDeferredRunner()

	if _, err := runner.RunWorkflowTask(workflow.TaskRunRequest{Request: "advance workflow"}); err == nil {
		t.Fatalf("expected error before delegate is initialized")
	}
}

func TestDeferredRunnerDelegatesAfterInitialization(t *testing.T) {
	runner := NewDeferredRunner()
	delegate := &fakeWorkflowTaskRunner{}
	runner.Set(delegate)

	result, err := runner.RunWorkflowTask(workflow.TaskRunRequest{
		WorkflowID: "workflow-1",
		Request:    "advance workflow",
		ProjectKey: "018-hai",
	})
	if err != nil {
		t.Fatalf("RunWorkflowTask returned error: %v", err)
	}
	if result == nil || !result.Passed || result.VerificationStatus != "verified" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if delegate.seen.WorkflowID != "workflow-1" || delegate.seen.ProjectKey != "018-hai" {
		t.Fatalf("request was not delegated intact: %#v", delegate.seen)
	}
}

func TestRunnerPreparesBindingForExactTaskEngineExecutionIdentity(t *testing.T) {
	tasks := &capturingTaskService{}
	preparer := &capturingApprovalBindingPreparer{
		binding: "automation-action:script:" + strings.Repeat("d", 64),
	}
	runner := NewRunner(tasks, preparer)
	workflowID := uuid.NewString()
	automationID := uuid.New()
	mandateID := uuid.NewString()
	request := workflow.WorkflowApprovalBindingRequest{
		OwnerIdentity: " alice ",
		WorkflowID:    workflowID,
		AutomationID:  automationID.String(),
		MandateID:     mandateID,
		Request:       " Email the lawyer with the approved evidence summary. ",
		ProjectKey:    " 018-hai ",
	}

	binding, err := runner.PrepareWorkflowApprovalBinding(request)
	if err != nil {
		t.Fatalf("PrepareWorkflowApprovalBinding: %v", err)
	}
	if binding != preparer.binding || preparer.automationID != automationID {
		t.Fatalf("binding = %q automation = %s", binding, preparer.automationID)
	}
	expectedTask := task.ExecutionTask(task.IntakeRequest{
		OwnerIdentity:  "alice",
		WorkflowID:     workflowID,
		Request:        "Email the lawyer with the approved evidence summary.",
		ProjectKey:     "018-hai",
		AutomationID:   automationID.String(),
		MandateID:      mandateID,
		ExecuteAllowed: true,
		HumanApproved:  true,
	})
	if preparer.request.OwnerIdentity != "alice" ||
		preparer.request.ProjectKey != "018-hai" ||
		preparer.request.MandateID != mandateID ||
		preparer.request.Task != expectedTask {
		t.Fatalf("prepared exact action request = %#v, expected task %q", preparer.request, expectedTask)
	}
}

func TestDeferredRunnerDelegatesApprovalBindingPreparation(t *testing.T) {
	preparer := &capturingApprovalBindingPreparer{
		binding: "automation-action:script:" + strings.Repeat("e", 64),
	}
	delegate := NewRunner(&capturingTaskService{}, preparer)
	runner := NewDeferredRunner()
	runner.Set(delegate)

	binding, err := runner.PrepareWorkflowApprovalBinding(workflow.WorkflowApprovalBindingRequest{
		OwnerIdentity: "alice",
		WorkflowID:    uuid.NewString(),
		AutomationID:  uuid.NewString(),
		Request:       "Run the approved script.",
	})
	if err != nil {
		t.Fatalf("PrepareWorkflowApprovalBinding: %v", err)
	}
	if binding != preparer.binding {
		t.Fatalf("binding = %q, want %q", binding, preparer.binding)
	}
}

func TestRunnerSelectsUniqueConfiguredAutomation(t *testing.T) {
	selectedID := uuid.New()
	catalog := &catalogApprovalBindingPreparer{automations: []*models.Automation{
		{
			ID:           uuid.New(),
			Name:         "Broken email drafter",
			LaunchType:   "agent_runtime",
			LaunchTarget: "runtime://broken-email",
			RuntimeType:  "hermes",
			Status:       "broken",
		},
		{
			ID:           selectedID,
			Name:         "Email reply drafter",
			LaunchType:   "agent_runtime",
			LaunchTarget: "runtime://email-drafter",
			RuntimeType:  "hermes",
			Status:       "healthy",
			DependencyNotes: "Draft email replies from source evidence; never send " +
				"without separate approval.",
		},
		{
			ID:          uuid.New(),
			Name:        "Unconfigured email helper",
			LaunchType:  "agent_runtime",
			RuntimeType: "hermes",
		},
	}}
	runner := NewRunner(&capturingTaskService{}, catalog)

	candidates, err := runner.SelectWorkflowAutomations(workflow.AutomationSelectionRequest{
		OwnerIdentity: "alice",
		TaskType:      "email_reply",
		Request:       "Draft an email reply using the attached evidence. Do not send it.",
		ProjectKey:    "vivare-case",
	})
	if err != nil {
		t.Fatalf("SelectWorkflowAutomations: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v, want one conservative match", candidates)
	}
	selected := candidates[0]
	if selected.ID != selectedID.String() || selected.Name != "Email reply drafter" {
		t.Fatalf("selected candidate = %#v", selected)
	}
	if selected.Score <= 0 || !strings.Contains(selected.Reason, "task capability match") ||
		!strings.Contains(selected.Reason, "health status healthy") {
		t.Fatalf("selection explanation = %#v", selected)
	}
}

func TestRunnerReturnsRankedAmbiguousAutomationsWithoutChoosing(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	catalog := &catalogApprovalBindingPreparer{automations: []*models.Automation{
		{
			ID:              secondID,
			Name:            "General email runtime",
			LaunchType:      "api",
			LaunchTarget:    "POST http://localhost:7777/draft-email",
			Status:          "warning",
			DependencyNotes: "Draft email replies",
		},
		{
			ID:              firstID,
			Name:            "Vivare email drafter",
			LaunchType:      "agent_runtime",
			LaunchTarget:    "runtime://vivare-email",
			RuntimeType:     "hermes",
			Status:          "healthy",
			DependencyNotes: "Draft evidence-grounded email replies for the Vivare case",
		},
	}}
	runner := NewRunner(&capturingTaskService{}, catalog)

	candidates, err := runner.SelectWorkflowAutomations(workflow.AutomationSelectionRequest{
		OwnerIdentity: "alice",
		TaskType:      "email_reply",
		Request:       "Draft an email reply for the Vivare case.",
		ProjectKey:    "vivare-case",
	})
	if err != nil {
		t.Fatalf("SelectWorkflowAutomations: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want both plausible matches for operator selection", candidates)
	}
	if candidates[0].ID != firstID.String() || candidates[0].Score <= candidates[1].Score {
		t.Fatalf("ranked candidates = %#v", candidates)
	}
}

func TestRunnerAutomationSelectionRequiresCatalog(t *testing.T) {
	runner := NewRunner(&capturingTaskService{}, &capturingApprovalBindingPreparer{})

	candidates, err := runner.SelectWorkflowAutomations(workflow.AutomationSelectionRequest{
		OwnerIdentity: "alice",
		TaskType:      "email_reply",
		Request:       "Draft an email reply.",
	})
	if err == nil || !strings.Contains(err.Error(), "automation catalog is not configured") {
		t.Fatalf("error = %v, candidates = %#v", err, candidates)
	}
	if candidates != nil {
		t.Fatalf("unconfigured selector returned candidates: %#v", candidates)
	}
}

func TestDeferredRunnerDelegatesAutomationSelection(t *testing.T) {
	automationID := uuid.New()
	delegate := NewRunner(&capturingTaskService{}, &catalogApprovalBindingPreparer{
		automations: []*models.Automation{{
			ID:           automationID,
			Name:         "Email drafter",
			LaunchType:   "agent_runtime",
			LaunchTarget: "runtime://email",
			RuntimeType:  "hermes",
			Status:       "healthy",
		}},
	})
	runner := NewDeferredRunner()
	runner.Set(delegate)

	candidates, err := runner.SelectWorkflowAutomations(workflow.AutomationSelectionRequest{
		TaskType: "email",
		Request:  "Draft email",
	})
	if err != nil {
		t.Fatalf("SelectWorkflowAutomations: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != automationID.String() {
		t.Fatalf("delegated candidates = %#v", candidates)
	}
}

func TestRunnerPassesPursuitAndWorkflowContextToTaskEngine(t *testing.T) {
	tasks := &capturingTaskService{}
	runner := NewRunner(tasks)
	approvalSourceID := "workflow-decision:" + uuid.NewString()
	mandateID := uuid.NewString()
	approvedAt := time.Now().UTC()
	approvalDigest := strings.Repeat("d", 64)

	result, err := runner.RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity:         "alice",
		PursuitID:             "pursuit-1",
		WorkflowID:            "workflow-1",
		Request:               "Advance the governed workflow.",
		SuccessCriteria:       []string{"Only use linked evidence", "Leave external messages as drafts"},
		ProjectKey:            "018-hai",
		RiskLevel:             "medium",
		MandateID:             mandateID,
		HumanApproved:         true,
		ApprovalSourceID:      approvalSourceID,
		ApprovalBindingDigest: approvalDigest,
		ApprovalActorIdentity: "alice",
		ApprovalApprovedAt:    &approvedAt,
	})
	if err != nil {
		t.Fatalf("RunWorkflowTask returned error: %v", err)
	}
	if result == nil || !result.Passed {
		t.Fatalf("unexpected result: %#v", result)
	}
	if tasks.request.PursuitID != "pursuit-1" || tasks.request.WorkflowID != "workflow-1" || tasks.request.OwnerIdentity != "alice" ||
		tasks.request.IdempotencyKey != "workflow:workflow-1:approval:"+approvalSourceID {
		t.Fatalf("task context = %#v", tasks.request)
	}
	if !reflect.DeepEqual(tasks.request.SuccessCriteria, []string{"Only use linked evidence", "Leave external messages as drafts"}) {
		t.Fatalf("workflow success criteria were not delegated: %#v", tasks.request.SuccessCriteria)
	}
	if !tasks.request.HumanApproved || tasks.request.ApprovalSourceID != approvalSourceID {
		t.Fatalf("workflow approval provenance was not delegated intact: %#v", tasks.request)
	}
	if tasks.request.ApprovalBindingDigest != approvalDigest ||
		tasks.request.ApprovalActorIdentity != "alice" ||
		tasks.request.ApprovalApprovedAt == nil || !tasks.request.ApprovalApprovedAt.Equal(approvedAt) {
		t.Fatalf("workflow approval proof was not delegated intact: %#v", tasks.request)
	}
	if tasks.request.MandateID != mandateID || tasks.previewRequest.MandateID != mandateID {
		t.Fatalf("standing mandate was not delegated through preview and execution: preview=%#v run=%#v", tasks.previewRequest, tasks.request)
	}
	if tasks.previews != 1 || tasks.previewRequest.ExecuteAllowed || tasks.previewRequest.HumanApproved {
		t.Fatalf("framework preflight was not side-effect-free: previews=%d request=%#v", tasks.previews, tasks.previewRequest)
	}
	if !tasks.previewRequest.ExecutionRequested {
		t.Fatalf("framework preflight lost execution intent: %#v", tasks.previewRequest)
	}
	if !tasks.previewRequest.FrameworkSelectionHumanApproved {
		t.Fatalf("framework preflight lost the non-authorizing approval context required for a stable selection: %#v", tasks.previewRequest)
	}
	if tasks.previewRequest.ApprovalBindingDigest != "" || tasks.previewRequest.ApprovalActorIdentity != "" ||
		tasks.previewRequest.ApprovalApprovedAt != nil {
		t.Fatalf("framework preview retained trusted approval evidence: %#v", tasks.previewRequest)
	}
	if result.FrameworkSelection == nil ||
		result.FrameworkSelection.SelectionDecisionID == "" ||
		result.FrameworkSelection.TaskPlanID != result.PlanID ||
		result.FrameworkSelection.CatalogDigest != strings.Repeat("a", 64) ||
		result.FrameworkSelection.ConstitutionDigest != strings.Repeat("c", 64) {
		t.Fatalf("framework selection provenance = %#v", result.FrameworkSelection)
	}
}

func TestRunnerRejectsSelectorV5WithoutExplicitRiskContractBeforeExecution(t *testing.T) {
	plan := validWorkflowCompletionPlan("selector-v5-missing-risk")
	plan.FrameworkDecision.SelectorAlgorithmVersion = "selector-v5"
	tasks := &capturingTaskService{plan: plan}

	result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Run controlled work",
		RiskLevel:     "medium",
	})
	if err == nil || !strings.Contains(err.Error(), "selector-v5 task risk level") {
		t.Fatalf("error = %v, want missing selector-v5 risk contract", err)
	}
	if result != nil || tasks.runs != 0 || tasks.previews != 1 {
		t.Fatalf("invalid selector-v5 contract reached execution: result=%#v runs=%d previews=%d", result, tasks.runs, tasks.previews)
	}
}

func TestFrameworkRiskContractRejectsDowngradeAndAcceptsExactCeiling(t *testing.T) {
	maximumAutonomy := 6
	requiresApproval := true
	valid := &workflow.FrameworkSelectionProvenance{
		SelectionDecisionID:       uuid.NewString(),
		TaskPlanID:                "plan-risk",
		CatalogVersion:            "v2",
		CatalogDigest:             strings.Repeat("a", 64),
		SelectorAlgorithmVersion:  "selector-v5",
		TaskRiskLevel:             "high",
		EffectiveRiskCeiling:      "high",
		MaximumAutonomyLevel:      &maximumAutonomy,
		RequiresApproval:          &requiresApproval,
		EffectivePreferenceDigest: strings.Repeat("b", 64),
		ConstitutionVersion:       1,
		ConstitutionDigest:        strings.Repeat("c", 64),
		ConstitutionSource:        "builtin-robert-constitution-v1:v1",
		OperatingContractDigest:   strings.Repeat("d", 64),
	}
	if err := enforceFrameworkRiskFloor(valid, "high"); err != nil {
		t.Fatalf("exact risk ceiling rejected: %v", err)
	}
	downgraded := *valid
	downgraded.TaskRiskLevel = "medium"
	if err := enforceFrameworkRiskFloor(&downgraded, "high"); err == nil || !strings.Contains(err.Error(), "below workflow risk floor") {
		t.Fatalf("workflow risk downgrade error = %v", err)
	}
	insufficient := *valid
	insufficient.EffectiveRiskCeiling = "medium"
	if err := enforceFrameworkRiskFloor(&insufficient, "high"); err == nil || !strings.Contains(err.Error(), "below task risk") {
		t.Fatalf("framework ceiling downgrade error = %v", err)
	}
	plan := &task.CompletionPlan{RiskAssessment: task.RiskAssessment{Level: "high"}}
	if err := enforceFrameworkPlanRisk(&downgraded, plan); err == nil || !strings.Contains(err.Error(), "does not cover task plan risk") {
		t.Fatalf("task plan risk downgrade error = %v", err)
	}
}

func TestFrameworkRiskContractTreatsCriticalPlanRiskAsHighestSelectionBand(t *testing.T) {
	maximumAutonomy := 6
	requiresApproval := true
	selection := &workflow.FrameworkSelectionProvenance{
		SelectionDecisionID:       uuid.NewString(),
		TaskPlanID:                "plan-critical-risk",
		CatalogVersion:            "v2",
		CatalogDigest:             strings.Repeat("a", 64),
		SelectorAlgorithmVersion:  "selector-v5",
		TaskRiskLevel:             "high",
		EffectiveRiskCeiling:      "high",
		MaximumAutonomyLevel:      &maximumAutonomy,
		RequiresApproval:          &requiresApproval,
		EffectivePreferenceDigest: strings.Repeat("b", 64),
		ConstitutionVersion:       1,
		ConstitutionDigest:        strings.Repeat("c", 64),
		ConstitutionSource:        "builtin-robert-constitution-v1:v1",
		OperatingContractDigest:   strings.Repeat("d", 64),
	}
	plan := &task.CompletionPlan{RiskAssessment: task.RiskAssessment{Level: "critical"}}
	if err := enforceFrameworkPlanRisk(selection, plan); err != nil {
		t.Fatalf("high selector-v5 contract rejected critical task plan risk: %v", err)
	}
}

func TestFrameworkRiskContractExtractionRequiresExplicitFields(t *testing.T) {
	contract, err := frameworkRiskContractFromDecision(struct {
		TaskRiskLevel        string `json:"taskRiskLevel"`
		EffectiveRiskCeiling string `json:"effectiveRiskCeiling"`
	}{TaskRiskLevel: " HIGH ", EffectiveRiskCeiling: "high"})
	if err != nil {
		t.Fatalf("frameworkRiskContractFromDecision: %v", err)
	}
	if contract.TaskRiskLevel != "high" || contract.EffectiveRiskCeiling != "high" {
		t.Fatalf("normalized risk contract = %#v", contract)
	}
	legacy, err := frameworkRiskContractFromDecision(struct {
		SelectorAlgorithmVersion string `json:"selectorAlgorithmVersion"`
	}{SelectorAlgorithmVersion: "selector-v4"})
	if err != nil {
		t.Fatalf("legacy frameworkRiskContractFromDecision: %v", err)
	}
	if legacy.TaskRiskLevel != "" || legacy.EffectiveRiskCeiling != "" {
		t.Fatalf("legacy risk contract was inferred: %#v", legacy)
	}
}

func TestCompareFrameworkExecutionContractsRejectsAutonomyOrApprovalDrift(t *testing.T) {
	maximumAutonomy := 6
	requiresApproval := true
	preflight := &workflow.FrameworkSelectionProvenance{
		SelectionDecisionID: uuid.NewString(), TaskPlanID: "plan-contract",
		CatalogVersion: "v2", CatalogDigest: strings.Repeat("a", 64),
		SelectorAlgorithmVersion: "selector-v5", TaskRiskLevel: "high",
		EffectiveRiskCeiling: "high", MaximumAutonomyLevel: &maximumAutonomy,
		RequiresApproval: &requiresApproval, EffectivePreferenceDigest: strings.Repeat("b", 64),
		ConstitutionVersion: 1, ConstitutionDigest: strings.Repeat("c", 64),
		ConstitutionSource:      "builtin-robert-constitution-v1:v1",
		OperatingContractDigest: strings.Repeat("d", 64),
	}
	executed := *preflight
	if err := compareFrameworkRiskContracts(preflight, &executed); err != nil {
		t.Fatalf("identical execution contract rejected: %v", err)
	}

	lowerAutonomy := maximumAutonomy - 1
	executed.MaximumAutonomyLevel = &lowerAutonomy
	if err := compareFrameworkRiskContracts(preflight, &executed); err == nil {
		t.Fatal("autonomy drift from side-effect-free preview was accepted")
	}

	executed = *preflight
	approvalRemoved := false
	executed.RequiresApproval = &approvalRemoved
	if err := compareFrameworkRiskContracts(preflight, &executed); err == nil {
		t.Fatal("approval drift from side-effect-free preview was accepted")
	}
}

func TestRunnerRejectsForgedWorkflowApprovalBeforeTaskEngine(t *testing.T) {
	for _, test := range []struct {
		name             string
		owner            string
		workflowID       string
		approvalSourceID string
		expectedError    string
	}{
		{
			name:             "ownerless",
			workflowID:       "workflow-1",
			approvalSourceID: "workflow-decision:" + uuid.NewString(),
			expectedError:    "owner identity is required",
		},
		{
			name:             "missing workflow",
			owner:            "alice",
			approvalSourceID: "workflow-decision:" + uuid.NewString(),
			expectedError:    "workflow ID is required",
		},
		{
			name:          "caller controlled flag only",
			owner:         "alice",
			workflowID:    "workflow-1",
			expectedError: "workflow-decision provenance",
		},
		{
			name:             "wrong provenance type",
			owner:            "alice",
			workflowID:       "workflow-1",
			approvalSourceID: "task-review:" + uuid.NewString(),
			expectedError:    "workflow-decision provenance",
		},
		{
			name:             "malformed decision",
			owner:            "alice",
			workflowID:       "workflow-1",
			approvalSourceID: "workflow-decision:forged",
			expectedError:    "valid workflow decision UUID",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tasks := &capturingTaskService{}
			result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity:    test.owner,
				WorkflowID:       test.workflowID,
				Request:          "Perform reviewed work",
				HumanApproved:    true,
				ApprovalSourceID: test.approvalSourceID,
			})
			if err == nil || !strings.Contains(err.Error(), test.expectedError) {
				t.Fatalf("error = %v, want %q", err, test.expectedError)
			}
			if result != nil || tasks.runs != 0 {
				t.Fatalf("forged approval reached task engine: result=%#v runs=%d", result, tasks.runs)
			}
		})
	}
}

func TestRunnerStripsStaleApprovalMetadataFromUnapprovedTask(t *testing.T) {
	tasks := &capturingTaskService{}
	_, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity:    " alice ",
		WorkflowID:       " workflow-1 ",
		Request:          " Continue safe planning ",
		HumanApproved:    false,
		ApprovalNote:     "stale approval",
		ApprovalSourceID: "workflow-decision:" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("RunWorkflowTask: %v", err)
	}
	if tasks.runs != 1 {
		t.Fatalf("task runs = %d, want 1", tasks.runs)
	}
	if tasks.request.HumanApproved || tasks.request.ApprovalNote != "" || tasks.request.ApprovalSourceID != "" {
		t.Fatalf("stale approval metadata reached task engine: %#v", tasks.request)
	}
	if tasks.request.OwnerIdentity != "alice" || tasks.request.WorkflowID != "workflow-1" ||
		tasks.request.IdempotencyKey != "workflow:workflow-1:unapproved" {
		t.Fatalf("workflow identity was not normalized: %#v", tasks.request)
	}
}

func TestRunnerRejectsCompletedExternalActionWithoutImmutableEvidence(t *testing.T) {
	plan := validWorkflowCompletionPlan("task-plan-without-launch-evidence")
	plan.ExecutionResult = &task.ExecutionResult{
		VerificationStatus: "verified",
		ToolExecution: &task.ToolExecutionResult{
			Status: "completed",
		},
	}
	tasks := &capturingTaskService{plan: plan}

	result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Run the approved external action",
	})
	if err == nil || !strings.Contains(err.Error(), "no immutable launch-event evidence") {
		t.Fatalf("error = %v, want immutable evidence failure", err)
	}
	if result == nil || !result.ExternalActionExecuted {
		t.Fatalf("partial result must preserve the reported external action: %#v", result)
	}
}

func TestRunnerRejectsMissingFrameworkSelectionInsteadOfReportingCompletion(t *testing.T) {
	previewPlan := validWorkflowCompletionPlan("task-plan-with-selection")
	tasks := &capturingTaskService{previewPlan: previewPlan, plan: &task.CompletionPlan{
		ID:               "task-plan-without-selection",
		CompletionStatus: "validated",
		ValidationResult: task.ValidationResult{Passed: true, Status: "verified"},
	}}
	runner := NewRunner(tasks)

	result, err := runner.RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Advance workflow",
	})
	if err == nil || !strings.Contains(err.Error(), "no framework selection decision") {
		t.Fatalf("error = %v, want missing framework selection failure", err)
	}
	if result == nil || result.PlanID != "task-plan-without-selection" {
		t.Fatalf("partial task result must preserve plan evidence: %#v", result)
	}
}

func TestRunnerDoesNotReportCompletionForNonValidatedPlan(t *testing.T) {
	for _, status := range []string{"retry_needed", "review_required", "unknown"} {
		t.Run(status, func(t *testing.T) {
			plan := validWorkflowCompletionPlan("task-plan-" + status)
			plan.CompletionStatus = status
			tasks := &capturingTaskService{plan: plan}

			result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "alice",
				WorkflowID:    "workflow-1",
				Request:       "Advance workflow",
			})
			if err != nil {
				t.Fatalf("RunWorkflowTask: %v", err)
			}
			if result == nil || result.Passed {
				t.Fatalf("non-validated plan was reported as passed: %#v", result)
			}
			if result.ReviewRequired != (status == "review_required") {
				t.Fatalf("review-required = %t for status %q", result.ReviewRequired, status)
			}
		})
	}
}

func TestRunnerClassifiesOnlyPreflightFailuresAsSafeNoSideEffect(t *testing.T) {
	tests := []struct {
		name  string
		tasks *capturingTaskService
		input workflow.TaskRunRequest
	}{
		{
			name:  "normalization failure",
			tasks: &capturingTaskService{},
			input: workflow.TaskRunRequest{WorkflowID: "workflow-1", Request: ""},
		},
		{
			name:  "selection preview failure",
			tasks: &capturingTaskService{previewErr: errors.New("preview unavailable")},
			input: workflow.TaskRunRequest{OwnerIdentity: "alice", WorkflowID: "workflow-1", Request: "Review this task"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRunner(test.tasks).RunWorkflowTask(test.input)
			if err == nil || !workflow.IsTaskFailureSafeNoSideEffect(err) {
				t.Fatalf("error = %v, want explicit safe-no-side-effect marker", err)
			}
			if test.tasks.runs != 0 {
				t.Fatalf("task service Run calls = %d, want no execution", test.tasks.runs)
			}
		})
	}
}

func TestRunnerDoesNotMarkTaskServiceRunErrorsSafe(t *testing.T) {
	tasks := &capturingTaskService{err: errors.New("task engine failed after dispatch")}
	_, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Perform the approved work",
	})
	if err == nil || workflow.IsTaskFailureSafeNoSideEffect(err) {
		t.Fatalf("error = %v, want unmarked outcome-uncertain task service failure", err)
	}
	if tasks.runs != 1 {
		t.Fatalf("task service Run calls = %d, want one execution attempt", tasks.runs)
	}
}

func TestRunnerPreservesPartialExternalActionResultAlongsideRunError(t *testing.T) {
	plan := validWorkflowCompletionPlan("task-plan-partial-action")
	plan.ExecutionResult = &task.ExecutionResult{
		VerificationStatus: "needs_review",
		ToolExecution: &task.ToolExecutionResult{
			Status:        "completed",
			LaunchEventID: uuid.NewString(),
			RuntimeType:   "docker",
		},
	}
	runErr := errors.New("task engine lost its response after dispatch")
	tasks := &capturingTaskService{plan: plan, err: runErr}
	result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Perform the approved work",
	})
	if err == nil || !strings.Contains(err.Error(), runErr.Error()) {
		t.Fatalf("error = %v, want original post-dispatch failure", err)
	}
	if workflow.IsTaskFailureSafeNoSideEffect(err) {
		t.Fatalf("post-Run error was marked safe: %v", err)
	}
	if result == nil || result.PlanID != plan.ID || !result.ExternalActionExecuted || !strings.HasPrefix(result.RuntimeEvidenceURI, "automation-launch://") {
		t.Fatalf("partial action result/evidence was discarded: %#v", result)
	}
}

func TestRunnerPropagatesFrameworkSelectionFailureWithoutCompletionResult(t *testing.T) {
	tasks := &capturingTaskService{err: errors.New("select planning frameworks: selector unavailable")}
	runner := NewRunner(tasks)

	result, err := runner.RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "workflow-1",
		Request:       "Advance workflow",
	})
	if err == nil || !strings.Contains(err.Error(), "selector unavailable") {
		t.Fatalf("error = %v, want selector failure", err)
	}
	if result != nil {
		t.Fatalf("selection failure was described as complete: %#v", result)
	}
}

func validWorkflowCompletionPlan(planID string) *task.CompletionPlan {
	return &task.CompletionPlan{
		ID:               planID,
		CompletionStatus: "validated",
		ValidationResult: task.ValidationResult{Passed: true, Status: "verified"},
		FrameworkDecision: &frameworkregistry.SelectionDecision{
			ID:                        uuid.NewString(),
			TaskPlanID:                planID,
			CatalogVersion:            "v1",
			CatalogDigest:             strings.Repeat("a", 64),
			SelectorAlgorithmVersion:  "chief-of-staff-v1",
			EffectivePreferenceDigest: strings.Repeat("b", 64),
			ConstitutionVersion:       1,
			ConstitutionDigest:        strings.Repeat("c", 64),
			ConstitutionSource:        "builtin-robert-constitution-v1:v1",
		},
	}
}
