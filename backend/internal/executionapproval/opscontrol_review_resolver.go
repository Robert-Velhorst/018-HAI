package executionapproval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/task"

	"github.com/google/uuid"
)

// OpsControlReviewPrefix identifies a task-ledger approval that can be used
// only to weaken a specific runtime safety control. It is intentionally not a
// generic task-review reference.
const OpsControlReviewPrefix = "opscontrol-review:"

// OpsControlReviewResolver reads the shared append-only review ledger but
// accepts only reviews whose immutable task identity contains the requested
// exact control-effect digest.
type OpsControlReviewResolver struct {
	repository OpsControlReviewRepository
	now        func() time.Time
}

var _ executionauth.ApprovalResolver = (*OpsControlReviewResolver)(nil)

// OpsControlReviewRepository provides both the immutable approval decision and
// its action-defining review item from the same owner-scoped ledger.
type OpsControlReviewRepository interface {
	TaskReviewDecisionRepository
	FindReviewItem(ownerIdentity, reviewItemID string) (*task.ReviewQueueItem, error)
}

func NewOpsControlReviewResolver(repository OpsControlReviewRepository) (*OpsControlReviewResolver, error) {
	if isNilRepository(repository) {
		return nil, fmt.Errorf("%w: task review repository is required", ErrInvalidRequest)
	}
	return &OpsControlReviewResolver{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *OpsControlReviewResolver) Resolve(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil || r.repository == nil || r.now == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: ops-control review resolver is not configured", ErrInvalidRequest)
	}
	if ctx == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: context is required", ErrInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if err := validateOwner(ownerIdentity); err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	reviewID, err := parseOpsControlReviewSource(sourceID)
	if err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if !validSHA256Digest(bindingDigest) {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: binding digest must be a lowercase SHA-256 digest", ErrInvalidRequest)
	}

	decision, err := r.repository.FindApprovedReviewDecision(ownerIdentity, reviewID.String())
	if err != nil || decision == nil {
		return executionauth.ResolvedApproval{}, ErrApprovalUnavailable
	}
	item, err := r.repository.FindReviewItem(ownerIdentity, reviewID.String())
	if err != nil || item == nil {
		return executionauth.ResolvedApproval{}, ErrApprovalUnavailable
	}
	if err := validateOpsControlReview(*decision, *item, ownerIdentity, reviewID, bindingDigest); err != nil {
		return executionauth.ResolvedApproval{}, err
	}

	now := r.now().UTC()
	approvedAt := decision.ResolvedAt.UTC()
	if approvedAt.After(now.Add(approvalFutureSkew)) {
		return executionauth.ResolvedApproval{}, ErrFutureApproval
	}
	expiresAt := approvedAt.Add(approvalFreshnessLimit)
	if !now.Before(expiresAt) {
		return executionauth.ResolvedApproval{}, ErrStaleApproval
	}
	decisionDigest, err := digestReviewDecision(*decision)
	if err != nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: %v", ErrInvalidDecision, err)
	}
	return executionauth.ResolvedApproval{
		SourceID:       sourceID,
		DecisionID:     decision.ID,
		DecisionDigest: decisionDigest,
		BindingDigest:  bindingDigest,
		ApprovedBy:     decision.ResolvedBy,
		ApproverRoles:  []string{"owner"},
		ApprovedAt:     approvedAt,
		ExpiresAt:      expiresAt,
	}, nil
}

func parseOpsControlReviewSource(sourceID string) (uuid.UUID, error) {
	if !strings.HasPrefix(sourceID, OpsControlReviewPrefix) {
		return uuid.Nil, ErrInvalidReference
	}
	rawID := strings.TrimPrefix(sourceID, OpsControlReviewPrefix)
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil || sourceID != OpsControlReviewPrefix+id.String() {
		return uuid.Nil, ErrInvalidReference
	}
	return id, nil
}

func validateOpsControlReview(
	decision task.ReviewDecisionRecord,
	item task.ReviewQueueItem,
	ownerIdentity string,
	reviewID uuid.UUID,
	bindingDigest string,
) error {
	decisionID, err := uuid.Parse(decision.ID)
	if err != nil || decisionID == uuid.Nil || decision.ID != decisionID.String() ||
		decision.ReviewItemID != reviewID.String() || decision.Decision != "approved" ||
		decision.ResolvedBy != ownerIdentity || decision.ResolvedAt.IsZero() {
		return ErrInvalidDecision
	}
	if item.ID != reviewID.String() || item.Request.OwnerIdentity != ownerIdentity ||
		item.Status != "approved" || item.TaskID != "opscontrol:resume:"+bindingDigest {
		return ErrBindingMismatch
	}
	return nil
}
