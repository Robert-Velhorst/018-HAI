package task

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

func TestToolExecutionOutcomeUncertain(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *ToolExecutionResult
		want   bool
	}{
		{"no execution", nil, false},
		{"completed", &ToolExecutionResult{Status: "completed"}, false},
		{"blocked", &ToolExecutionResult{Status: "blocked"}, false},
		{"approval only", &ToolExecutionResult{Status: "needs_approval", RequiresApproval: true}, false},
		{"explicit uncertainty dominates completion", &ToolExecutionResult{Status: "completed", OutcomeUncertain: true}, true},
		{"contradictory completion", &ToolExecutionResult{Status: "completed", RequiresApproval: true}, true},
		{"failed may have effects", &ToolExecutionResult{Status: "failed"}, true},
		{"pending", &ToolExecutionResult{Status: "pending"}, true},
		{"running", &ToolExecutionResult{Status: "running"}, true},
		{"indeterminate", &ToolExecutionResult{Status: "indeterminate"}, true},
		{"empty status", &ToolExecutionResult{}, true},
		{"unknown future status", &ToolExecutionResult{Status: "new_unknown_status"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ToolExecutionOutcomeUncertain(test.result); got != test.want {
				t.Fatalf("uncertainty = %t, want %t", got, test.want)
			}
		})
	}
}

func TestToolFailureBeforeDispatchDoesNotHideJoinedUnknownFailure(t *testing.T) {
	safe := MarkToolFailureBeforeDispatch(errors.New("rejected before runtime call"))
	if !IsToolFailureBeforeDispatch(fmt.Errorf("context: %w", safe)) {
		t.Fatal("ordinary wrapping lost the pre-dispatch marker")
	}
	for _, err := range []error{nil, errors.New("unknown"), errors.Join(safe, errors.New("runtime outcome missing")), fmt.Errorf("context: %w", errors.Join(safe, errors.New("runtime outcome missing"))), MarkToolFailureBeforeDispatch(errors.Join(errors.New("preflight"), errors.New("runtime outcome missing"))), fmt.Errorf("outer: %w", MarkToolFailureBeforeDispatch(errors.Join(safe, errors.New("unknown"))))} {
		if IsToolFailureBeforeDispatch(err) {
			t.Fatalf("unproven no-effect error accepted: %v", err)
		}
	}
}

func TestRunUncertainToolOutcomeRetainsReceiptDisablesRetryAndDurablyReplays(t *testing.T) {
	for _, name := range []string{"partial receipt and error", "completed receipt and error", "failed receipt", "indeterminate receipt", "explicit completed uncertainty", "nil result and error", "nil result without error", "pre-dispatch error"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HAI_EMERGENCY_STOP", "false")
			harness := newFallbackReviewGenerationHarness(t)
			executor := &fakeToolExecutor{result: completedToolResult()}
			uncertain := true
			switch name {
			case "partial receipt and error":
				executor.result.Status = "running"
				executor.err = errors.New("partial runtime transport failure token=super-secret")
			case "completed receipt and error":
				executor.err = errors.New("launch projection failed")
			case "failed receipt":
				executor.result.Status = "failed"
			case "indeterminate receipt":
				executor.result.Status = "indeterminate"
			case "explicit completed uncertainty":
				executor.result.OutcomeUncertain = true
			case "nil result and error":
				executor.result = nil
				executor.err = errors.New("runtime connection closed")
			case "nil result without error":
				executor.result = nil
			case "pre-dispatch error":
				executor.result = nil
				executor.err = MarkToolFailureBeforeDispatch(errors.New("invalid runtime ID"))
				uncertain = false
			}
			var wantReceipt *ToolExecutionResult
			if executor.result != nil {
				copy := *executor.result
				wantReceipt = &copy
			}
			memory := &fakeMemoryService{}
			verifier := &sequencedVerificationService{statuses: []string{verification.StatusVerified}}
			engine := NewServiceWithEngines(memory, harness.llmService, nil, verifier, executor).(*service)
			engine.frameworkSelector = harness.frameworkSelector
			request := IntakeRequest{
				OwnerIdentity: "alice", ProjectKey: "018-HAI", ExecuteAllowed: true,
				Request:      "Run local script tests and verify the routing architecture",
				AutomationID: uuid.NewString(), IdempotencyKey: "uncertain-outcome:" + strings.ReplaceAll(name, " ", "-"),
			}
			if wantReceipt != nil {
				request.AutomationID = wantReceipt.AutomationID
			}
			plan, err := engine.Run(request)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if plan.ExecutionResult == nil || plan.ExecutionResult.OutcomeUncertain != uncertain || plan.CompletionStatus != "review_required" || plan.ValidationResult.Passed || plan.RetryPolicy.RetryAvailable || plan.ReviewQueueItem == nil {
				t.Fatalf("uncertain outcome bypassed review: completion=%s execution=%#v validation=%#v retry=%#v", plan.CompletionStatus, plan.ExecutionResult, plan.ValidationResult, plan.RetryPolicy)
			}
			if !reflect.DeepEqual(plan.ExecutionResult.ToolExecution, wantReceipt) {
				t.Fatalf("receipt was discarded or rewritten: got=%#v want=%#v", plan.ExecutionResult.ToolExecution, wantReceipt)
			}
			if uncertain && (!strings.Contains(plan.ExecutionResult.Output, "may have occurred") || plan.ExecutionResult.VerificationStatus != verification.StatusUncertain) {
				t.Fatalf("unknown effects not explained: %#v", plan.ExecutionResult)
			}
			if strings.Contains(plan.ExecutionResult.BlockedReason, "super-secret") {
				t.Fatal("runtime error secret leaked into review")
			}
			if executor.calls != 1 || harness.generateCalls.Load() != 0 || verifier.calls != 0 || len(plan.StoredMemoryIDs) != 0 || len(memory.ownerCreateOwners) != 0 {
				t.Fatalf("uncertain launch repeated, generated, verified or learned: launches=%d generation=%d verification=%d memory=%d", executor.calls, harness.generateCalls.Load(), verifier.calls, len(plan.StoredMemoryIDs))
			}
			replay, err := engine.Run(request)
			if err != nil || replay.ID != plan.ID || !reflect.DeepEqual(replay.ExecutionResult, plan.ExecutionResult) || executor.calls != 1 {
				t.Fatalf("uncertainty did not durably replay: err=%v launches=%d", err, executor.calls)
			}
			if uncertain {
				_, err := engine.ResolveReviewItemForOwner("alice", plan.ReviewQueueItem.ID, ApprovalDecision{Approved: true})
				if !errors.Is(err, ErrTaskOutcomeReconciliationRequired) || executor.calls != 1 {
					t.Fatalf("ordinary approval repeated uncertain work: err=%v launches=%d", err, executor.calls)
				}
				item, err := engine.stateRepository.FindReviewItem("alice", plan.ReviewQueueItem.ID)
				if err != nil || item.Status != "open" || item.Decision != "" {
					t.Fatalf("rejected repeat mutated unresolved review: item=%#v err=%v", item, err)
				}
			}
		})
	}
}

func TestPriorUncertainToolOutcomeCannotBeReusedOrRelaunched(t *testing.T) {
	for _, actionOnly := range []bool{false, true} {
		prior := &ExecutionResult{ToolExecution: completedToolResult(), OutcomeUncertain: true}
		if actionOnly {
			prior.ToolExecution = nil
			prior.OutcomeUncertain = false
			prior.Actions = []ExecutedAction{{Name: "automation.launch", Status: "indeterminate"}}
		}
		plan := &CompletionPlan{Request: "retry previous action", ExecutionResult: prior}
		executor := &fakeToolExecutor{result: completedToolResult()}
		engine := &service{toolExecutor: executor}
		result := engine.executeAllowedSteps(plan, IntakeRequest{ExecuteAllowed: true})
		if !result.OutcomeUncertain || completedToolExecution(prior) != nil || executor.calls != 0 || result.ToolExecution != prior.ToolExecution || !strings.Contains(result.BlockedReason, "reconcile") {
			t.Fatalf("uncertain evidence reused/relaunched: %#v launches=%d", result, executor.calls)
		}
	}
}

func TestUncertainOutcomeCannotPassValidationOrStoreAcceptedLessons(t *testing.T) {
	for _, executionLevel := range []bool{false, true} {
		plan := validStructuredValidationPlan()
		plan.LessonsLearned = []MemoryUpdateProposal{{Kind: "lesson", Content: "synthetic lesson must not be learned from uncertain execution", Confidence: 1}}
		plan.Intake.NeedsTools = true
		plan.ExecutionResult.ToolExecution = deterministicReadOnlyToolExecution()
		if baseline := validatePlan(plan, 1); !baseline.Passed {
			t.Fatalf("positive validation control failed: %#v", baseline.Failures)
		}
		if executionLevel {
			plan.ExecutionResult.OutcomeUncertain = true
		} else {
			plan.ExecutionResult.ToolExecution.OutcomeUncertain = true
		}
		validation := validatePlan(plan, 1)
		if validation.Passed || !containsString(validation.Failures, uncertainToolOutcomeReason) || deterministicReadOnlyRuntimeCompleted(plan.ExecutionResult.ToolExecution) && !executionLevel {
			t.Fatalf("uncertainty was verified as completion: %#v", validation)
		}
		memory := &fakeMemoryService{}
		engine := &service{memoryService: memory}
		if stored := engine.storeLessons(plan); len(stored) != 0 || len(memory.ownerCreateOwners) != 0 {
			t.Fatal("uncertain output stored as accepted memory")
		}
	}
}
