package task

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type operationEvidenceFailureRepository struct {
	TaskStateRepository
	failure  error
	boundary string
}

func (r *operationEvidenceFailureRepository) MarkTaskOperationNeedsReview(owner string, id uuid.UUID, lease string, generation int64, reason string, now time.Time) (bool, error) {
	if r.boundary == "mark" {
		return false, r.failure
	}
	return r.TaskStateRepository.MarkTaskOperationNeedsReview(owner, id, lease, generation, reason, now)
}

func (r *operationEvidenceFailureRepository) FindReviewItem(owner, id string) (*ReviewQueueItem, error) {
	if r.boundary == "review" {
		return nil, r.failure
	}
	return r.TaskStateRepository.FindReviewItem(owner, id)
}

func (r *operationEvidenceFailureRepository) CompleteTaskOperation(owner string, id uuid.UUID, lease string, generation int64, planID string, now time.Time) (bool, error) {
	switch r.boundary {
	case "complete":
		return false, r.failure
	case "fence":
		return false, nil
	case "readback", "missing_readback":
		return true, nil
	default:
		return r.TaskStateRepository.CompleteTaskOperation(owner, id, lease, generation, planID, now)
	}
}

func (r *operationEvidenceFailureRepository) FindCompletionPlan(owner, id string) (*CompletionPlan, error) {
	if r.boundary == "readback" {
		return nil, r.failure
	}
	if r.boundary == "missing_readback" {
		return nil, nil
	}
	return r.TaskStateRepository.FindCompletionPlan(owner, id)
}

func TestTaskOperationRetainsEvidenceWhenReconciliationStorageFails(t *testing.T) {
	for _, boundary := range []string{"mark", "review", "complete", "fence", "readback", "missing_readback"} {
		t.Run(boundary, func(t *testing.T) {
			storageErr := errors.New("reconciliation storage unavailable")
			executionErr := errors.New("entered execution failed")
			repo := &operationEvidenceFailureRepository{TaskStateRepository: NewMemoryTaskStateRepository(), failure: storageErr, boundary: boundary}
			svc := &service{stateRepository: repo}
			plan := &CompletionPlan{ID: uuid.NewString(), ExecutionResult: &ExecutionResult{ToolExecution: completedToolResult()}}
			plan.CompletionStatus = "completed"
			plan.ValidationResult.Passed = true
			plan.RetryPolicy.RetryAvailable = true
			returned, err := svc.withTaskOperation(IntakeRequest{OwnerIdentity: "alice", Request: "Run local script", IdempotencyKey: "failed-reconciliation-" + boundary}, "run", func(IntakeRequest) (*CompletionPlan, error) {
				if boundary == "mark" || boundary == "review" {
					return plan, executionErr
				}
				return plan, nil
			})
			if returned != plan {
				t.Fatal("reconciliation failure erased acknowledged execution evidence")
			}
			if returned.CompletionStatus != "review_required" || returned.ValidationResult.Passed || returned.RetryPolicy.RetryAvailable {
				t.Fatal("unconfirmed persistence outcome was presented as completed or retryable")
			}
			if !errors.Is(err, ErrTaskOperationNeedsReview) {
				t.Fatalf("lost reconciliation error identity: %v", err)
			}
			if (boundary == "mark" || boundary == "review") && !errors.Is(err, executionErr) {
				t.Fatalf("lost execution failure identity: %v", err)
			}
			if boundary != "fence" && boundary != "missing_readback" && !errors.Is(err, storageErr) {
				t.Fatalf("lost persistence failure identity: %v", err)
			}
		})
	}
}
