package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPlanIncludesSuccessCriteriaAndValidationGate(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Plan(IntakeRequest{
		Request:    "Add API code and tests for completion-first routing",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if len(plan.Intake.SuccessCriteria) == 0 {
		t.Fatalf("expected explicit success criteria")
	}
	if plan.ValidationPlan.CompletionGate == "" {
		t.Fatalf("expected validation completion gate")
	}
	if plan.ModelDecision.SelectedModelID == "" {
		t.Fatalf("expected model decision")
	}
	if len(plan.MemoryUpdateProposals) == 0 {
		t.Fatalf("expected memory update proposal")
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("expected universal task steps")
	}
	if len(plan.ToolDecision.SelectedTools) == 0 {
		t.Fatalf("expected tool routing decision")
	}
	if plan.FrameworkDecision == nil || len(plan.FrameworkDecision.Selected) == 0 {
		t.Fatalf("expected an explicit operating-framework decision")
	}
	if plan.FrameworkDecision.ConstitutionVersion == 0 {
		t.Fatalf("expected framework decision to identify its Constitution version")
	}
}

func TestTaskExecutionCancelsLLMGenerationWithTaskContext(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseProvider
	}))
	defer func() {
		close(releaseProvider)
		server.CloseClientConnections()
		server.Close()
	}()

	providers, err := json.Marshal([]llm.Provider{{
		ID: "localai", Name: "LocalAI", Enabled: true, Local: true,
		EndpointURL: server.URL,
		Models: []llm.Model{{
			ID: "task-local-model", Name: "Task local model", Tier: llm.TierLocal,
			Enabled: true, MaxDifficulty: 5, MaxReasoning: "very_high",
		}},
	}})
	if err != nil {
		t.Fatalf("marshal providers: %v", err)
	}
	t.Setenv("LLM_PROVIDERS_JSON", string(providers))
	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	llmService.WithFinalEffectAuthorization(
		llm.FinalEffectAuthorizerFunc(func(context.Context, llm.FinalEffectAuthorizationRequest) error { return nil }),
		llm.EmergencyStopEvaluatorFunc(func(context.Context) (llm.EmergencyStopState, error) {
			return llm.EmergencyStopState{}, nil
		}),
	)
	taskService := &service{llmService: llmService}
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan *ExecutionResult, 1)
	go func() {
		resultCh <- taskService.executeAllowedSteps(&CompletionPlan{
			ID:            "task-cancel-generation",
			OwnerIdentity: "alice",
			Request:       "Summarize context",
			RealGoal:      "Summarize context",
			ProjectKey:    "018-hai",
			RiskAssessment: RiskAssessment{
				AllowedNow: true,
			},
			ModelDecision: llm.RouteDecision{
				SelectedProviderID: "localai",
				SelectedModelID:    "task-local-model",
				SelectedModelName:  "Task local model",
				Tier:               llm.TierLocal,
			},
		}, IntakeRequest{Request: "Summarize context", ProjectKey: "018-hai", executionContext: ctx})
	}()

	select {
	case <-requestStarted:
	case result := <-resultCh:
		t.Fatalf("task generation returned before provider call: %#v", result)
	case <-time.After(time.Second):
		t.Fatal("task generation did not reach the provider")
	}
	cancel()
	var result *ExecutionResult
	select {
	case result = <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("task execution did not return after cancellation")
	}
	if result == nil || result.LLMGeneration == nil || result.LLMGeneration.Status != "stopped" {
		t.Fatalf("task generation result = %#v", result)
	}
	if !strings.Contains(result.BlockedReason, "caller canceled") || !hasTaskAction(result.Actions, "task.cancelled", "stopped") {
		t.Fatalf("task cancellation was not surfaced truthfully: %#v", result)
	}
	if hasTaskAction(result.Actions, "verification.answer", "completed") {
		t.Fatalf("task continued into verification after cancellation: %#v", result.Actions)
	}
}

func TestPlanReturnsConfigurationErrorWhenLLMRouterIsMissing(t *testing.T) {
	service := NewService(&fakeMemoryService{}, nil)

	_, err := service.Plan(IntakeRequest{Request: "Prepare a bounded task plan"})
	if !errors.Is(err, ErrTaskLLMRouterNotConfigured) {
		t.Fatalf("Plan error = %v, want %v", err, ErrTaskLLMRouterNotConfigured)
	}
}

func TestLowRiskReadOnlyAPIRuntimeCompletesWithoutModelOrApproval(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("LLM_PROVIDERS_JSON", "[]")
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")

	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	executor := &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		llmService,
		nil,
		nil,
		executor,
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Launch the local dashboard API for a bounded health review using the configured local dashboard automation.",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.ApprovalGranted {
		t.Fatalf("bounded read-only runtime acquired an approval requirement: %#v", plan.RiskAssessment)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1; risk=%#v preflight=%#v", executor.calls, plan.RiskAssessment, plan.FrameworkEvidencePreflight)
	}
	if plan.ExecutionResult == nil || !deterministicReadOnlyRuntimeCompleted(plan.ExecutionResult.ToolExecution) {
		t.Fatalf("deterministic runtime result was not retained: %#v", plan.ExecutionResult)
	}
	if plan.ModelDecision.SelectedModelID != "" {
		t.Fatalf("deterministic runtime unexpectedly selected model %q", plan.ModelDecision.SelectedModelID)
	}
	if !plan.ValidationResult.Passed || plan.CompletionStatus != "validated" {
		t.Fatalf("deterministic runtime did not validate: completion=%q validation=%#v preflight=%#v", plan.CompletionStatus, plan.ValidationResult, plan.FrameworkEvidencePreflight)
	}
}

func TestLowRiskReadOnlyAPIRuntimeIgnoresUnknownHumanCapacity(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("LLM_PROVIDERS_JSON", "[]")
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")

	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	executor := &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}
	provider := &operatingContextProviderStub{capacity: &frameworkregistry.CapacitySnapshot{
		Status:      "unknown",
		NeedsReview: true,
		Constraints: []string{"no owner-confirmed capacity snapshot is available"},
	}}
	service := NewServiceWithDependencies(
		&fakeMemoryService{},
		llmService,
		nil,
		nil,
		executor,
		nil,
		nil,
		nil,
		provider,
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Probe the local backend readiness endpoint and verify HTTP 200.",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 1 || plan.CompletionStatus != "validated" || !plan.ValidationResult.Passed {
		t.Fatalf("bounded automation was blocked by unrelated human capacity: calls=%d plan=%#v", executor.calls, plan)
	}
	if plan.RiskAssessment.ApprovalRequired || !plan.RiskAssessment.AllowedNow {
		t.Fatalf("bounded automation acquired a human-capacity approval gate: %#v", plan.RiskAssessment)
	}
	if provider.capacityCalls != 1 {
		t.Fatalf("operating capacity was not evaluated exactly once: %d", provider.capacityCalls)
	}
}

func TestUnapprovedHighRiskRuntimeStillBlocksBeforeExecutor(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	executor := &fakeToolExecutor{result: deterministicReadOnlyToolExecution()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
	)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Delete the external account and all retained records.",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.AllowedNow {
		t.Fatalf("high-risk action escaped the approval gate: %#v", plan.RiskAssessment)
	}
	if executor.calls != 0 {
		t.Fatalf("high-risk executor calls = %d, want zero", executor.calls)
	}
}

func TestTaskEntryPointsRejectMalformedStandingMandateBeforePlanning(t *testing.T) {
	engine := NewService(&fakeMemoryService{}, newTaskTestLLMService(t)).(*service)
	request := IntakeRequest{
		Request:   "Plan a harmless local checklist.",
		MandateID: "not-a-uuid",
	}

	for name, call := range map[string]func(IntakeRequest) (*CompletionPlan, error){
		"plan":    engine.Plan,
		"preview": engine.Preview,
		"run":     engine.Run,
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := call(request)
			if !errors.Is(err, ErrInvalidStandingMandateID) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidStandingMandateID)
			}
			if plan != nil {
				t.Fatalf("malformed mandate returned a plan: %#v", plan)
			}
		})
	}
}

func TestFrameworkDecisionCanTightenButNotBypassTaskRisk(t *testing.T) {
	selector := &fakeFrameworkSelector{decision: &frameworkregistry.SelectionDecision{
		ID:                   "selection-1",
		LifeDomain:           "communication",
		NeedOrCommitment:     "external commitment",
		MaximumAutonomyLevel: 3,
		RequiresApproval:     true,
		ApprovalReasons:      []string{"communication framework requires approval"},
		Selected: []frameworkregistry.SelectedFramework{{
			ID: "communication", Version: "1.0.0", Name: "Communication pack",
		}},
		CompletionCriteria:   []string{"approved communication matches the draft"},
		EvidenceRequirements: []string{"recipient and purpose"},
		ConstitutionVersion:  1,
	}}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		nil,
		selector,
	)

	plan, err := service.Plan(IntakeRequest{Request: "Prepare a short internal summary"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if selector.calls != 1 {
		t.Fatalf("framework selector calls = %d, want 1", selector.calls)
	}
	if !plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.AllowedNow {
		t.Fatalf("framework decision did not tighten risk: %#v", plan.RiskAssessment)
	}
	if plan.RiskAssessment.ApprovalGranted {
		t.Fatalf("framework selection must not manufacture approval")
	}
}

func TestFrameworkAutonomyCeilingBlocksExecutionEvenWhenApprovalIsReported(t *testing.T) {
	selector := &fakeFrameworkSelector{decision: &frameworkregistry.SelectionDecision{
		ID:                   "selection-constraint",
		LifeDomain:           "relationships_care",
		NeedOrCommitment:     "relationship support",
		MaximumAutonomyLevel: 1,
		Selected: []frameworkregistry.SelectedFramework{{
			ID: "relationships-care", Version: "1.0.0", Name: "Relationship and care pack",
		}},
		CompletionCriteria:  []string{"operator reviews the proposed action"},
		ConstitutionVersion: 1,
	}}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		nil,
		selector,
	)

	plan, err := service.Plan(IntakeRequest{
		Request:        "Implement the local change and run repository tests",
		AutomationID:   "controlled-runtime",
		ExecuteAllowed: true,
		HumanApproved:  true,
		ApprovalNote:   "Approved for this test only.",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	risk := plan.RiskAssessment
	if risk.AllowedNow {
		t.Fatalf("framework ceiling did not block execution: %#v", risk)
	}
	if risk.FrameworkAutonomyCeiling != 1 || risk.RequiredFrameworkAutonomy != 6 {
		t.Fatalf("framework authority intersection = ceiling %d / required %d", risk.FrameworkAutonomyCeiling, risk.RequiredFrameworkAutonomy)
	}
	if !strings.Contains(
		strings.Join(risk.Reasons, "\n"),
		"selected framework ceiling level 1 is below the level 6 required for this action",
	) {
		t.Fatalf("framework ceiling reason missing: %#v", risk.Reasons)
	}
}

func TestFrameworkOperatingContractBlocksUnavailableCapacityAndUnassignedTeamExecution(t *testing.T) {
	risk := applyFrameworkRisk(
		RiskAssessment{AllowedNow: true},
		&frameworkregistry.SelectionDecision{
			MaximumAutonomyLevel: 6,
			Capacity: frameworkregistry.CapacitySnapshot{
				Status: "unavailable",
			},
			Coordination: frameworkregistry.CoordinationPlan{
				Mode: "hierarchical",
			},
			Delegations: []frameworkregistry.DelegationContract{{
				Delegatee: "specialist",
				State:     "requires_assignment",
			}},
			ActionAutonomy: []frameworkregistry.ActionAutonomyDecision{{
				Action:           "execute_case_approved_action",
				RequiredLevel:    6,
				EffectiveCeiling: 6,
				Allowed:          true,
			}},
		},
		IntakeAnalysis{NeedsTools: true},
		IntakeRequest{ExecuteAllowed: true},
	)

	if risk.AllowedNow {
		t.Fatalf("unavailable capacity and unassigned multi-agent execution were allowed: %#v", risk)
	}
	reasons := strings.Join(risk.Reasons, "\n")
	for _, fragment := range []string{"capacity is unavailable", "fresh verified agent card"} {
		if !strings.Contains(reasons, fragment) {
			t.Errorf("risk reasons %v do not contain %q", risk.Reasons, fragment)
		}
	}
}

func TestFrameworkOperatingContractBlocksUnassignedSpecialistInSingleEngineMode(t *testing.T) {
	risk := applyFrameworkRisk(
		RiskAssessment{AllowedNow: true},
		&frameworkregistry.SelectionDecision{
			MaximumAutonomyLevel: 8,
			Capacity:             frameworkregistry.CapacitySnapshot{Status: "available"},
			Coordination:         frameworkregistry.CoordinationPlan{Mode: "single_engine"},
			Delegations: []frameworkregistry.DelegationContract{
				{Delegatee: "hai_task_engine", State: "ready"},
				{Delegatee: "health_admin_assistant", State: "requires_assignment"},
			},
			ActionAutonomy: []frameworkregistry.ActionAutonomyDecision{{
				Action: "execute_reversible_low_risk_action", RequiredLevel: 8, EffectiveCeiling: 8, Allowed: true,
			}},
		},
		IntakeAnalysis{NeedsTools: true},
		IntakeRequest{ExecuteAllowed: true},
	)

	if risk.AllowedNow {
		t.Fatalf("single-engine fallback silently bypassed an unassigned required specialist: %#v", risk)
	}
	if !strings.Contains(strings.Join(risk.Reasons, "\n"), "every required delegated participant") {
		t.Fatalf("missing-specialist reason was not retained: %#v", risk.Reasons)
	}
}

func TestRequiredFrameworkAutonomyDistinguishesApprovedAndAutomaticExecution(t *testing.T) {
	intake := IntakeAnalysis{NeedsTools: true}
	if got := requiredFrameworkAutonomy(intake, IntakeRequest{
		ExecuteAllowed: true,
		HumanApproved:  true,
	}); got != 6 {
		t.Fatalf("case-approved execution requires level %d, want 6", got)
	}
	if got := requiredFrameworkAutonomy(intake, IntakeRequest{
		ExecuteAllowed: true,
	}); got != 8 {
		t.Fatalf("automatic reversible execution requires level %d, want 8", got)
	}
}

func TestAssessRiskPreservesApprovalForRequirementsDiscoveredAfterIntake(t *testing.T) {
	risk := assessRisk(IntakeAnalysis{
		RiskLevel:  "low",
		NeedsTools: true,
	}, IntakeRequest{
		Request:        "Run the selected read-only readiness probe",
		AutomationID:   "controlled-runtime",
		ExecuteAllowed: true,
		HumanApproved:  true,
	})

	if !risk.ApprovalGranted {
		t.Fatalf("recorded approval was lost before later planning gates: %#v", risk)
	}
	if !risk.AllowedNow {
		t.Fatalf("approved low-risk execution should remain eligible: %#v", risk)
	}

	decision := &resourceplanner.Decision{
		Authority:       "advisory_only",
		Feasibility:     resourceplanner.FeasibleWithApprovals,
		ApprovalFlags:   []resourceplanner.ApprovalFlag{{Code: "operator_review"}},
		CanExecute:      false,
		GrantsAuthority: false,
	}
	risk = applyResourcePlanningRisk(risk, decision)
	if !risk.ApprovalRequired || !risk.ApprovalGranted || !risk.AllowedNow {
		t.Fatalf("resource planning overrode the recorded approval: %#v", risk)
	}
}

func TestAnalyzeIntakeDoesNotRequireRuntimeForAPIExplanation(t *testing.T) {
	analysis := analyzeIntake(IntakeRequest{Request: "Explain the API architecture and compare routing options"})
	if analysis.NeedsTools || analysis.NeedsLocalExecution {
		t.Fatalf("analysis-only request incorrectly requires runtime execution: %#v", analysis)
	}
}

func TestAnalyzeIntakeRequiresRuntimeForTechnicalImplementation(t *testing.T) {
	analysis := analyzeIntake(IntakeRequest{Request: "Implement API code and run repository tests"})
	if !analysis.NeedsTools || !analysis.NeedsLocalExecution {
		t.Fatalf("implementation request did not require controlled local execution: %#v", analysis)
	}
}

func TestAnalyzeIntakeUsesExplicitAutomationForReadOnlyProbe(t *testing.T) {
	analysis := analyzeIntake(IntakeRequest{
		Request:      "Probe the local backend readiness endpoint and verify HTTP 200.",
		AutomationID: uuid.NewString(),
	})
	if !analysis.NeedsTools || analysis.NeedsLocalExecution {
		t.Fatalf("explicit API automation was not classified as a controlled non-local tool: %#v", analysis)
	}
	if analysis.TaskType != "automation" || analysis.RequiredReasoning != "low" {
		t.Fatalf("read-only automation classification = %#v", analysis)
	}
	tools := routeTools(analysis, IntakeRequest{
		Request:      "Probe the local backend readiness endpoint and verify HTTP 200.",
		AutomationID: uuid.NewString(),
	})
	if !containsString(tools.SelectedTools, "automation.launch") {
		t.Fatalf("controlled automation tool was not selected: %#v", tools)
	}
}

func TestPlanRefreshesDueSourcesBeforeSourceSearch(t *testing.T) {
	mem := &fakeMemoryService{}
	src := &fakeTaskSourceService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService, src)

	plan, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Summarize local project files and source context",
		ProjectKey:    "018-HAI",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if src.refreshCalls != 0 || len(src.ownerRefreshOwners) != 1 || src.ownerRefreshOwners[0] != "alice" {
		t.Fatalf("source refresh = global %d / owner %#v, want one Alice-scoped refresh", src.refreshCalls, src.ownerRefreshOwners)
	}
	if src.searchCalls != 1 {
		t.Fatalf("searchCalls = %d, want 1", src.searchCalls)
	}
	if len(src.order) < 2 || src.order[0] != "owner-refresh" || src.order[1] != "search" {
		t.Fatalf("order = %#v, want owner-scoped refresh before search", src.order)
	}
	if plan.ContextPlan.SourceRefresh == nil {
		t.Fatalf("expected source refresh result in context plan")
	}
	if len(plan.ContextPlan.SourceContext) != 1 {
		t.Fatalf("source context = %d, want 1", len(plan.ContextPlan.SourceContext))
	}
}

func TestTaskSourceRefreshPropagatesTaskCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &fakeTaskSourceService{refreshStarted: make(chan struct{})}
	svc := NewService(&fakeMemoryService{}, newTaskTestLLMService(t), src).(*service)

	type result struct {
		run *source.ScheduledSyncRun
		err error
	}
	finished := make(chan result, 1)
	go func() {
		run, _, err := svc.refreshSourcesForTask(
			IntakeRequest{OwnerIdentity: "alice", Request: "Summarize the connected documents", executionContext: ctx},
			IntakeAnalysis{NeedsDocuments: true},
		)
		finished <- result{run: run, err: err}
	}()
	select {
	case <-src.refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("context-aware source refresh did not start")
	}
	cancel()
	select {
	case got := <-finished:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("refresh sources error = %v, want context canceled", got.err)
		}
		if got.run != nil || len(src.ownerRefreshOwners) != 0 {
			t.Fatalf("canceled refresh used detached path: run=%#v owners=%#v", got.run, src.ownerRefreshOwners)
		}
	case <-time.After(time.Second):
		t.Fatal("source refresh did not stop after task cancellation")
	}
}

func TestPlanSkipsConnectedSourceSearchForBoundedReadOnlyAutomation(t *testing.T) {
	mem := &fakeMemoryService{}
	src := &fakeTaskSourceService{}
	service := NewService(mem, newTaskTestLLMService(t), src)

	plan, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Probe the local backend readiness endpoint and verify HTTP 200.",
		ProjectKey:    "018-HAI",
		AutomationID:  uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if src.refreshCalls != 0 || len(src.ownerRefreshOwners) != 0 {
		t.Fatalf("bounded automation refreshed connected sources: global=%d owner=%#v", src.refreshCalls, src.ownerRefreshOwners)
	}
	if src.searchCalls != 0 || len(src.searchRequests) != 0 {
		t.Fatalf("bounded automation searched connected sources: calls=%d requests=%#v", src.searchCalls, src.searchRequests)
	}
	if len(plan.ContextPlan.SourceContext) != 0 {
		t.Fatalf("bounded automation loaded source context: %#v", plan.ContextPlan.SourceContext)
	}
	if !strings.Contains(plan.ContextPlan.Explanation, "retrieval skipped") {
		t.Fatalf("context explanation does not disclose the source skip: %q", plan.ContextPlan.Explanation)
	}
}

func TestPlanScopesMemoryAndSourceSearchToOwnerAndSkipsGlobalRefresh(t *testing.T) {
	mem := &fakeMemoryService{}
	src := &fakeTaskSourceService{}
	service := NewService(mem, newTaskTestLLMService(t), src)

	plan, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Summarize local project files and source context",
		ProjectKey:    "018-HAI",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.OwnerIdentity != "alice" {
		t.Fatalf("plan owner = %q, want alice", plan.OwnerIdentity)
	}
	if len(mem.ownerRetrieveOwners) != 1 || mem.ownerRetrieveOwners[0] != "alice" {
		t.Fatalf("owner-scoped memory retrieval = %#v, want alice", mem.ownerRetrieveOwners)
	}
	if src.refreshCalls != 0 {
		t.Fatalf("owner-scoped task triggered global source refresh %d times", src.refreshCalls)
	}
	if len(src.ownerRefreshOwners) != 1 || src.ownerRefreshOwners[0] != "alice" {
		t.Fatalf("owner-scoped refresh owners = %#v, want alice", src.ownerRefreshOwners)
	}
	if plan.ContextPlan.SourceRefresh == nil {
		t.Fatal("owner-scoped task did not retain its source refresh result")
	}
	if len(src.searchRequests) != 1 || src.searchRequests[0].OwnerIdentity != "alice" {
		t.Fatalf("source search requests = %#v, want owner alice", src.searchRequests)
	}
}

func TestPlanContextReachesMemoryRetrieval(t *testing.T) {
	type contextKey struct{}
	mem := &fakeMemoryService{}
	service := NewService(mem, newTaskTestLLMService(t))
	ctx := context.WithValue(context.Background(), contextKey{}, "request-bound")

	_, err := service.(PlanningContextService).PlanContext(ctx, IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Prepare a bounded project status brief",
		ProjectKey:    "018-HAI",
	})
	if err != nil {
		t.Fatalf("PlanContext: %v", err)
	}
	if mem.lastRetrieveContext == nil || mem.lastRetrieveContext.Value(contextKey{}) != "request-bound" {
		t.Fatal("task planning did not propagate its request context to memory retrieval")
	}
}

func TestPlanUsesOwnerScopedCalendarCapacityEvidence(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeTaskSourceService{calendarBusy: []source.CalendarBusyInterval{{
		Start: now.Add(10 * time.Minute), End: now.Add(50 * time.Minute),
		Title: "Existing appointment", SourceURI: "https://calendar.example/event", SourceID: uuid.NewString(),
	}}}
	service := NewService(&fakeMemoryService{}, newTaskTestLLMService(t), src)
	plan, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice", Request: "Prepare a bounded project brief", ProjectKey: "018-HAI",
		Capacity: &frameworkregistry.CapacitySnapshot{Status: "available", TimeAvailableMinutes: 120, Fresh: true},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if src.calendarOwner != "alice" || !src.calendarEnd.After(src.calendarStart) {
		t.Fatalf("calendar capacity request was not owner scoped and bounded: owner=%q start=%v end=%v", src.calendarOwner, src.calendarStart, src.calendarEnd)
	}
	if plan.CalendarCapacity.Status != "source_backed" || len(plan.CalendarCapacity.BusyIntervals) != 1 {
		t.Fatalf("calendar capacity evidence missing from plan: %#v", plan.CalendarCapacity)
	}
	if plan.ResourceDecision == nil {
		t.Fatal("calendar-aware resource decision missing")
	}
	foundEvent := false
	for _, taskEvent := range plan.Events {
		if taskEvent.Stage == "calendar-capacity" && strings.Contains(taskEvent.Message, "read-only Google Calendar") {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("calendar capacity audit event missing: %#v", plan.Events)
	}
}

func TestPlanFailsClosedWhenCalendarCapacityCannotBeRead(t *testing.T) {
	src := &fakeTaskSourceService{calendarBusyErr: errors.New("calendar repository unavailable")}
	service := NewService(&fakeMemoryService{}, newTaskTestLLMService(t), src)
	_, err := service.Plan(IntakeRequest{OwnerIdentity: "alice", Request: "Prepare a bounded project brief"})
	if err == nil || !strings.Contains(err.Error(), "load calendar-backed capacity") {
		t.Fatalf("calendar capacity error did not fail closed: %v", err)
	}
}

func TestOwnerScopedTaskHistoryAndReviewQueueDoNotLeakAcrossOwners(t *testing.T) {
	service := NewService(&fakeMemoryService{}, newTaskTestLLMService(t))
	scoped, ok := service.(OwnerScopedService)
	if !ok {
		t.Fatal("native task service does not implement OwnerScopedService")
	}
	if _, err := service.Plan(IntakeRequest{OwnerIdentity: "alice", Request: "Plan Alice project context"}); err != nil {
		t.Fatalf("Plan alice: %v", err)
	}
	if _, err := service.Plan(IntakeRequest{OwnerIdentity: "bob", Request: "Plan Bob project context"}); err != nil {
		t.Fatalf("Plan bob: %v", err)
	}
	if logs := scoped.LogsForOwner("alice"); len(logs) != 1 || logs[0].OwnerIdentity != "alice" {
		t.Fatalf("alice logs = %#v, want only Alice record", logs)
	}

	aliceReview, err := service.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete Alice account data"})
	if err != nil || aliceReview.ReviewQueueItem == nil {
		t.Fatalf("Run alice high-risk task = %#v, %v", aliceReview, err)
	}
	bobReview, err := service.Run(IntakeRequest{OwnerIdentity: "bob", Request: "Delete Bob account data"})
	if err != nil || bobReview.ReviewQueueItem == nil {
		t.Fatalf("Run bob high-risk task = %#v, %v", bobReview, err)
	}
	if queue := scoped.ReviewQueueForOwner("alice"); len(queue) != 1 || queue[0].Request.OwnerIdentity != "alice" {
		t.Fatalf("alice review queue = %#v, want only Alice item", queue)
	}
	if _, err := scoped.ResolveReviewItemForOwner("alice", bobReview.ReviewQueueItem.ID, ApprovalDecision{Approved: false}); err == nil {
		t.Fatal("expected cross-owner review resolution to be rejected")
	}
	if _, err := scoped.ResolveReviewItemForOwner("alice", aliceReview.ReviewQueueItem.ID, ApprovalDecision{Approved: false, Note: "not approved"}); err != nil {
		t.Fatalf("owner could not resolve own review item: %v", err)
	}
}

func TestPursuitScopedTaskRunPersistsStartAndFinalOutcome(t *testing.T) {
	recorder := &fakePursuitAttemptRecorder{}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		recorder,
	)
	pursuitID := uuid.New()
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		PursuitID:      pursuitID.String(),
		Request:        "Delete account data after legal review with api_key=plain-text-secret",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recorder.attempts) != 2 {
		t.Fatalf("persist calls = %d, want start and final outcome", len(recorder.attempts))
	}
	final := recorder.attempts[1]
	if final.PursuitID != pursuitID || final.TaskPlanID != plan.ID || final.Status != "review_required" {
		t.Fatalf("final pursuit attempt = %#v", final)
	}
	if final.CompletedAt == nil || final.BlockedReason == "" {
		t.Fatalf("final attempt lacks completion audit: %#v", final)
	}
	if strings.Contains(final.RequestSummary, "plain-text-secret") {
		t.Fatalf("task attempt leaked request secret: %#v", final)
	}
}

func TestWorkflowScopedPursuitTaskRunDoesNotDuplicateWorkflowAttempt(t *testing.T) {
	recorder := &fakePursuitAttemptRecorder{}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		recorder,
	)
	if _, err := service.Run(IntakeRequest{
		OwnerIdentity: "alice",
		PursuitID:     uuid.NewString(),
		WorkflowID:    uuid.NewString(),
		Request:       "Create a bounded low-risk workflow summary.",
		ProjectKey:    "018-HAI",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recorder.attempts) != 0 {
		t.Fatalf("workflow-owned task run created duplicate pursuit attempts: %#v", recorder.attempts)
	}
}

func TestPursuitScopedTaskPlanRejectsMalformedPursuitID(t *testing.T) {
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		&fakePursuitAttemptRecorder{},
	)
	if _, err := service.Plan(IntakeRequest{Request: "Plan a bounded review", PursuitID: "not-a-uuid"}); err == nil {
		t.Fatal("expected malformed pursuit id to be rejected")
	}
}

func TestPursuitScopedTaskPlanChecksLifecycleGuardBeforePlanning(t *testing.T) {
	guard := &fakePursuitTaskGuard{err: fmt.Errorf("pursuit candidate must be accepted before direct task planning or execution")}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		nil,
		nil,
		nil,
		nil,
		guard,
	)

	_, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice",
		PursuitID:     uuid.NewString(),
		Request:       "Prepare a task plan for this candidate.",
	})
	if err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("Plan returned %v, want lifecycle guard error", err)
	}
	if guard.calls != 1 || len(guard.attempts) != 0 {
		t.Fatalf("guard calls=%d attempts=%#v, want one pre-plan check and no persisted attempts", guard.calls, guard.attempts)
	}
}

func TestPursuitScopedTaskRunPersistsExactRuntimeLaunchEvidence(t *testing.T) {
	recorder := &fakePursuitAttemptRecorder{}
	executor := &fakeToolExecutor{result: completedToolResult()}
	verifier := &sequencedVerificationService{}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		verifier,
		executor,
		recorder,
	)
	pursuitID := uuid.NewString()
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		PursuitID:      pursuitID,
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recorder.attempts) != 2 {
		t.Fatalf("persist calls = %d, want start and final outcome", len(recorder.attempts))
	}
	if got := recorder.attempts[1].LaunchEventID; got != executor.result.LaunchEventID {
		t.Fatalf("launch evidence = %q, want %q", got, executor.result.LaunchEventID)
	}
	if len(verifier.requests) != 1 || verifier.requests[0].PursuitID != pursuitID {
		t.Fatalf("verification request pursuit id = %#v, want %q; validation=%#v", verifier.requests, pursuitID, plan.ValidationResult)
	}
}

func TestPursuitScopedTaskRunRequiresReviewWhenVerificationCannotLinkEvidence(t *testing.T) {
	verifier := &sequencedVerificationService{pursuitLinkError: "pursuit is not visible to the authenticated owner"}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		verifier,
		nil,
		&fakePursuitAttemptRecorder{},
	)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity: "alice",
		PursuitID:     uuid.NewString(),
		Request:       "Summarize project context for the dashboard",
		ProjectKey:    "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" || plan.ExecutionResult == nil {
		t.Fatalf("verification link failure was treated as complete: %#v", plan)
	}
	if plan.ExecutionResult.BlockedReason != "verification evidence could not be linked to the pursuit" {
		t.Fatalf("blocked reason = %q", plan.ExecutionResult.BlockedReason)
	}
}

func TestPursuitResourceReservationRunsOnlyForExecutionAndSettlesEveryAttempt(t *testing.T) {
	recorder := &fakePursuitAttemptRecorder{}
	verifier := &sequencedVerificationService{
		statuses: []string{verification.StatusNeedsReview, verification.StatusSourceSupported},
	}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{}, newTaskTestLLMService(t), nil, verifier, nil, recorder,
	)
	pursuitID := uuid.NewString()
	if _, err := service.Plan(IntakeRequest{
		OwnerIdentity: "alice", PursuitID: pursuitID,
		Request: "Prepare a bounded project summary.", ProjectKey: "018-HAI",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(recorder.reservations) != 0 || len(recorder.settlements) != 0 {
		t.Fatalf("planning held execution resources: reservations=%#v settlements=%#v", recorder.reservations, recorder.settlements)
	}

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity: "alice", PursuitID: pursuitID,
		Request: "Prepare a bounded project summary.", ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.RetryPolicy.CurrentAttempt != 2 {
		t.Fatalf("retry attempt = %d, want 2 (status %q)", plan.RetryPolicy.CurrentAttempt, plan.CompletionStatus)
	}
	if len(recorder.reservations) != 2 || len(recorder.settlements) != 2 {
		t.Fatalf("reservation lifecycle = %#v %#v, want two complete attempts", recorder.reservations, recorder.settlements)
	}
	for index := range recorder.reservations {
		if !strings.HasSuffix(recorder.reservations[index], fmt.Sprintf(":attempt:%d", index+1)) {
			t.Fatalf("reservation operation %q does not identify attempt %d", recorder.reservations[index], index+1)
		}
		if recorder.settlements[index] != recorder.reservations[index]+":consumed" {
			t.Fatalf("settlement %q does not close %q", recorder.settlements[index], recorder.reservations[index])
		}
	}
}

func TestPursuitResourceReservationFailureBlocksBeforeExecution(t *testing.T) {
	recorder := &fakePursuitAttemptRecorder{reserveErr: fmt.Errorf("effort reservation exceeds remaining ceiling")}
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{}, newTaskTestLLMService(t), nil, nil, executor, recorder,
	)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity: "alice", PursuitID: uuid.NewString(),
		Request: "Run local script tests for the project", ProjectKey: "018-HAI",
		AutomationID: executor.result.AutomationID, ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("runtime executed %d times after failed resource hold", executor.calls)
	}
	if plan.ExecutionResult == nil || !strings.Contains(plan.ExecutionResult.BlockedReason, "reservation blocked execution") {
		t.Fatalf("reservation failure was not surfaced: %#v", plan.ExecutionResult)
	}
	if len(recorder.settlements) != 0 {
		t.Fatalf("failed reservation was settled: %#v", recorder.settlements)
	}
}

func TestRunQueuesReviewForHighRiskTask(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Run(IntakeRequest{
		Request:    "Delete account data and send a public posting",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if plan.CompletionStatus != "review_required" {
		t.Fatalf("status = %q, want review_required", plan.CompletionStatus)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatalf("expected review queue item")
	}
	if len(service.ReviewQueue()) == 0 {
		t.Fatalf("expected service review queue entry")
	}
}

func TestTaskClassificationProtectsExternalCommunicationAndMutations(t *testing.T) {
	communication := analyzeIntake(IntakeRequest{Request: "Send a reply email to the client"})
	if communication.RiskLevel != "high" || !communication.NeedsApproval {
		t.Fatalf("external communication risk = %#v, want high risk with approval", communication)
	}

	mutation := analyzeIntake(IntakeRequest{Request: "Commit and push the repository update"})
	if mutation.RiskLevel != "medium" || !mutation.NeedsApproval {
		t.Fatalf("repository mutation risk = %#v, want medium risk with approval", mutation)
	}
}

func TestRunQueuesReviewForMediumRiskRuntimeMutation(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(mem, llmService, nil, nil, executor)

	plan, err := service.Run(IntakeRequest{
		Request:        "Commit and push the repository update",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" || plan.RiskAssessment.Level != "medium" {
		t.Fatalf("medium-risk mutation was not queued for review: %#v", plan.RiskAssessment)
	}
	if plan.ReviewQueueItem == nil || executor.calls != 0 {
		t.Fatalf("medium-risk runtime mutation bypassed review: item=%#v calls=%d", plan.ReviewQueueItem, executor.calls)
	}
}

func TestRunBlocksExecutionWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Run(IntakeRequest{
		Request:        "Create a low-risk admin checklist",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
		HumanApproved:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" {
		t.Fatalf("status = %q, want review_required", plan.CompletionStatus)
	}
	if plan.ExecutionResult == nil || plan.ExecutionResult.BlockedReason == "" {
		t.Fatalf("expected blocked execution result, got %#v", plan.ExecutionResult)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatalf("expected review queue item")
	}
}

func TestRunBlocksExecutionWhenPersistedEmergencyStopActive(t *testing.T) {
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator paused execution", nil
	}))
	defer restore()

	service := NewService(&fakeMemoryService{}, newTaskTestLLMService(t))
	plan, err := service.Run(IntakeRequest{
		Request:        "Create a low-risk admin checklist",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
		HumanApproved:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" || plan.ExecutionResult == nil {
		t.Fatalf("persisted stop did not block task execution: %#v", plan)
	}
	if plan.ExecutionResult.BlockedReason != "operator paused execution" {
		t.Fatalf("blocked reason = %q", plan.ExecutionResult.BlockedReason)
	}
}

func TestRunWithoutExecutionPermissionQueuesReviewForToolWork(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Run(IntakeRequest{
		Request:    "Run local Docker build and tests for the project",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if plan.CompletionStatus != "review_required" {
		t.Fatalf("status = %q, want review_required", plan.CompletionStatus)
	}
	if plan.ExecutionResult != nil {
		t.Fatalf("execution result = %#v, want nil when execution was not explicitly allowed", plan.ExecutionResult)
	}
	if len(service.ReviewQueue()) == 0 {
		t.Fatalf("expected review queue entry")
	}
}

func TestRunQueuesMissingAutomationBeforeRuntimeExecution(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(mem, llmService, nil, nil, executor)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" || plan.RiskAssessment.ActionResolution != "clarify" {
		t.Fatalf("missing runtime configuration was not converted into clarification: %#v", plan.RiskAssessment)
	}
	if len(plan.RiskAssessment.MissingParameters) != 1 || plan.RiskAssessment.MissingParameters[0] != "controlled automation" {
		t.Fatalf("missing execution details = %#v", plan.RiskAssessment.MissingParameters)
	}
	if plan.ReviewQueueItem == nil || plan.ReviewQueueItem.Reason != "missing required execution details: controlled automation" || executor.calls != 0 {
		t.Fatalf("runtime preflight bypassed review: item=%#v calls=%d", plan.ReviewQueueItem, executor.calls)
	}
}

func TestResolveReviewItemRecordsOneShotApprovalAndReopensBlockedTask(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	taskService := NewService(mem, llmService)

	plan, err := taskService.Run(IntakeRequest{
		Request:    "Send a public posting after approval",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatalf("expected review queue item")
	}

	result, err := taskService.ResolveReviewItem(plan.ReviewQueueItem.ID, ApprovalDecision{
		Approved: true,
		Note:     "Operator approved controlled internal execution only.",
	})
	if err != nil {
		t.Fatalf("ResolveReviewItem: %v", err)
	}
	if result.Plan == nil {
		t.Fatalf("expected approved item to rerun task")
	}
	if !result.Plan.RiskAssessment.ApprovalGranted {
		t.Fatalf("expected approval to be reflected in risk assessment")
	}
	if result.Plan.CompletionStatus != "review_required" {
		t.Fatalf("completion status = %q, want review_required after authority gate", result.Plan.CompletionStatus)
	}
	if result.Item.Status != "needs_review" || result.Item.Decision != "" || result.Item.ResolvedAt != nil {
		t.Fatalf("blocked one-shot approval did not reopen for a new decision: %#v", result.Item)
	}

	implementation := taskService.(*service)
	decisions, err := implementation.stateRepository.ListReviewDecisions(
		internalTaskStateOwnerIdentity,
		plan.ReviewQueueItem.ID,
		50,
	)
	if err != nil {
		t.Fatalf("ListReviewDecisions: %v", err)
	}
	if len(decisions) != 1 ||
		decisions[0].Decision != "approved" ||
		decisions[0].ApprovalSourceID != "task-review:"+plan.ReviewQueueItem.ID {
		t.Fatalf("immutable approval provenance = %#v", decisions)
	}
}

func TestRunValidatedTaskStoresLesson(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Run(IntakeRequest{
		Request:    "Summarize project context for the dashboard",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if plan.CompletionStatus != "validated" {
		t.Fatalf("status = %q, want validated: validation=%#v model=%#v events=%#v", plan.CompletionStatus, plan.ValidationResult, plan.ModelDecision, plan.Events)
	}
	if !plan.ValidationResult.Passed {
		t.Fatalf("expected validation to pass")
	}
	if len(plan.StoredMemoryIDs) == 0 {
		t.Fatalf("expected stored lesson memory")
	}
}

func TestRunToolTaskRequiresConfiguredControlledRuntime(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   uuid.NewString(),
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" {
		t.Fatalf("status = %q, want review_required", plan.CompletionStatus)
	}
	if plan.ExecutionResult == nil || plan.ExecutionResult.BlockedReason != "controlled runtime executor is not configured" {
		t.Fatalf("unexpected execution result: %#v", plan.ExecutionResult)
	}
	if plan.RetryPolicy.RetryAvailable {
		t.Fatalf("configuration blockers must not be retried automatically")
	}
}

func TestRunToolTaskExecutesConfiguredAutomation(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(mem, llmService, nil, nil, executor)

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
	if plan.CompletionStatus != "validated" {
		t.Fatalf("status = %q, want validated: %#v", plan.CompletionStatus, plan.ValidationResult)
	}
	if executor.calls != 1 {
		t.Fatalf("runtime calls = %d, want 1", executor.calls)
	}
	if len(executor.requests) != 1 || executor.requests[0].ApprovalSourceID != "" {
		t.Fatalf("low-risk classification synthesized approval provenance: %#v", executor.requests)
	}
	governance := executor.requests[0].Governance
	if governance.TaskPlanID != plan.ID || len(governance.TaskPlanDigest) != 64 {
		t.Fatalf("runtime request did not bind the exact task plan: %#v", governance)
	}
	if governance.FrameworkSelectionID == "" ||
		governance.FrameworkSelectionID != plan.FrameworkDecision.ID ||
		len(governance.FrameworkConstitutionDigest) != 64 {
		t.Fatalf("runtime request did not bind framework governance: %#v", governance)
	}
	if plan.DomainPackDecision != nil &&
		governance.DomainPackDecisionID != plan.DomainPackDecision.ID {
		t.Fatalf("runtime request did not bind domain-pack guidance: %#v", governance)
	}
	if plan.ExecutionResult == nil || plan.ExecutionResult.ToolExecution == nil {
		t.Fatalf("expected persisted runtime evidence")
	}
}

func TestRunToolTaskBlocksBeforeEffectWithoutVerifiedOwnerIdentity(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
	)

	plan, err := service.Run(IntakeRequest{
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("runtime calls = %d, want zero without verified owner identity", executor.calls)
	}
	if plan.ExecutionResult == nil ||
		!strings.Contains(plan.ExecutionResult.BlockedReason, "verified owner identity") {
		t.Fatalf("execution result = %#v, want identity block", plan.ExecutionResult)
	}
}

func TestRunToolTaskUsesLaunchEventURIAsRuntimeEvidence(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	verifier := &sequencedVerificationService{}
	service := NewServiceWithEngines(mem, llmService, nil, verifier, executor)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests and verify exact runtime evidence",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "validated" {
		t.Fatalf("status = %q, want validated: %#v", plan.CompletionStatus, plan.ValidationResult)
	}
	if len(verifier.requests) == 0 {
		t.Fatalf("verification service was not called")
	}
	foundRuntimeEvidence := false
	for _, evidence := range verifier.requests[len(verifier.requests)-1].ExternalEvidence {
		if evidence.SourceType == "controlled_runtime" {
			foundRuntimeEvidence = true
			if evidence.SourceID != executor.result.LaunchEventID || evidence.SourceURI != "automation-launch://"+executor.result.LaunchEventID {
				t.Fatalf("runtime evidence did not use launch event id: %#v", evidence)
			}
			if !strings.Contains(evidence.Snippet, "runtime=openclaw") || !strings.Contains(evidence.Snippet, "skills=autoreview, gitcrawl") {
				t.Fatalf("runtime route trace missing from evidence snippet: %#v", evidence)
			}
		}
	}
	if !foundRuntimeEvidence {
		t.Fatalf("controlled runtime evidence missing: %#v", verifier.requests[len(verifier.requests)-1].ExternalEvidence)
	}
}

func TestRunToolTaskBlocksNilRuntimeResult(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{}
	service := NewServiceWithEngines(mem, llmService, nil, nil, executor)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests for the project",
		ProjectKey:     "018-HAI",
		AutomationID:   uuid.NewString(),
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "review_required" || plan.ExecutionResult == nil {
		t.Fatalf("nil runtime result was not blocked: %#v", plan)
	}
	if plan.ExecutionResult.BlockedReason != "controlled runtime execution returned no result" {
		t.Fatalf("blocked reason = %q", plan.ExecutionResult.BlockedReason)
	}
}

func TestValidationRetryReusesSuccessfulRuntimeExecution(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	verifier := &sequencedVerificationService{
		statuses: []string{verification.StatusNeedsReview, verification.StatusSourceSupported},
	}
	service := NewServiceWithEngines(mem, llmService, nil, verifier, executor)

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests and verify the result",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "validated" || plan.RetryPolicy.CurrentAttempt != 2 {
		t.Fatalf("retry did not validate: status=%q retry=%#v validation=%#v", plan.CompletionStatus, plan.RetryPolicy, plan.ValidationResult)
	}
	if executor.calls != 1 {
		t.Fatalf("runtime executed %d times, want exactly once", executor.calls)
	}
	if !hasTaskAction(plan.ExecutionResult.Actions, "automation.launch", "reused") {
		t.Fatalf("expected retry to record reused runtime evidence")
	}
}

func TestValidationRetrySkipsFallbackRoutingWhenRouterBecomesUnavailable(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	verifier := &sequencedVerificationService{
		statuses: []string{verification.StatusNeedsReview, verification.StatusSourceSupported},
	}
	taskService := NewServiceWithEngines(mem, llmService, nil, verifier, executor).(*service)
	verifier.onAnswer = func(call int) {
		if call == 1 {
			taskService.llmService = nil
		}
	}

	plan, err := taskService.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Run local script tests and verify the result",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.CompletionStatus != "validated" || plan.RetryPolicy.CurrentAttempt != 2 {
		t.Fatalf("retry did not finish safely: status=%q retry=%#v validation=%#v", plan.CompletionStatus, plan.RetryPolicy, plan.ValidationResult)
	}
	if !hasTaskEvent(plan.Events, "routing", "fallback model route skipped because the task LLM router is not configured") {
		t.Fatalf("missing fallback-skip audit event: %#v", plan.Events)
	}
}

func TestApprovedReviewIsNotFalselyCompletedWhenRuntimeStillBlocked(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	service := NewService(mem, llmService)
	plan, err := service.Run(IntakeRequest{
		Request:      "Delete account data by running a local script",
		ProjectKey:   "018-HAI",
		AutomationID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	result, err := service.ResolveReviewItem(plan.ReviewQueueItem.ID, ApprovalDecision{
		Approved: true,
		Note:     "Approved only for the configured controlled runtime.",
	})
	if err != nil {
		t.Fatalf("ResolveReviewItem: %v", err)
	}
	if result.Item.Status != "needs_review" {
		t.Fatalf("review status = %q, want needs_review", result.Item.Status)
	}
	if result.Plan == nil || result.Plan.CompletionStatus != "review_required" {
		t.Fatalf("blocked approved task was falsely completed: %#v", result.Plan)
	}
	if queue := service.ReviewQueue(); len(queue) != 1 {
		t.Fatalf("blocked approval created duplicate review items: %#v", queue)
	}
	if _, err := service.ResolveReviewItem(result.Item.ID, ApprovalDecision{Approved: false, Note: "Reject until runtime is configured."}); err != nil {
		t.Fatalf("needs_review item could not be resolved again: %v", err)
	}
}

func TestApprovedReviewPassesExactReviewItemAsApprovalSource(t *testing.T) {
	mem := &fakeMemoryService{}
	llmService := newTaskTestLLMService(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(mem, llmService, nil, nil, executor)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Delete account data by running a local script",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.ReviewQueueItem == nil || executor.calls != 0 {
		t.Fatalf("initial run bypassed review: item=%#v calls=%d", plan.ReviewQueueItem, executor.calls)
	}

	result, err := service.ResolveReviewItem(plan.ReviewQueueItem.ID, ApprovalDecision{
		Approved: true,
		Note:     "Approved this exact controlled action.",
	})
	if err != nil {
		t.Fatalf("ResolveReviewItem: %v", err)
	}
	if result.Plan == nil || executor.calls != 1 || len(executor.requests) != 1 {
		t.Fatalf("approved review did not execute exactly once: result=%#v calls=%d requests=%#v", result, executor.calls, executor.requests)
	}
	expectedSource := "task-review:" + plan.ReviewQueueItem.ID
	if executor.requests[0].ApprovalSourceID != expectedSource {
		t.Fatalf("approval source = %q, want %q", executor.requests[0].ApprovalSourceID, expectedSource)
	}
}

func newTaskTestLLMService(t *testing.T) *llm.Service {
	t.Helper()
	t.Setenv("LLM_PROVIDERS_JSON", "")
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	t.Setenv("OLLAMA_BASE_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_MODEL_IDS", "qwen2.5-coder:32b,qwen2.5-coder:7b,gemma3:4b")
	t.Setenv("LM_STUDIO_BASE_URL", "")
	t.Setenv("FREE_CLOUD_OPENAI_BASE_URL", "")
	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	return llmService
}

type fakeFrameworkSelector struct {
	decision *frameworkregistry.SelectionDecision
	err      error
	calls    int
}

func (f *fakeFrameworkSelector) PlanSelection(request frameworkregistry.SelectionRequest) (*frameworkregistry.SelectionDecision, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.decision == nil {
		return nil, fmt.Errorf("fake framework decision is not configured")
	}
	copied := *f.decision
	copied.TaskPlanID = request.TaskPlanID
	return &copied, nil
}

type fakeMemoryService struct {
	ownerCreateOwners   []string
	ownerRetrieveOwners []string
	lastRetrieveContext context.Context
	memories            map[uuid.UUID]models.ContextMemory
}

type fakeToolExecutor struct {
	result   *ToolExecutionResult
	err      error
	calls    int
	requests []ToolExecutionRequest
}

func (f *fakeToolExecutor) Execute(request ToolExecutionRequest) (*ToolExecutionResult, error) {
	f.calls++
	f.requests = append(f.requests, request)
	return f.result, f.err
}

func completedToolResult() *ToolExecutionResult {
	launchEventID := uuid.NewString()
	return &ToolExecutionResult{
		AutomationID:  uuid.NewString(),
		LaunchEventID: launchEventID,
		RuntimeType:   "script",
		LaunchType:    "script",
		Target:        "verify-project.sh",
		Status:        "completed",
		Message:       "script completed",
		Output:        "build and tests passed",
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
			RuntimeID:         "openclaw",
			Intent:            "code_review",
			ExecutionMode:     "read_only",
			RiskLevel:         "medium",
			RecommendedSkills: []string{"autoreview", "gitcrawl"},
			BlockedSurfaces:   []string{"external_message_sending"},
		},
		ExitCode:    0,
		DurationMs:  25,
		AuditEvents: []string{"script executed without shell"},
		ExecutedAt:  time.Now().UTC(),
	}
}

type sequencedVerificationService struct {
	statuses         []string
	pursuitLinkError string
	onAnswer         func(call int)
	calls            int
	requests         []verification.AnswerRequest
}

func (s *sequencedVerificationService) Answer(request verification.AnswerRequest) (*verification.VerificationResult, error) {
	s.requests = append(s.requests, request)
	s.calls++
	if s.onAnswer != nil {
		s.onAnswer(s.calls)
	}
	status := verification.StatusSourceSupported
	if s.calls <= len(s.statuses) {
		status = s.statuses[s.calls-1]
	}
	run := models.VerificationRun{
		ID:       uuid.New(),
		Answer:   "controlled runtime evidence checked",
		Status:   status,
		Question: request.Question,
	}
	claims := []models.VerificationClaim{{
		ID:          uuid.New(),
		ClaimText:   "controlled runtime evidence checked",
		Status:      status,
		Confidence:  0.9,
		NeedsReview: status == verification.StatusNeedsReview,
	}}
	result := &verification.VerificationResult{Run: run, Claims: claims, PursuitLinkError: s.pursuitLinkError}
	if status == verification.StatusNeedsReview {
		result.UnsupportedClaims = claims
	}
	return result, nil
}

func (s *sequencedVerificationService) Runs() ([]models.VerificationRun, error) {
	return nil, nil
}

func (s *sequencedVerificationService) RunsForOwner(string) ([]models.VerificationRun, error) {
	return nil, nil
}

func (s *sequencedVerificationService) RunDetails(id uuid.UUID) (*verification.VerificationResult, error) {
	return nil, nil
}

func (s *sequencedVerificationService) RunDetailsForOwner(string, uuid.UUID) (*verification.VerificationResult, error) {
	return nil, nil
}

func hasTaskAction(actions []ExecutedAction, name, status string) bool {
	for _, action := range actions {
		if action.Name == name && action.Status == status {
			return true
		}
	}
	return false
}

func hasTaskEvent(events []TaskEvent, stage, message string) bool {
	for _, item := range events {
		if item.Stage == stage && item.Message == message {
			return true
		}
	}
	return false
}

func (f *fakeMemoryService) Create(request memory.CreateRequest) (*models.ContextMemory, error) {
	created := &models.ContextMemory{
		ID:         uuid.New(),
		ProjectKey: request.ProjectKey,
		Kind:       request.Kind,
		Content:    request.Content,
		Summary:    request.Summary,
		Confidence: request.Confidence,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if f.memories == nil {
		f.memories = map[uuid.UUID]models.ContextMemory{}
	}
	f.memories[created.ID] = *created
	return created, nil
}

func (f *fakeMemoryService) CreateForOwner(ownerIdentity string, request memory.CreateRequest) (*models.ContextMemory, error) {
	f.ownerCreateOwners = append(f.ownerCreateOwners, ownerIdentity)
	created, err := f.Create(request)
	if created != nil {
		created.OwnerIdentity = ownerIdentity
		f.memories[created.ID] = *created
	}
	return created, err
}

type fakePursuitAttemptRecorder struct {
	attempts     []models.PursuitTaskAttempt
	reservations []string
	settlements  []string
	reserveErr   error
	settleErr    error
}

func (f *fakePursuitAttemptRecorder) UpsertTaskAttempt(attempt models.PursuitTaskAttempt) error {
	f.attempts = append(f.attempts, attempt)
	return nil
}

func (f *fakePursuitAttemptRecorder) ReservePursuitTaskResources(_ uuid.UUID, _ string, operationID string, _, _ int64) error {
	f.reservations = append(f.reservations, operationID)
	return f.reserveErr
}

func (f *fakePursuitAttemptRecorder) SettlePursuitTaskResources(_ uuid.UUID, _ string, operationID, disposition string, _, _ int64) error {
	f.settlements = append(f.settlements, operationID+":"+disposition)
	return f.settleErr
}

type fakePursuitTaskGuard struct {
	fakePursuitAttemptRecorder
	err   error
	calls int
}

func (f *fakePursuitTaskGuard) ValidatePursuitTaskAttempt(uuid.UUID, string) error {
	f.calls++
	return f.err
}

type fakeTaskSourceService struct {
	refreshCalls       int
	ownerRefreshOwners []string
	searchCalls        int
	order              []string
	searchRequests     []source.SearchRequest
	calendarBusy       []source.CalendarBusyInterval
	calendarBusyErr    error
	calendarOwner      string
	calendarStart      time.Time
	calendarEnd        time.Time
	refreshStarted     chan struct{}
}

func (s *fakeTaskSourceService) CalendarBusyIntervalsForOwner(ownerIdentity string, start, end time.Time) ([]source.CalendarBusyInterval, error) {
	s.calendarOwner, s.calendarStart, s.calendarEnd = ownerIdentity, start, end
	return append([]source.CalendarBusyInterval(nil), s.calendarBusy...), s.calendarBusyErr
}

func (s *fakeTaskSourceService) Connectors() ([]models.SourceConnector, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) CreateSource(request source.CreateSourceRequest) (*models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) UpdateSource(id uuid.UUID, request source.UpdateSourceRequest) (*models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) Sources(includeDisabled bool) ([]models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) SourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) SyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) SyncJobsForOwner(ownerIdentity string, limit int) ([]models.SourceSyncJob, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) Sync(sourceID uuid.UUID, request source.ImportRequest) (*source.SyncResult, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) SyncContext(context.Context, uuid.UUID, source.ImportRequest) (*source.SyncResult, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) StartGoogleOAuth(sourceID uuid.UUID) (string, error) {
	return "", nil
}

func (s *fakeTaskSourceService) CompleteGoogleOAuth(ctx context.Context, code, state, ownerIdentity string) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (s *fakeTaskSourceService) DueSources(now time.Time) ([]models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) RunDueScheduledSyncs(now time.Time) (*source.ScheduledSyncRun, error) {
	s.refreshCalls++
	s.order = append(s.order, "refresh")
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func (s *fakeTaskSourceService) RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*source.ScheduledSyncRun, error) {
	s.ownerRefreshOwners = append(s.ownerRefreshOwners, ownerIdentity)
	s.order = append(s.order, "owner-refresh")
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func (s *fakeTaskSourceService) RunDueScheduledSyncsForOwnerContext(ctx context.Context, now time.Time, ownerIdentity string) (*source.ScheduledSyncRun, error) {
	if s.refreshStarted == nil {
		return s.RunDueScheduledSyncsForOwner(now, ownerIdentity)
	}
	close(s.refreshStarted)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *fakeTaskSourceService) Reindex(sourceID uuid.UUID) (*source.SyncResult, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) Pause(sourceID uuid.UUID, paused bool) (*models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) Revoke(sourceID uuid.UUID) (*models.ConnectedSource, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) Search(request source.SearchRequest) (*source.SearchResult, error) {
	s.searchCalls++
	s.order = append(s.order, "search")
	s.searchRequests = append(s.searchRequests, request)
	return &source.SearchResult{
		Query: request.Query,
		UsedContext: []source.RankedExtraction{
			{
				Extraction: models.SourceExtraction{
					ID:          uuid.New(),
					ProjectKey:  request.ProjectKey,
					ContentType: "local_file_md",
					Summary:     "Local project files describe scheduled ingestion and approval-gated execution.",
					SourceLabel: "project-note.md",
				},
				Score:       0.92,
				Explanation: "same project, source linked",
			},
		},
		Explanation: "fake source context retrieved",
	}, nil
}

func (s *fakeTaskSourceService) Extractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) ExtractionsForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) ExtractionsForOwnerLimit(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) ExtractionForOwner(ownerIdentity string, id uuid.UUID) (*models.SourceExtraction, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *fakeTaskSourceService) UpdateExtraction(id uuid.UUID, request models.SourceExtraction) (*models.SourceExtraction, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) ArchiveExtraction(id uuid.UUID, archived bool) (*models.SourceExtraction, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) DeleteExtraction(id uuid.UUID) error {
	return nil
}

func (s *fakeTaskSourceService) AuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	return nil, nil
}

func (s *fakeTaskSourceService) AuditLogsForOwner(ownerIdentity string, limit int) ([]models.SourceAuditLog, error) {
	return nil, nil
}

func (fakeMemoryService) Update(id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (fakeMemoryService) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (f *fakeMemoryService) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	item, ok := f.memories[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copy := item
	return &copy, nil
}

func (fakeMemoryService) Archive(id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (fakeMemoryService) Delete(id uuid.UUID) error {
	return nil
}

func (f *fakeMemoryService) Retrieve(request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	now := time.Now().UTC()
	item := models.ContextMemory{
		ID: uuid.New(), ProjectKey: request.ProjectKey, Kind: "project",
		Summary:    "Completion-first routing prefers validated completion before cost minimization.",
		Confidence: 0.9, CreatedAt: now, UpdatedAt: now,
	}
	if f.memories == nil {
		f.memories = map[uuid.UUID]models.ContextMemory{}
	}
	f.memories[item.ID] = item
	return &memory.RetrieveResult{
		Query: request.Query,
		UsedContext: []memory.RankedMemory{
			{
				Memory:      item,
				Score:       0.9,
				Explanation: "same project, high relevance",
			},
		},
		Explanation: "retrieved fake context",
	}, nil
}

func (f *fakeMemoryService) UpdateForOwner(_ string, id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return f.Update(id, request)
}

func (f *fakeMemoryService) FindAllForOwner(_ string, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return f.FindAll(projectKey, includeArchived)
}

func (f *fakeMemoryService) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	item, err := f.FindByID(id)
	if err != nil || item.OwnerIdentity != ownerIdentity {
		return nil, gorm.ErrRecordNotFound
	}
	return item, nil
}

func (f *fakeMemoryService) ArchiveForOwner(_ string, id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return f.Archive(id, archived)
}

func (f *fakeMemoryService) DeleteForOwner(_ string, id uuid.UUID) error {
	return f.Delete(id)
}

func (f *fakeMemoryService) RetrieveForOwner(ownerIdentity string, request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	f.ownerRetrieveOwners = append(f.ownerRetrieveOwners, ownerIdentity)
	result, err := f.Retrieve(request)
	if err != nil {
		return nil, err
	}
	for index := range result.UsedContext {
		result.UsedContext[index].Memory.OwnerIdentity = ownerIdentity
		f.memories[result.UsedContext[index].Memory.ID] = result.UsedContext[index].Memory
	}
	return result, nil
}

func (f *fakeMemoryService) RetrieveForOwnerContext(ctx context.Context, ownerIdentity string, request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	f.lastRetrieveContext = ctx
	return f.RetrieveForOwner(ownerIdentity, request)
}
