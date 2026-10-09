package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultApprovedReviewReconciliationAge = 30 * time.Minute
	minimumApprovedReviewReconciliationAge = 5 * time.Minute
	maximumApprovedReviewReconciliationAge = 30 * 24 * time.Hour
	approvedReviewReconciliationConfirm    = "RECONCILE APPROVED TASKS"
)

// ApprovedReviewReconciliationRequest controls a fail-closed recovery pass.
// It never retries work. Apply=false is a side-effect-free preview.
type ApprovedReviewReconciliationRequest struct {
	Apply            bool   `json:"apply"`
	Confirmation     string `json:"confirmation,omitempty"`
	OlderThanMinutes int    `json:"olderThanMinutes,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

type ApprovedReviewReconciliationItem struct {
	ReviewItemID string `json:"reviewItemId"`
	TaskPlanID   string `json:"taskPlanId"`
	Disposition  string `json:"disposition"`
	Reason       string `json:"reason"`
	Applied      bool   `json:"applied"`
}

type ApprovedReviewReconciliationResult struct {
	DryRun           bool                               `json:"dryRun"`
	Cutoff           time.Time                          `json:"cutoff"`
	Inspected        int                                `json:"inspected"`
	ApprovedFound    int                                `json:"approvedFound"`
	Eligible         int                                `json:"eligible"`
	Completed        int                                `json:"completed"`
	ReturnedToReview int                                `json:"returnedToReview"`
	Conflicts        int                                `json:"conflicts"`
	Items            []ApprovedReviewReconciliationItem `json:"items"`
}

// ReviewReconciliationService is separate from the ordinary approval service
// so HTTP handlers can fail closed when a deployment does not provide the
// durable recovery boundary.
type ReviewReconciliationService interface {
	ReconcileApprovedReviewsForOwner(ownerIdentity string, request ApprovedReviewReconciliationRequest) (*ApprovedReviewReconciliationResult, error)
}

func (s *service) ReconcileApprovedReviewsForOwner(ownerIdentity string, request ApprovedReviewReconciliationRequest) (*ApprovedReviewReconciliationResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if request.Apply && request.Confirmation != approvedReviewReconciliationConfirm {
		return nil, fmt.Errorf("confirmation must exactly match %q", approvedReviewReconciliationConfirm)
	}
	age, err := normalizeApprovedReviewReconciliationAge(request.OlderThanMinutes)
	if err != nil {
		return nil, err
	}
	limit := normalizeTaskStateLimit(request.Limit)
	now := time.Now().UTC()
	cutoff := now.Add(-age)
	result := &ApprovedReviewReconciliationResult{
		DryRun: !request.Apply,
		Cutoff: cutoff,
		Items:  []ApprovedReviewReconciliationItem{},
	}

	reviews, err := s.stateRepository.ListReviewItems(ownerIdentity, limit)
	if err != nil {
		return nil, fmt.Errorf("list task reviews for reconciliation: %w", err)
	}
	plans, err := s.stateRepository.ListCompletionPlans(ownerIdentity, taskStateMaximumLimit)
	if err != nil {
		return nil, fmt.Errorf("list task evidence for reconciliation: %w", err)
	}
	latestByReview := latestCompletionPlansByReview(plans)

	for _, review := range reviews {
		result.Inspected++
		if review.Status != "approved" {
			continue
		}
		result.ApprovedFound++
		if review.ResolvedAt == nil || review.ResolvedAt.After(cutoff) {
			continue
		}
		result.Eligible++
		item := approvedReviewReconciliationDecision(review, latestByReview[review.ID])
		if request.Apply {
			updated, markErr := s.stateRepository.MarkReviewOutcome(ownerIdentity, review.ID, ReviewOutcome{
				TaskPlanID: item.TaskPlanID,
				Status:     reconciliationOutcomeStatus(item.Disposition),
				Reason:     item.Reason,
				At:         now,
			})
			if markErr != nil {
				if errors.Is(markErr, ErrTaskReviewInvalidTransition) || errors.Is(markErr, ErrTaskStateConflict) {
					item.Disposition = "conflict"
					item.Reason = "review state changed during reconciliation; reload before taking any action"
					result.Conflicts++
					result.Items = append(result.Items, item)
					continue
				}
				return nil, fmt.Errorf("reconcile approved review %s: %w", review.ID, markErr)
			}
			item.Applied = true
			s.updateReviewMirror(*updated)
		}
		if item.Disposition == "complete" {
			result.Completed++
		} else {
			result.ReturnedToReview++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func normalizeApprovedReviewReconciliationAge(minutes int) (time.Duration, error) {
	if minutes == 0 {
		return defaultApprovedReviewReconciliationAge, nil
	}
	age := time.Duration(minutes) * time.Minute
	if age < minimumApprovedReviewReconciliationAge || age > maximumApprovedReviewReconciliationAge {
		return 0, fmt.Errorf("olderThanMinutes must be between %d and %d", int(minimumApprovedReviewReconciliationAge/time.Minute), int(maximumApprovedReviewReconciliationAge/time.Minute))
	}
	return age, nil
}

func latestCompletionPlansByReview(plans []CompletionPlan) map[string]*CompletionPlan {
	result := make(map[string]*CompletionPlan)
	for index := range plans {
		plan := &plans[index]
		reviewID := strings.TrimSpace(plan.ReviewItemID)
		if reviewID == "" {
			continue
		}
		current := result[reviewID]
		if current == nil || plan.CreatedAt.After(current.CreatedAt) {
			result[reviewID] = plan
		}
	}
	return result
}

func approvedReviewReconciliationDecision(review ReviewQueueItem, plan *CompletionPlan) ApprovedReviewReconciliationItem {
	item := ApprovedReviewReconciliationItem{
		ReviewItemID: review.ID,
		TaskPlanID:   review.TaskID,
		Disposition:  "review",
		Reason:       "approved task has no linked durable completion evidence; outcome is indeterminate and must be reviewed before any retry",
	}
	if plan == nil {
		return item
	}
	item.TaskPlanID = plan.ID
	if plan.CompletionStatus == "validated" &&
		plan.ValidationResult.Passed &&
		plan.ExecutionResult != nil &&
		!executionOutcomeUncertain(plan.ExecutionResult) &&
		verificationStatusAcceptsCompletion(plan.ExecutionResult.VerificationStatus) {
		item.Disposition = "complete"
		item.Reason = "linked durable task evidence proves execution completed and passed validation"
		return item
	}
	item.Reason = "linked task attempt did not prove verified completion; return to review without repeating the action"
	return item
}

func reconciliationOutcomeStatus(disposition string) string {
	if disposition == "complete" {
		return "completed"
	}
	return "needs_review"
}
