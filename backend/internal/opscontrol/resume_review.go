package opscontrol

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const controlReviewApprovalPrefix = "opscontrol-review:"

// ResumeApprovalRequest identifies the durable owner review for one exact
// emergency-stop revision. The digest changes whenever the stop is re-engaged.
type ResumeApprovalRequest struct {
	ReviewItemID          string `json:"reviewItemId"`
	ApprovalSourceID      string `json:"approvalSourceId"`
	ApprovalBindingDigest string `json:"approvalBindingDigest"`
}

// ControlReviewItem is the runtime service's narrow view of a durable owner
// review. The router boundary adapts it to the shared task-review ledger.
type ControlReviewItem struct {
	ID              string
	TaskID          string
	OwnerIdentity   string
	Request         string
	ProjectKey      string
	SuccessCriteria []string
	Reason          string
	Status          string
	CreatedAt       time.Time
}

type ControlReviewDecision struct {
	ResolvedBy string
}

type ControlReviewRepository interface {
	ListPendingControlReviews(ownerIdentity string, limit int) ([]ControlReviewItem, error)
	CreateControlReview(ownerIdentity string, item ControlReviewItem) (*ControlReviewItem, error)
	FindControlReview(ownerIdentity, reviewItemID string) (*ControlReviewItem, error)
	ApproveControlReview(ownerIdentity, reviewItemID, note string, resolvedAt time.Time) error
	FindApprovedControlReview(ownerIdentity, reviewItemID string) (*ControlReviewDecision, error)
}

// RequestResumeApproval creates or returns the pending review for the current
// stop revision. It never changes the emergency-stop state.
func (s *Service) RequestResumeApproval(ctx context.Context, actor string) (ResumeApprovalRequest, error) {
	if err := s.requireOwnerActor(actor); err != nil {
		return ResumeApprovalRequest{}, err
	}
	if ctx == nil {
		return ResumeApprovalRequest{}, ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return ResumeApprovalRequest{}, err
	}
	if s.reviews == nil {
		return ResumeApprovalRequest{}, ErrAuthorizationUnavailable
	}
	state, err := s.control.emergency.Status()
	if err != nil {
		return ResumeApprovalRequest{}, fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if !state.Engaged {
		return ResumeApprovalRequest{}, ErrEmergencyStopStateChanged
	}
	binding, err := s.resumeEffectDigest(state.Revision)
	if err != nil {
		return ResumeApprovalRequest{}, err
	}
	taskID := resumeReviewTaskID(binding)
	items, err := s.reviews.ListPendingControlReviews(s.owner, 200)
	if err != nil {
		return ResumeApprovalRequest{}, fmt.Errorf("read pending control reviews: %w", err)
	}
	for _, item := range items {
		if item.TaskID == taskID && item.OwnerIdentity == s.owner && item.Status == "open" {
			return resumeApprovalFor(item.ID, binding), nil
		}
	}
	item, err := s.reviews.CreateControlReview(s.owner, ControlReviewItem{
		ID:              uuid.NewString(),
		TaskID:          taskID,
		OwnerIdentity:   s.owner,
		Request:         fmt.Sprintf("Resume background processing after emergency-stop revision %d.", state.Revision),
		ProjectKey:      "runtime-control",
		SuccessCriteria: []string{"clear only this emergency-stop revision", "retain durable owner approval evidence"},
		Reason:          "Resuming background processing weakens a safety control and requires explicit owner review for this exact stop revision.",
		Status:          "open",
		CreatedAt:       s.now().UTC(),
	})
	if err != nil {
		return ResumeApprovalRequest{}, fmt.Errorf("create control review: %w", err)
	}
	return resumeApprovalFor(item.ID, binding), nil
}

// ApproveAndResume records an explicit owner decision only after verifying the
// referenced item is the exact review for the currently engaged stop. It then
// consumes that fresh approval through the normal execution authorization path.
func (s *Service) ApproveAndResume(ctx context.Context, actor, reviewItemID, note string) (EmergencyStopState, error) {
	if err := s.requireOwnerActor(actor); err != nil {
		return s.control.EmergencyState(), err
	}
	if ctx == nil {
		return s.control.EmergencyState(), ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return s.control.EmergencyState(), err
	}
	if s.reviews == nil {
		return s.control.EmergencyState(), ErrAuthorizationUnavailable
	}
	state, err := s.control.emergency.Status()
	if err != nil {
		return s.control.EmergencyState(), fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if !state.Engaged {
		if s.resumedByReview(actor, reviewItemID, state) {
			return state, nil
		}
		return state, ErrEmergencyStopStateChanged
	}
	binding, err := s.resumeEffectDigest(state.Revision)
	if err != nil {
		return state, err
	}
	reviewItemID = strings.TrimSpace(reviewItemID)
	item, err := s.reviews.FindControlReview(s.owner, reviewItemID)
	if err != nil || item == nil {
		return state, fmt.Errorf("%w: resume review is unavailable", ErrAuthorizationDenied)
	}
	if item.ID != reviewItemID || item.OwnerIdentity != s.owner || item.TaskID != resumeReviewTaskID(binding) {
		return state, ErrAuthorizationMismatch
	}
	if item.Status != "open" {
		if item.Status == "approved" {
			return s.resumeWithApprovedReview(ctx, actor, reviewItemID, binding, *item)
		}
		return state, ErrAuthorizationDenied
	}
	if err := s.reviews.ApproveControlReview(s.owner, reviewItemID, strings.TrimSpace(note), s.now().UTC()); err != nil {
		// A retry after a durable approval is safe only for this same validated
		// review and current stop revision.
		approved, lookupErr := s.reviews.FindApprovedControlReview(s.owner, reviewItemID)
		if lookupErr != nil || approved == nil {
			return s.control.EmergencyState(), fmt.Errorf("%w: resume approval could not be recorded", ErrAuthorizationDenied)
		}
	}
	return s.ResumeWithApprovedReview(ctx, actor, reviewItemID)
}

// ResumeWithApprovedReview consumes an already-decided review. Review and
// execution are separate operations so callers cannot silently approve work.
func (s *Service) ResumeWithApprovedReview(ctx context.Context, actor, reviewItemID string) (EmergencyStopState, error) {
	if err := s.requireOwnerActor(actor); err != nil {
		return s.control.EmergencyState(), err
	}
	if ctx == nil {
		return s.control.EmergencyState(), ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return s.control.EmergencyState(), err
	}
	if s.reviews == nil {
		return s.control.EmergencyState(), ErrAuthorizationUnavailable
	}
	state, err := s.control.emergency.Status()
	if err != nil {
		return s.control.EmergencyState(), fmt.Errorf("%w: %v", ErrControlPersistence, err)
	}
	if !state.Engaged {
		if s.resumedByReview(actor, reviewItemID, state) {
			return state, nil
		}
		return state, ErrEmergencyStopStateChanged
	}
	binding, err := s.resumeEffectDigest(state.Revision)
	if err != nil {
		return state, err
	}
	item, err := s.reviews.FindControlReview(s.owner, strings.TrimSpace(reviewItemID))
	if err != nil || item == nil {
		return state, fmt.Errorf("%w: approved resume review is unavailable", ErrAuthorizationDenied)
	}
	return s.resumeWithApprovedReview(ctx, actor, reviewItemID, binding, *item)
}

func (s *Service) resumedByReview(actor, reviewItemID string, state EmergencyStopState) bool {
	if s.reviews == nil || state.Engaged || state.Revision == 0 || state.Actor != strings.TrimSpace(actor) {
		return false
	}
	binding, err := s.resumeEffectDigest(state.Revision - 1)
	if err != nil {
		return false
	}
	item, err := s.reviews.FindControlReview(s.owner, strings.TrimSpace(reviewItemID))
	if err != nil || item == nil || item.ID != strings.TrimSpace(reviewItemID) ||
		item.OwnerIdentity != s.owner || item.TaskID != resumeReviewTaskID(binding) || item.Status != "approved" {
		return false
	}
	decision, err := s.reviews.FindApprovedControlReview(s.owner, item.ID)
	return err == nil && decision != nil && decision.ResolvedBy == strings.TrimSpace(actor)
}

func (s *Service) resumeWithApprovedReview(ctx context.Context, actor, reviewItemID, binding string, item ControlReviewItem) (EmergencyStopState, error) {
	if item.ID != strings.TrimSpace(reviewItemID) || item.OwnerIdentity != s.owner || item.TaskID != resumeReviewTaskID(binding) || item.Status != "approved" {
		return s.control.EmergencyState(), ErrAuthorizationMismatch
	}
	decision, err := s.reviews.FindApprovedControlReview(s.owner, item.ID)
	if err != nil || decision == nil {
		return s.control.EmergencyState(), fmt.Errorf("%w: approved resume review is required", ErrAuthorizationDenied)
	}
	return s.DisengageEmergencyStop(ctx, ControlAuthorization{
		ActorIdentity:         strings.TrimSpace(actor),
		IdempotencyKey:        "opscontrol-resume:" + item.ID,
		TaskID:                item.TaskID,
		ApprovalSourceID:      controlReviewApprovalPrefix + item.ID,
		ApprovalBindingDigest: binding,
	})
}

func (s *Service) requireOwnerActor(actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ErrUnauthenticated
	}
	if strings.TrimSpace(s.owner) == "" || actor != s.owner {
		return ErrAuthorizationDenied
	}
	return nil
}

func (s *Service) resumeEffectDigest(revision uint64) (string, error) {
	return controlEffectDigest(controlEffect{
		Version:       1,
		OwnerIdentity: s.owner,
		Action:        clearEmergencyStopAction,
		ResourceType:  emergencyStopResourceType,
		ResourceID:    emergencyStopResourceID(revision),
		Target:        "disengaged",
	})
}

func resumeReviewTaskID(binding string) string { return "opscontrol:resume:" + binding }

func resumeApprovalFor(reviewItemID, binding string) ResumeApprovalRequest {
	return ResumeApprovalRequest{
		ReviewItemID:          reviewItemID,
		ApprovalSourceID:      controlReviewApprovalPrefix + reviewItemID,
		ApprovalBindingDigest: binding,
	}
}
