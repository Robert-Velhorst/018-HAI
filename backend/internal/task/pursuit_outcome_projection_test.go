package task

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPursuitProjectionUncertaintyDominatesOptimisticCompletion(t *testing.T) {
	for _, source := range []string{"execution", "tool", "legacy", "blocked", "missing_execution", "validation_failed", "unverified", "approval", "known_completed", "schema_validated", "plan_only"} {
		t.Run(source, func(t *testing.T) {
			recorder := &fakePursuitAttemptRecorder{}
			s := &service{pursuitAttempts: recorder}
			launchID := uuid.New().String()
			plan := &CompletionPlan{
				ID: uuid.New().String(), OwnerIdentity: "alice", CompletionStatus: "validated",
				CreatedAt: time.Now().UTC(), ValidationResult: ValidationResult{Passed: true, Status: "passed"},
				ExecutionResult: &ExecutionResult{VerificationStatus: "verified", ToolExecution: &ToolExecutionResult{Status: "completed", LaunchEventID: launchID}},
			}
			switch source {
			case "execution":
				plan.ExecutionResult.OutcomeUncertain = true
			case "tool":
				plan.ExecutionResult.ToolExecution.OutcomeUncertain = true
			case "legacy":
				plan.ExecutionResult.Actions = []ExecutedAction{{Name: "automation.launch", Status: "indeterminate"}}
			case "blocked":
				plan.ExecutionResult.BlockedReason = "requires reconciliation"
			case "missing_execution":
				plan.ExecutionResult = nil
			case "validation_failed":
				plan.ValidationResult.Passed = false
			case "unverified":
				plan.ExecutionResult.VerificationStatus = "uncertain"
			case "approval":
				plan.RiskAssessment.ApprovalRequired = true
			case "schema_validated":
				plan.ExecutionResult.VerificationStatus = "schema_validated"
			case "plan_only":
				plan.ExecutionResult = nil
			}
			mode := "run"
			if source == "plan_only" {
				mode = "plan"
			}
			if err := s.persistPursuitAttempt(plan, IntakeRequest{PursuitID: uuid.New().String()}, mode, true); err != nil {
				t.Fatal(err)
			}
			if len(recorder.attempts) != 1 {
				t.Fatalf("attempt count = %d", len(recorder.attempts))
			}
			got := recorder.attempts[0]
			if (plan.ExecutionResult != nil && got.LaunchEventID != launchID) || got.TaskPlanID != plan.ID || got.OwnerIdentity != "alice" || got.CompletedAt == nil {
				t.Fatalf("projection lost source identity or attempt completion time: %#v", got)
			}
			if source == "known_completed" || source == "schema_validated" || source == "plan_only" {
				if got.Status != "validated" || got.BlockedReason != "" {
					t.Fatalf("known completion was changed: %#v", got)
				}
			} else if got.Status != "review_required" || got.VerificationStatus != "needs_review" || got.BlockedReason == "" {
				t.Fatalf("uncertain outcome projected as success: %#v", got)
			}
		})
	}
}
