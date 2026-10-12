package workflowtask

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

func TestRunnerUnknownToolOutcomeOverridesCompletionAndApproval(t *testing.T) {
	for _, test := range []struct {
		name               string
		status             string
		executionUncertain bool
		toolUncertain      bool
		requiresApproval   bool
	}{
		{name: "explicit execution uncertainty", status: "completed", executionUncertain: true},
		{name: "explicit tool uncertainty", status: "completed", toolUncertain: true},
		{name: "indeterminate receipt", status: "indeterminate"},
		{name: "failed receipt", status: "failed"},
		{name: "nonterminal receipt", status: "running"},
		{name: "empty receipt status"},
		{name: "unknown receipt with approval", status: "indeterminate", requiresApproval: true},
		{name: "completed receipt with approval", status: "completed", requiresApproval: true},
		{name: "explicit uncertainty overrides blocked", status: "blocked", toolUncertain: true, requiresApproval: true},
		{name: "explicit uncertainty overrides approval", status: "needs_approval", executionUncertain: true, requiresApproval: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			launchID := uuid.NewString()
			trace := &models.AutomationRuntimeRouteTrace{RuntimeID: "bounded-runtime"}
			plan := validWorkflowCompletionPlan("unknown-tool-plan")
			plan.ExecutionResult = &task.ExecutionResult{
				OutcomeUncertain:   test.executionUncertain,
				VerificationStatus: "verified",
				Output:             "retained bounded task output",
				BlockedReason:      "runtime audit needs reconciliation",
				ToolExecution: &task.ToolExecutionResult{
					Status:             test.status,
					OutcomeUncertain:   test.toolUncertain,
					RequiresApproval:   test.requiresApproval,
					LaunchEventID:      launchID,
					RuntimeType:        "controlled-runtime",
					RuntimeTaskID:      "runtime-task-1",
					ExecutionReference: "runtime-operation-1",
					RuntimeRouteTrace:  trace,
					AuditEvents:        []string{"bounded runtime launch audit"},
				},
			}
			tasks := &capturingTaskService{plan: plan}
			result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "alice",
				WorkflowID:    "workflow-unknown-tool",
				Request:       "Run the controlled task",
			})
			if err != nil {
				t.Fatalf("RunWorkflowTask: %v", err)
			}
			assertUncertainWorkflowTaskResult(t, result)
			if result.ExternalActionExecuted != (test.status == "completed") {
				t.Fatalf("reported completed-action flag changed: %#v", result)
			}
			if result.PlanID != plan.ID || result.RuntimeEvidenceURI != "automation-launch://"+launchID ||
				result.RuntimeEvidenceLabel != "controlled-runtime" || result.RuntimeRouteTrace != trace ||
				result.Output != plan.ExecutionResult.Output || result.FrameworkSelection == nil {
				t.Fatalf("partial plan/receipt/route evidence was lost: %#v", result)
			}
			if !strings.Contains(result.FailureReason, plan.ExecutionResult.BlockedReason) {
				t.Fatalf("execution blocker was lost: %q", result.FailureReason)
			}
			if tasks.runs != 1 || tasks.previews != 1 || tasks.previewRequest.ExecuteAllowed || tasks.previewRequest.HumanApproved {
				t.Fatalf("side-effect-free preview or single-run boundary changed: %#v", tasks)
			}
			if !plan.ValidationResult.Passed || plan.CompletionStatus != "validated" ||
				plan.ExecutionResult.ToolExecution.RuntimeTaskID != "runtime-task-1" ||
				len(plan.ExecutionResult.ToolExecution.AuditEvents) != 1 {
				t.Fatal("adapter rewrote the task plan or its launch audit")
			}
		})
	}
}

func TestRunnerLegacyActionOnlyOutcomeUncertainty(t *testing.T) {
	for _, status := range []string{"indeterminate", "failed", "running", "uncertain", "", " FAILED "} {
		t.Run("launch status "+status, func(t *testing.T) {
			plan := validWorkflowCompletionPlan("legacy-action-plan")
			plan.ExecutionResult = &task.ExecutionResult{
				VerificationStatus: "test_passed",
				Actions: []task.ExecutedAction{
					{Name: "source.search", Status: "completed"},
					{Name: "automation.launch", Status: status},
				},
			}
			result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "alice",
				WorkflowID:    "legacy-workflow",
				Request:       "Review legacy task evidence",
			})
			if err != nil {
				t.Fatalf("RunWorkflowTask: %v", err)
			}
			assertUncertainWorkflowTaskResult(t, result)
			if result.ExternalActionExecuted || result.RuntimeEvidenceURI != "" {
				t.Fatalf("legacy unknown outcome invented execution proof: %#v", result)
			}
		})
	}
}

func TestRunnerReceiptFreeExplicitExecutionUncertainty(t *testing.T) {
	plan := validWorkflowCompletionPlan("receipt-free-plan")
	plan.ExecutionResult = &task.ExecutionResult{
		OutcomeUncertain:   true,
		VerificationStatus: "verified",
		Actions:            []task.ExecutedAction{{Name: "automation.launch", Status: "blocked"}},
	}
	result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "alice",
		WorkflowID:    "receipt-free-workflow",
		Request:       "Reconcile task execution",
	})
	if err != nil {
		t.Fatalf("RunWorkflowTask: %v", err)
	}
	assertUncertainWorkflowTaskResult(t, result)
	if result.RuntimeEvidenceURI != "" || result.ExternalActionExecuted {
		t.Fatalf("execution uncertainty invented a receipt or confirmed effect: %#v", result)
	}
}

func TestRunnerUncertainReceiptRetainsRouteAlongsideEvidenceAndRunErrors(t *testing.T) {
	for _, test := range []struct {
		name          string
		launchID      string
		status        string
		runErr        error
		expectedError string
	}{
		{name: "missing uncertain receipt", status: "indeterminate"},
		{name: "invalid receipt", status: "indeterminate", launchID: "not-a-uuid", expectedError: "invalid launch-event evidence ID"},
		{name: "nil UUID receipt", status: "indeterminate", launchID: uuid.Nil.String(), expectedError: "invalid launch-event evidence ID"},
		{name: "completed without immutable evidence", status: "completed", expectedError: "no immutable launch-event evidence"},
		{name: "valid partial result plus run error", status: "indeterminate", launchID: uuid.NewString(), runErr: errors.New("response lost after dispatch"), expectedError: "response lost after dispatch"},
		{name: "invalid receipt plus run error", status: "indeterminate", launchID: "invalid", runErr: errors.New("response lost after dispatch"), expectedError: "invalid launch-event evidence ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := &models.AutomationRuntimeRouteTrace{RuntimeID: "receipt-runtime"}
			plan := validWorkflowCompletionPlan("partial-receipt-plan")
			plan.ExecutionResult = &task.ExecutionResult{
				OutcomeUncertain:   true,
				VerificationStatus: "verified",
				ToolExecution: &task.ToolExecutionResult{
					Status:            test.status,
					LaunchEventID:     test.launchID,
					RuntimeType:       "receipt-runtime",
					RuntimeRouteTrace: trace,
				},
			}
			result, err := NewRunner(&capturingTaskService{plan: plan, err: test.runErr}).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "alice",
				WorkflowID:    "partial-receipt-workflow",
				Request:       "Reconcile the launch evidence",
			})
			if test.expectedError == "" {
				if err != nil {
					t.Fatalf("RunWorkflowTask: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.expectedError) || workflow.IsTaskFailureSafeNoSideEffect(err) {
				t.Fatalf("error = %v, want unmarked %q", err, test.expectedError)
			}
			if test.runErr != nil && (err == nil || !strings.Contains(err.Error(), test.runErr.Error())) {
				t.Fatalf("task service error was lost: %v", err)
			}
			assertUncertainWorkflowTaskResult(t, result)
			if result.RuntimeRouteTrace != trace || result.RuntimeEvidenceLabel != "receipt-runtime" || result.PlanID != plan.ID {
				t.Fatalf("receipt-free/invalid route evidence was lost: %#v", result)
			}
			if id, parseErr := uuid.Parse(test.launchID); parseErr == nil && id != uuid.Nil {
				if result.RuntimeEvidenceURI != "automation-launch://"+id.String() {
					t.Fatalf("valid launch evidence was lost: %#v", result)
				}
			} else if result.RuntimeEvidenceURI != "" {
				t.Fatalf("immutable evidence URI was fabricated: %#v", result)
			}
		})
	}
}

func TestRunnerKnownOutcomesAndSourceUncertaintyKeepTheirMeaning(t *testing.T) {
	for _, test := range []struct {
		name         string
		tool         *task.ToolExecutionResult
		actions      []task.ExecutedAction
		verification string
		approval     bool
	}{
		{name: "completed receipt", tool: &task.ToolExecutionResult{Status: "completed", LaunchEventID: uuid.NewString()}, verification: "verified"},
		{name: "blocked receipt", tool: &task.ToolExecutionResult{Status: "blocked", RequiresApproval: true}, verification: "needs_review", approval: true},
		{name: "approval receipt", tool: &task.ToolExecutionResult{Status: "needs_approval", RequiresApproval: true}, verification: "needs_review", approval: true},
		{name: "source uncertainty is not tool uncertainty", verification: "uncertain"},
		{name: "non-tool failure", actions: []task.ExecutedAction{{Name: "source.search", Status: "failed"}}, verification: "needs_review"},
		{name: "known legacy actions", actions: []task.ExecutedAction{
			{Name: "automation.launch", Status: "completed"},
			{Name: "automation.launch", Status: "reused"},
			{Name: "automation.launch", Status: "blocked"},
			{Name: "automation.launch", Status: "needs_approval"},
		}, verification: "verified"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := validWorkflowCompletionPlan("known-outcome-plan")
			if test.approval {
				plan.CompletionStatus = "review_required"
				plan.ValidationResult.Passed = false
			}
			plan.ExecutionResult = &task.ExecutionResult{
				ToolExecution:      test.tool,
				Actions:            test.actions,
				VerificationStatus: test.verification,
			}
			result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "alice",
				WorkflowID:    "known-outcome-workflow",
				Request:       "Continue bounded task work",
			})
			if err != nil {
				t.Fatalf("RunWorkflowTask: %v", err)
			}
			if result == nil || result.ExecutionOutcomeUncertain || result.ApprovalRequired != test.approval ||
				result.CompletionStatus != plan.CompletionStatus || result.VerificationStatus != test.verification ||
				result.Passed != plan.ValidationResult.Passed {
				t.Fatalf("known execution/source semantics changed: %#v", result)
			}
		})
	}
}

func assertUncertainWorkflowTaskResult(t *testing.T, result *workflow.TaskRunResult) {
	t.Helper()
	if result == nil || !result.ExecutionOutcomeUncertain || result.Passed || !result.ReviewRequired || result.ApprovalRequired ||
		result.CompletionStatus != "review_required" || result.VerificationStatus != "uncertain" ||
		!strings.Contains(result.FailureReason, "reconcile possible side effects") {
		t.Fatalf("unknown outcome was not retained for execution review: %#v", result)
	}
}
