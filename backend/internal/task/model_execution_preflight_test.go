package task

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type modelPreflightExecutor struct {
	*fakeToolExecutor
	readOnly    bool
	err         error
	inspections int
}

func (e *modelPreflightExecutor) IsReadOnlyRuntime(string) (bool, error) {
	e.inspections++
	return e.readOnly, e.err
}

func TestModelExecutionPreflightCannotSubstituteUnknownRuntimeForReasoning(t *testing.T) {
	for _, name := range []string{"no inspector", "typed nil inspector", "write runtime", "metadata error", "approval required", "risk blocked", "reasoning only", "model selected", "read-only runtime"} {
		t.Run(name, func(t *testing.T) {
			executor := &modelPreflightExecutor{fakeToolExecutor: &fakeToolExecutor{}, readOnly: true}
			s := &service{toolExecutor: executor}
			plan := &CompletionPlan{Intake: IntakeAnalysis{NeedsTools: true}, RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true}}
			wantBlock := true
			switch name {
			case "no inspector":
				s.toolExecutor = executor.fakeToolExecutor
			case "typed nil inspector":
				s.toolExecutor = (*modelPreflightExecutor)(nil)
			case "write runtime":
				executor.readOnly = false
			case "metadata error":
				executor.err = errors.New("postgres://operator:synthetic-secret@example.invalid/db")
			case "approval required":
				plan.RiskAssessment.ApprovalRequired = true
			case "risk blocked":
				plan.RiskAssessment.AllowedNow = false
			case "reasoning only":
				plan.Intake.NeedsTools = false
			case "model selected":
				plan.ModelDecision = llm.RouteDecision{SelectedModelID: "eligible-test-model"}
				wantBlock = false
			case "read-only runtime":
				wantBlock = false
			}
			reason := s.modelExecutionPreflightBlocker(plan, IntakeRequest{AutomationID: "test-runtime"})
			if (reason != "") != wantBlock || strings.Contains(reason, "synthetic-secret") {
				t.Fatalf("blocker = %q, want blocked=%t", reason, wantBlock)
			}
			if executor.calls != 0 {
				t.Fatal("preflight dispatched an action")
			}
			if (name == "approval required" || name == "risk blocked" || name == "reasoning only" || name == "model selected") && executor.inspections != 0 {
				t.Fatal("non-exempt path used runtime metadata")
			}
		})
	}
}

func TestFallbackExecutionRechecksMissingModelBeforeToolDispatch(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	s := &service{toolExecutor: executor}
	plan := &CompletionPlan{Intake: IntakeAnalysis{NeedsTools: true}, RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true}}
	result := s.executeAllowedSteps(plan, IntakeRequest{ExecuteAllowed: true, AutomationID: executor.result.AutomationID})
	if executor.calls != 0 || result.ToolExecution != nil || !strings.Contains(result.BlockedReason, "no capable model") {
		t.Fatalf("fallback route dispatched without a capable model: calls=%d result=%#v", executor.calls, result)
	}
}

func TestFallbackModelBlockRetainsPriorExternalActionReceipt(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	s := &service{toolExecutor: executor}
	prior := executor.result
	plan := &CompletionPlan{Intake: IntakeAnalysis{NeedsTools: true}, RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true},
		ExecutionResult: &ExecutionResult{ToolExecution: prior, Actions: []ExecutedAction{{Name: "automation.launch", Status: "completed"}}}}
	result := s.executeAllowedSteps(plan, IntakeRequest{ExecuteAllowed: true, AutomationID: prior.AutomationID})
	if executor.calls != 0 || result.ToolExecution != prior || result.ToolExecution.LaunchEventID != prior.LaunchEventID ||
		len(result.Actions) != 2 || !strings.Contains(result.Output, "prior runtime evidence") || strings.Contains(result.Output, "before dispatch") {
		t.Fatalf("model block discarded or misrepresented a previous effect: %#v", result)
	}
}

func TestCompletedReadOnlyReceiptCanBeReusedWithoutRelaunchOrNewModel(t *testing.T) {
	tool := deterministicReadOnlyToolExecution()
	plan := &CompletionPlan{ExecutionResult: &ExecutionResult{ToolExecution: tool}}
	s := &service{}
	if reason := s.modelExecutionPreflightBlocker(plan, IntakeRequest{}); reason != "" {
		t.Fatalf("past immutable evidence was mistaken for a new model or runtime request: %s", reason)
	}
}

func TestModelFreeWriteRuntimeBlocksBeforeEffectsAndAutomaticRetry(t *testing.T) {
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post", "methodless", "script", "agent", "foreign record", "missing record", "invalid id", "repository error", "absent inspector", "typed nil inspector"} {
		t.Run(name, func(t *testing.T) {
			tool := deterministicReadOnlyToolExecution()
			reader := &readOnlyRuntimeTestReader{record: &models.Automation{ID: uuid.MustParse(tool.AutomationID), LaunchType: "api", LaunchTarget: "GET http://backend/readyz"}}
			var executor ToolExecutor = NewAutomationToolExecutor(reader)
			fallback := &fakeToolExecutor{result: tool}
			switch name {
			case "post":
				reader.record.LaunchTarget = "POST http://backend/readyz"
			case "methodless":
				reader.record.LaunchTarget = "http://backend/readyz"
			case "script", "agent":
				reader.record.LaunchType = name
			case "foreign record":
				reader.record.ID = uuid.New()
			case "missing record":
				reader.record = nil
			case "invalid id":
				tool.AutomationID = "invalid-automation"
			case "repository error":
				reader.err = errors.New("postgres://operator:synthetic-secret@example.invalid/db")
			case "absent inspector":
				executor = fallback
			case "typed nil inspector":
				executor = (*modelPreflightExecutor)(nil)
			}
			svc := NewServiceWithEnginesAndPursuitAttempts(&fakeMemoryService{}, newTaskNoProviderLLMService(t), nil,
				&sequencedVerificationService{}, executor, nil, selector)
			plan, err := svc.Run(IntakeRequest{OwnerIdentity: "operator@example.test", Request: "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.", AutomationID: tool.AutomationID, ExecuteAllowed: true})
			if err != nil {
				t.Fatal(err)
			}
			reader.assertUnused(t)
			if plan.CompletionStatus != "review_required" || plan.ExecutionResult == nil ||
				!strings.Contains(plan.ExecutionResult.BlockedReason, "no capable model") || strings.Contains(plan.ExecutionResult.BlockedReason, "synthetic-secret") ||
				plan.RetryPolicy.RetryAvailable || plan.ValidationResult.Passed || fallback.calls != 0 ||
				plan.FrameworkEvidencePreflight != nil || plan.ReviewQueueItem == nil {
				t.Fatalf("write runtime was admitted or retried: %#v", plan)
			}
		})
	}
}

func TestModelFreeAdmissionInspectsActualConfiguredGetAndHeadWithoutDispatch(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			id := uuid.New()
			reader := &readOnlyRuntimeTestReader{record: &models.Automation{ID: id, LaunchType: "api", LaunchTarget: method + " http://backend/readyz"}}
			s := &service{toolExecutor: NewAutomationToolExecutor(reader)}
			plan := &CompletionPlan{Intake: IntakeAnalysis{NeedsTools: true}, RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true}}
			if reason := s.modelExecutionPreflightBlocker(plan, IntakeRequest{AutomationID: id.String()}); reason != "" {
				t.Fatal(reason)
			}
			if reader.readCalls != 1 || reader.requested != id {
				t.Fatal("configured action was not inspected")
			}
			reader.assertUnused(t)
		})
	}
}

type changedModelRuntimeReader struct{ readOnlyRuntimeTestReader }

func (r *changedModelRuntimeReader) FindByID(id uuid.UUID) (*models.Automation, error) {
	if r.readCalls > 0 {
		r.record.LaunchTarget = "POST http://backend/readyz"
	}
	return r.readOnlyRuntimeTestReader.FindByID(id)
}

func (r *changedModelRuntimeReader) InspectReviewConfiguration(id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	// Mock the new pending-review read separately from the two preflight probes.
	snapshot := historicalReviewSnapshot(id)
	snapshot.Scope = automation.ApprovalScopeAPIMutate
	return snapshot, nil
}

func TestModelFreeRuntimeMetadataIsRecheckedBeforeDispatch(t *testing.T) {
	id := uuid.New()
	reader := &changedModelRuntimeReader{readOnlyRuntimeTestReader: readOnlyRuntimeTestReader{record: &models.Automation{
		ID: id, LaunchType: "api", LaunchTarget: "GET http://backend/readyz",
	}}}
	selector, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewServiceWithEnginesAndPursuitAttempts(&fakeMemoryService{}, newTaskNoProviderLLMService(t), nil,
		&sequencedVerificationService{}, NewAutomationToolExecutor(reader), nil, selector)
	plan, err := svc.Run(IntakeRequest{OwnerIdentity: "operator@example.test", Request: "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.", AutomationID: id.String(), ExecuteAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	reader.assertUnused(t)
	if reader.readCalls != 2 || plan.ExecutionResult == nil || !strings.Contains(plan.ExecutionResult.BlockedReason, "registered GET or HEAD") ||
		plan.CompletionStatus != "review_required" || plan.RetryPolicy.RetryAvailable {
		t.Fatalf("changed metadata reached runtime dispatch: reads=%d result=%#v", reader.readCalls, plan)
	}
}
