package workflow

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestRunDueCannotCompleteWhenTaskRunnerSeparatelyRequiresApproval(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "approval-required-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
		ApprovalRequired:   true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Completed != 0 || summary.Blocked != 1 || len(summary.Results) != 1 ||
		summary.Results[0].Status == "completed" || !summary.Results[0].ReviewRequired || summary.Results[0].Attempts != 0 {
		t.Fatalf("approval-required task result was reported complete: %#v", summary)
	}

	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateNeedsApproval || !updated.Item.RequiresApproval ||
		updated.Item.ApprovalStatus != "pending" {
		t.Fatalf("approval-required result did not enter the approval queue: %#v", updated.Item)
	}
	if _, exists := repo.attestations[record.Item.ID]; exists {
		t.Fatal("approval-required task result created a completion attestation")
	}
}

func TestRunDueRequiresReconciliationWhenApprovalIsRequestedAfterExternalAction(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:                 "post-action-approval-plan",
		CompletionStatus:       "validated",
		VerificationStatus:     "verified",
		RuntimeEvidenceURI:     "automation-launch://11111111-1111-1111-1111-111111111111",
		ExternalActionExecuted: true,
		Passed:                 true,
		ApprovalRequired:       true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Completed != 0 || summary.Blocked != 1 || len(summary.Results) != 1 ||
		summary.Results[0].Status == "completed" || !summary.Results[0].ReviewRequired {
		t.Fatalf("post-action approval requirement was reported complete: %#v", summary)
	}

	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.RecoveryStatus != RecoveryNeedsReview ||
		updated.Item.CompletedAt != nil || updated.Item.WorkerClaimID != "" {
		t.Fatalf("post-action approval requirement did not retain a blocked, recoverable state: %#v", updated.Item)
	}
	if _, exists := repo.attestations[record.Item.ID]; exists {
		t.Fatal("post-action approval requirement created a completion attestation")
	}
}

func TestPostgresUpdateClaimedItemPersistsRuntimeApprovalGate(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin test transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	repo := NewGormRepository(tx)
	claimID := uuid.NewString()
	leaseUntil := time.Now().UTC().Add(time.Minute)
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "approval-persistence-test", Title: "Runtime approval gate",
		CurrentState: StateInProgress, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		ApprovalStatus: "not_required", WorkerClaimID: claimID, WorkerLeaseUntil: &leaseUntil,
	})
	if err != nil {
		t.Fatalf("create claimed workflow: %v", err)
	}

	item.CurrentState = StateNeedsApproval
	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	item.ApprovalReason = "task engine requires explicit human approval"
	item.NextAction = "review the exact proposed action and approve before execution"
	updated, owned, err := repo.UpdateClaimedItem(item, claimID)
	if err != nil || !owned || updated == nil {
		t.Fatalf("UpdateClaimedItem = (%#v, %t, %v), want persisted approval transition", updated, owned, err)
	}
	if updated.CurrentState != StateNeedsApproval || !updated.RequiresApproval ||
		updated.ApprovalStatus != "pending" || updated.ApprovalReason != item.ApprovalReason ||
		updated.WorkerClaimID != "" || updated.WorkerLeaseUntil != nil {
		t.Fatalf("persisted runtime approval transition = %#v", updated)
	}
}

func TestCompletionAttestationRejectsOutstandingApprovalRequirement(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "approval-attestation-test"}
	validResult := func() *TaskRunResult {
		return &TaskRunResult{
			PlanID: "approval-attestation-plan", CompletionStatus: "validated",
			VerificationStatus: "verified", Passed: true,
		}
	}

	result := validResult()
	result.ApprovalRequired = true
	if _, err := newWorkflowCompletionAttestation(item, result, time.Now().UTC()); err == nil {
		t.Fatal("completion attestation accepted a result that still requires approval")
	}

	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	if _, err := newWorkflowCompletionAttestation(item, validResult(), time.Now().UTC()); err == nil {
		t.Fatal("completion attestation accepted a workflow with unapproved required approval")
	}

	item.ApprovalStatus = "approved"
	if _, err := newWorkflowCompletionAttestation(item, validResult(), time.Now().UTC()); err != nil {
		t.Fatalf("completion attestation rejected a workflow with recorded approval: %v", err)
	}
}
