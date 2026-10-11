package workflowtask

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

func TestRunnerPreservesTaskApprovalBeforeRuntimeDispatch(t *testing.T) {
	plan := approvalOnlyWorkflowCompletionPlan(t, "approval-preflight-plan")
	plan.ValidationResult = task.ValidationResult{Status: "blocked", Failures: []string{
		"approval is required before execution",
	}}
	tasks := &capturingTaskService{plan: plan}
	result, err := NewRunner(tasks).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "operator@example.test", WorkflowID: uuid.NewString(), Request: "Review the proposed action",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.ReviewRequired || !result.ApprovalRequired || result.Passed || result.ExternalActionExecuted {
		t.Fatalf("pre-runtime approval was lost: %#v", result)
	}
	if result.FailureReason != plan.ReviewQueueItem.Reason {
		t.Fatalf("approval reason buried under unexecuted completion checks: %q", result.FailureReason)
	}
}

func TestRunnerDoesNotReplaceReadinessOrExecutionReviewWithApproval(t *testing.T) {
	for _, name := range []string{
		"missing parameters", "missing participants", "missing model", "missing resources",
		"empty resource digest", "blank resource digest", "short resource digest", "invalid resource digest",
		"infeasible resources", "unknown resource feasibility", "missing resource feasibility",
		"invalid resource authority", "resource can execute", "resource grants authority",
		"nonadvisory domain", "domain grants authority", "unavailable capacity", "per-action denial",
		"clarification", "authority ceiling", "already approved", "empty execution result", "runtime started", "not review",
	} {
		t.Run(name, func(t *testing.T) {
			plan := approvalOnlyWorkflowCompletionPlan(t, "review-boundary-plan")
			plan.ValidationResult = task.ValidationResult{Status: "blocked", Failures: []string{"readiness needs review"}}
			switch name {
			case "missing parameters":
				plan.RiskAssessment.MissingParameters = []string{"controlled automation"}
			case "missing participants":
				plan.RiskAssessment.MissingRequiredAgents = []string{"evidence_reviewer"}
			case "missing model":
				plan.ModelDecision.SelectedModelID = ""
			case "missing resources":
				plan.ResourceDecision = nil
			case "empty resource digest":
				plan.ResourceDecision.DecisionDigest = ""
			case "blank resource digest":
				plan.ResourceDecision.DecisionDigest = " \t\n "
			case "short resource digest":
				plan.ResourceDecision.DecisionDigest = strings.Repeat("a", 63)
			case "invalid resource digest":
				plan.ResourceDecision.DecisionDigest = strings.Repeat("z", 64)
			case "infeasible resources":
				plan.ResourceDecision.Feasibility = resourceplanner.Infeasible
			case "unknown resource feasibility":
				plan.ResourceDecision.Feasibility = "unknown"
			case "missing resource feasibility":
				plan.ResourceDecision.Feasibility = ""
			case "invalid resource authority":
				plan.ResourceDecision.Authority = "execution"
			case "resource can execute":
				plan.ResourceDecision.CanExecute = true
			case "resource grants authority":
				plan.ResourceDecision.GrantsAuthority = true
			case "nonadvisory domain":
				plan.DomainPackDecision.AdvisoryOnly = false
			case "domain grants authority":
				plan.DomainPackDecision.ExecutionAuthorityGranted = true
			case "unavailable capacity":
				plan.FrameworkDecision.Capacity.Status = "unavailable"
			case "per-action denial":
				plan.FrameworkDecision.ActionAutonomy = []frameworkregistry.ActionAutonomyDecision{{Action: "execute_case_approved_action", Allowed: false}}
			case "clarification":
				plan.RiskAssessment.ActionResolution = "clarify"
			case "authority ceiling":
				plan.RiskAssessment.RequiredFrameworkAutonomy = 8
				plan.RiskAssessment.FrameworkAutonomyCeiling = 4
			case "already approved":
				plan.RiskAssessment.ApprovalGranted = true
			case "empty execution result":
				plan.ExecutionResult = &task.ExecutionResult{}
			case "runtime started":
				plan.ExecutionResult = &task.ExecutionResult{ToolExecution: &task.ToolExecutionResult{
					Status: "completed", LaunchEventID: uuid.NewString(),
				}}
			case "not review":
				plan.CompletionStatus = "retry_needed"
			}
			result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "operator@example.test", WorkflowID: uuid.NewString(), Request: "Inspect the blocker",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.ApprovalRequired || result.Passed || !strings.Contains(result.FailureReason, "readiness needs review") {
				t.Fatalf("%s became an ordinary approval flow: %#v", name, result)
			}
			if name != "not review" && !result.ReviewRequired {
				t.Fatalf("%s lost review classification: %#v", name, result)
			}
			if name == "runtime started" && !result.ExternalActionExecuted {
				t.Fatal("executed side effect was discarded")
			}
		})
	}
}

func approvalOnlyWorkflowCompletionPlan(t *testing.T, planID string) *task.CompletionPlan {
	t.Helper()
	plan := validWorkflowCompletionPlan(planID)
	plan.CompletionStatus = "review_required"
	plan.ModelDecision.SelectedModelID = "configured-test-model"
	plan.RiskAssessment = task.RiskAssessment{
		ApprovalRequired: true, ActionResolution: "proceed",
		RequiredFrameworkAutonomy: 4, FrameworkAutonomyCeiling: 6,
	}
	plan.ReviewQueueItem = &task.ReviewQueueItem{Reason: "approval required before task execution"}
	plan.DomainPackDecision = &task.DomainPackDecision{AdvisoryOnly: true}
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	zeroCost := int64(0)
	resource, err := resourceplanner.New().Plan(resourceplanner.Request{
		OwnerIdentity: "operator@example.test", PlanID: planID,
		AsOf: start, HorizonStart: start, HorizonEnd: end,
		Tasks: []resourceplanner.Task{{
			ID:        "review-action",
			Duration:  resourceplanner.DurationEstimate{OptimisticMinutes: 10, ExpectedMinutes: 10, PessimisticMinutes: 10},
			Resources: []resourceplanner.ResourceRequirement{{ResourceID: "operator", CapacityUnits: 1}},
			Approval:  resourceplanner.TaskApproval{Required: true, Reasons: []string{"owner approval required"}},
		}},
		Availability: []resourceplanner.CapacityWindow{{ResourceID: "operator", Start: start, End: end, CapacityUnits: 1}},
		Budget:       resourceplanner.Budget{MaxCostMicros: &zeroCost},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Feasibility != resourceplanner.FeasibleWithApprovals || resource.Authority != "advisory_only" ||
		resource.CanExecute || resource.GrantsAuthority || len(resource.DecisionDigest) != 64 {
		t.Fatalf("invalid advisory fixture: %#v", resource)
	}
	plan.ResourceDecision = &resource
	return plan
}

func TestRunnerPreservesNamedMixedReadinessPrerequisites(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parameters []string
		agents     []string
		reason     string
	}{
		{
			name: "controlled automation", parameters: []string{"controlled automation"},
			reason: "missing required execution details: controlled automation",
		},
		{
			name: "evidence reviewer", agents: []string{"evidence_reviewer"},
			reason: "assign and verify required participants before execution: evidence_reviewer",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := approvalOnlyWorkflowCompletionPlan(t, "named-prerequisite-plan")
			plan.RiskAssessment.MissingParameters = tc.parameters
			plan.RiskAssessment.MissingRequiredAgents = tc.agents
			plan.ReviewQueueItem.Reason = tc.reason
			plan.ValidationResult = task.ValidationResult{Status: "blocked", Failures: []string{
				"approval is required before execution", "task execution was not completed", tc.reason,
			}}
			result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
				OwnerIdentity: "operator@example.test", WorkflowID: uuid.NewString(), Request: "Inspect named prerequisites",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.ReviewRequired || result.ApprovalRequired || result.Passed || result.ExternalActionExecuted {
				t.Fatalf("named readiness blocker became approval-only: %#v", result)
			}
			want := tc.reason + "; approval is required before execution; task execution was not completed"
			if result.FailureReason != want {
				t.Fatalf("failure reason = %q, want %q", result.FailureReason, want)
			}
			if plan.ReviewQueueItem.Reason != tc.reason || !plan.RiskAssessment.ApprovalRequired {
				t.Fatal("adapter modified the task's prerequisite or approval facts")
			}
		})
	}
}

func TestRunnerProjectsReviewReasonsWithoutSecretsDuplicatesOrUnboundedText(t *testing.T) {
	plan := approvalOnlyWorkflowCompletionPlan(t, "safe-review-projection-plan")
	plan.RiskAssessment.MissingRequiredAgents = []string{"evidence_reviewer"}
	plan.ReviewQueueItem.Reason = "assign and verify required participants before execution: evidence_reviewer\n token=synthetic-review-secret"
	plan.ValidationResult = task.ValidationResult{Status: "blocked", Failures: []string{
		"approval is required before execution", " approval\n is required before execution ",
		`{"password":"synthetic-validation-secret"}`, strings.Repeat("repair detail ", 1000),
	}}
	result, err := NewRunner(&capturingTaskService{plan: plan}).RunWorkflowTask(workflow.TaskRunRequest{
		OwnerIdentity: "operator@example.test", WorkflowID: uuid.NewString(), Request: "Inspect safe review projection",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.ReviewRequired || result.ApprovalRequired || result.Passed {
		t.Fatalf("safe projection changed classification: %#v", result)
	}
	if !strings.HasPrefix(result.FailureReason, "assign and verify required participants before execution: evidence_reviewer") ||
		!strings.Contains(result.FailureReason, "REDACTED") || strings.Contains(result.FailureReason, "synthetic-") {
		t.Fatalf("named prerequisite was lost or credentials exposed: %q", result.FailureReason)
	}
	if strings.Count(result.FailureReason, "approval is required before execution") != 1 ||
		strings.ContainsAny(result.FailureReason, "\n\r\t") || !strings.HasSuffix(result.FailureReason, "...") ||
		utf8.RuneCountInString(result.FailureReason) > workflowTaskFailureReasonLimit {
		t.Fatalf("failure reason was not normalized, deduplicated and bounded: %q", result.FailureReason)
	}
}

func TestWorkflowTaskFailureReasonBoundsUnicodeAndRedactsBeforeTruncation(t *testing.T) {
	value := "token=" + strings.Repeat("synthetic-secret", workflowTaskFailureReasonLimit)
	if got := workflowTaskFailureReason(value); strings.Contains(got, "synthetic-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("truncation exposed part of a secret: %q", got)
	}
	got := workflowTaskFailureReason(strings.Repeat("\u754c", workflowTaskFailureReasonLimit))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != workflowTaskFailureReasonLimit {
		t.Fatal("projection split Unicode or used a byte limit")
	}
	got = workflowTaskFailureReason("", "   ", "same reason", " same\nreason ", strings.Repeat("\u754c", workflowTaskFailureReasonLimit))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != workflowTaskFailureReasonLimit ||
		strings.Count(got, "same reason") != 1 || !strings.HasSuffix(got, "...") {
		t.Fatal("combined projection was not deduplicated or rune-bounded")
	}
}
