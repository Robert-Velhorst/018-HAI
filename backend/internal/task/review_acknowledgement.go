package task

import "strings"

// Validate the adapter acknowledgement before it changes the mirror, learning,
// or execution authority. The immutable decision remains in storage on failure.
func validateReviewAcknowledgement(owner string, before ReviewQueueItem, requested ReviewResolution, received *PersistedReviewResolution) error {
	if received == nil {
		return ErrTaskReviewBindingMismatch
	}
	item, decision := received.Item, received.Decision
	beforeDigest, err := ReviewRequestDigest(owner, before.Request)
	if err != nil {
		return ErrTaskReviewBindingMismatch
	}
	afterDigest, err := ReviewRequestDigest(owner, item.Request)
	if err != nil || beforeDigest != afterDigest || decision.RequestDigest != beforeDigest {
		return ErrTaskReviewBindingMismatch
	}
	if item.ID != before.ID || item.TaskID != before.TaskID ||
		!item.CreatedAt.Equal(before.CreatedAt) || item.Request.OwnerIdentity != owner ||
		strings.TrimSpace(decision.ID) == "" || decision.ReviewItemID != before.ID ||
		decision.TaskPlanID != before.TaskID || decision.ResolvedBy != owner ||
		decision.ReviewRevision < 1 || decision.ApprovalSource != taskReviewApprovalSource ||
		decision.ApprovalSourceID != taskReviewApprovalSource+":"+before.ID ||
		decision.Decision != requested.Decision || item.Decision != requested.Decision ||
		item.Status != requested.Decision || decision.ResolutionNote != sanitizeApprovalNote(requested.Note) ||
		item.ResolutionNote != decision.ResolutionNote || item.ResolvedAt == nil ||
		decision.ResolvedAt.IsZero() || !decision.ResolvedAt.Equal(*item.ResolvedAt) ||
		!decision.ResolvedAt.Equal(normalizeTaskStateTimestamp(requested.ResolvedAt)) ||
		decision.ResolvedAt.Before(before.CreatedAt) {
		return ErrTaskReviewBindingMismatch
	}
	return nil
}

func validateReviewOutcomeAcknowledgement(owner string, approved ReviewQueueItem, requested ReviewOutcome, received *ReviewQueueItem) error {
	if received == nil {
		return ErrTaskReviewBindingMismatch
	}
	outcome, err := normalizeReviewOutcome(requested)
	if err != nil {
		return ErrTaskReviewBindingMismatch
	}
	beforeDigest, err := ReviewRequestDigest(owner, approved.Request)
	if err != nil {
		return ErrTaskReviewBindingMismatch
	}
	afterDigest, err := ReviewRequestDigest(owner, received.Request)
	if err != nil || beforeDigest != afterDigest || received.Request.OwnerIdentity != owner ||
		received.ID != approved.ID || !received.CreatedAt.Equal(approved.CreatedAt) ||
		received.TaskID != outcome.TaskPlanID || received.Status != outcome.Status ||
		received.Reason != outcome.Reason {
		return ErrTaskReviewBindingMismatch
	}
	if outcome.Status == "completed" {
		if received.Decision != "approved" || received.ResolutionNote != approved.ResolutionNote ||
			received.ResolvedAt == nil || !received.ResolvedAt.Equal(outcome.At) {
			return ErrTaskReviewBindingMismatch
		}
	} else if received.ResolvedAt != nil || received.Decision != "" || received.ResolutionNote != "" {
		return ErrTaskReviewBindingMismatch
	}
	return nil
}
