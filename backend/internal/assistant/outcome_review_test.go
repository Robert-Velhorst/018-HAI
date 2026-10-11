package assistant

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/agentcycle"
	"automation-hub-backend/internal/task"
)

func TestAssistantRequiresReviewForUncertainExecution(t *testing.T) {
	if planRequiresReview(nil) {
		t.Fatal("nil plan fabricated a review")
	}
	for _, kind := range []string{"known", "execution", "tool", "legacy_action"} {
		t.Run(kind, func(t *testing.T) {
			plan := &task.CompletionPlan{
				CompletionStatus: "validated",
				ValidationResult: task.ValidationResult{Passed: true},
				ExecutionResult:  &task.ExecutionResult{VerificationStatus: "verified"},
			}
			switch kind {
			case "execution":
				plan.ExecutionResult.OutcomeUncertain = true
			case "tool":
				plan.ExecutionResult.ToolExecution = &task.ToolExecutionResult{Status: "completed", OutcomeUncertain: true}
			case "legacy_action":
				plan.ExecutionResult.Actions = []task.ExecutedAction{{Name: "automation.launch", Status: "indeterminate"}}
			}
			engine := &outcomeReviewTaskEngine{plan: plan}
			result, err := NewService(engine, nil).Command(CommandRequest{
				OwnerIdentity: "alice", Message: "Prepare the local checklist", ExecuteAllowed: true,
			})
			if err != nil || result == nil {
				t.Fatalf("assistant command failed: result=%#v err=%v", result, err)
			}
			if result.ReviewRequired != (kind != "known") {
				t.Fatalf("assistant hid uncertainty: %#v", result)
			}
			if kind != "known" {
				result.Plan.ValidationResult.NextAction = "the task is verified; continue with the next item"
				result.AgentCycle = &agentcycle.RunResult{NextAction: "continue other work"}
				if !strings.Contains(result.NextAction, "reconcile") || !strings.Contains(result.Summary, "Execution may have occurred") || !strings.Contains(deriveNextAction(result), "reconcile") || !strings.Contains(deriveSummary(result), "Execution may have occurred") {
					t.Fatalf("optimistic context overrode reconciliation: %#v", result)
				}
			}
		})
	}
}

type outcomeReviewTaskEngine struct {
	fakeTaskEngine
	plan *task.CompletionPlan
}

func (engine *outcomeReviewTaskEngine) Run(task.IntakeRequest) (*task.CompletionPlan, error) {
	return engine.plan, nil
}
