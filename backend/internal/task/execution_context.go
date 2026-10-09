package task

import (
	"context"
	"errors"
	"strings"
	"time"

	"automation-hub-backend/internal/verification"
)

var ErrTaskReviewContextUnavailable = errors.New("task review requires cancellation-aware resolution")

type ContextualReviewService interface {
	ResolveReviewItemForOwnerContext(context.Context, string, string, ApprovalDecision) (*ReviewResolutionResult, error)
}

func (s *service) ResolveReviewItemForOwnerContext(ctx context.Context, owner, id string, decision ApprovalDecision) (*ReviewResolutionResult, error) {
	if ctx == nil || strings.TrimSpace(owner) == "" {
		return nil, ErrTaskReviewContextUnavailable
	}
	return s.resolveReviewItemForOwnerWithStorage(ctx, owner, id, decision, true)
}

func taskExecutionContext(request IntakeRequest) context.Context {
	if request.ExecutionContext != nil {
		return request.ExecutionContext
	}
	return context.Background()
}

func cancelledTaskExecution(result *ExecutionResult, plan *CompletionPlan, request IntakeRequest, started time.Time) *ExecutionResult {
	if result == nil {
		result = newExecutionResult(plan, request, started)
	}
	result.Mode = "blocked"
	result.VerificationStatus = verification.StatusNeedsReview
	result.BlockedReason = "task execution cancelled; inspect retained evidence before another attempt"
	result.CompletedAt = time.Now().UTC()
	result.Actions = append(result.Actions, executedAction("task.cancellation", "blocked", "", result.BlockedReason, started))
	return result
}
