package task

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkflowOwnedOperationCannotRetryThroughTaskReview(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	executor := &fakeToolExecutor{result: completedToolResult()}
	engine := &service{stateRepository: repo, toolExecutor: executor}
	item := taskStateTestReviewItem("alice", "operation:"+uuid.NewString(), time.Now().UTC())
	item.Request.WorkflowID = uuid.NewString()
	queued, err := repo.CreateReviewItem("alice", item)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.ResolveReviewItemForOwner("alice", queued.ID, ApprovalDecision{Approved: true, Confirmation: TaskOperationRetryConfirmation})
	if !errors.Is(err, ErrTaskOutcomeReconciliationRequired) || executor.calls != 0 {
		t.Fatalf("workflow recovery bypassed through task retry: err=%v effects=%d", err, executor.calls)
	}
	stored, err := repo.FindReviewItem("alice", queued.ID)
	if err != nil || stored.Status != "open" || stored.Decision != "" {
		t.Fatalf("blocked workflow retry changed review: err=%v item=%#v", err, stored)
	}
	if _, err := engine.ResolveReviewItemForOwner("alice", queued.ID, ApprovalDecision{Approved: false}); err != nil {
		t.Fatalf("rejecting workflow operation review was blocked: %v", err)
	}
}

func TestApprovedReviewReconciliationCannotCompleteUncertainExecution(t *testing.T) {
	for _, toolLevel := range []bool{false, true} {
		plan := validStructuredValidationPlan()
		plan.CompletionStatus = "validated"
		plan.ValidationResult.Passed = true
		plan.ExecutionResult.VerificationStatus = "verified"
		review := ReviewQueueItem{ID: "review", TaskID: plan.ID}
		if decision := approvedReviewReconciliationDecision(review, plan); decision.Disposition != "complete" {
			t.Fatalf("positive completion control failed: %#v", decision)
		}
		if toolLevel {
			plan.ExecutionResult.ToolExecution = completedToolResult()
			plan.ExecutionResult.ToolExecution.OutcomeUncertain = true
		} else {
			plan.ExecutionResult.OutcomeUncertain = true
		}
		if decision := approvedReviewReconciliationDecision(review, plan); decision.Disposition != "review" {
			t.Fatalf("uncertain evidence accepted during reconciliation: %#v", decision)
		}
		for _, requirement := range []string{"postcondition verification", "validation result", "reproducible result", "deliverable evidence", "individual outputs", "action feedback"} {
			if evidence := exactFrameworkPostconditionEvidence(plan, FrameworkEvidenceContract{Requirement: requirement}); len(evidence) != 0 {
				t.Fatalf("uncertain postcondition generated %q evidence: %#v", requirement, evidence)
			}
		}
	}
}
