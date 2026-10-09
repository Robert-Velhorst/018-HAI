// Package executionapproval resolves execution authority from durable,
// owner-scoped approval records. It never accepts caller-constructed approval
// evidence as authority.
package executionapproval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/task"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	taskReviewPrefix       = "task-review:"
	taskReviewSource       = "task-review"
	approvalFreshnessLimit = executionauth.TaskReviewApprovalFreshnessLimit
	approvalFutureSkew     = executionauth.TaskReviewApprovalFutureSkew
	maximumOwnerBytes      = 255
	maximumTaskPlanRunes   = 160
	maximumNoteRunes       = 512
)

// TaskReviewDecisionRepository is the owner-scoped durable read needed by the
// resolver. task.TaskStateRepository satisfies this contract.
type TaskReviewDecisionRepository interface {
	FindApprovedReviewDecision(ownerIdentity, reviewItemID string) (*task.ReviewDecisionRecord, error)
}

// TaskReviewResolver implements executionauth.ApprovalResolver using immutable
// task-review decisions from the server-side task state repository.
type TaskReviewResolver struct {
	repository TaskReviewDecisionRepository
	now        func() time.Time
}

var _ executionauth.ApprovalResolver = (*TaskReviewResolver)(nil)

func NewTaskReviewResolver(repository TaskReviewDecisionRepository) (*TaskReviewResolver, error) {
	if isNilRepository(repository) {
		return nil, fmt.Errorf("%w: task review repository is required", ErrInvalidRequest)
	}
	return &TaskReviewResolver{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *TaskReviewResolver) Resolve(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil || r.repository == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review resolver is not configured",
			ErrInvalidRequest,
		)
	}
	return r.resolveUsing(ctx, ownerIdentity, sourceID, bindingDigest, r.repository.FindApprovedReviewDecision)
}

type taskReviewPostgresTransactionRepository interface {
	FindApprovedReviewDecisionInPostgresTransaction(
		*gorm.DB,
		string,
		string,
	) (*task.ReviewDecisionRecord, error)
}

func (r *TaskReviewResolver) ResolveInPostgresTransaction(
	ctx context.Context,
	tx *gorm.DB,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil || r.repository == nil || tx == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review resolver and PostgreSQL transaction are required",
			ErrInvalidRequest,
		)
	}
	repository, ok := r.repository.(taskReviewPostgresTransactionRepository)
	if !ok || repository == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review repository cannot resolve approvals in the execution transaction",
			ErrApprovalUnavailable,
		)
	}
	return r.resolveUsing(ctx, ownerIdentity, sourceID, bindingDigest, func(owner, reviewID string) (*task.ReviewDecisionRecord, error) {
		return repository.FindApprovedReviewDecisionInPostgresTransaction(tx, owner, reviewID)
	})
}

func (r *TaskReviewResolver) resolveUsing(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
	findDecision func(string, string) (*task.ReviewDecisionRecord, error),
) (executionauth.ResolvedApproval, error) {
	if r == nil || r.repository == nil || r.now == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review resolver is not configured",
			ErrInvalidRequest,
		)
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
	reviewID, err := parseTaskReviewSource(sourceID)
	if err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if !validSHA256Digest(bindingDigest) {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: binding digest must be a lowercase SHA-256 digest",
			ErrInvalidRequest,
		)
	}

	decision, err := findDecision(ownerIdentity, reviewID.String())
	if err != nil {
		if errors.Is(err, task.ErrTaskStateNotFound) {
			return executionauth.ResolvedApproval{}, ErrApprovalUnavailable
		}
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: read durable task review decision: %w",
			ErrApprovalUnavailable,
			err,
		)
	}
	if err := ctx.Err(); err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if decision == nil {
		return executionauth.ResolvedApproval{}, ErrApprovalUnavailable
	}
	if err := validateDecision(*decision, ownerIdentity, sourceID, reviewID); err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if decision.RequestDigest != bindingDigest {
		return executionauth.ResolvedApproval{}, ErrBindingMismatch
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

func validateOwner(ownerIdentity string) error {
	if ownerIdentity == "" ||
		ownerIdentity != strings.TrimSpace(ownerIdentity) ||
		!utf8.ValidString(ownerIdentity) ||
		len(ownerIdentity) > maximumOwnerBytes {
		return fmt.Errorf("%w: owner identity is invalid", ErrInvalidRequest)
	}
	return nil
}

func parseTaskReviewSource(sourceID string) (uuid.UUID, error) {
	if !strings.HasPrefix(sourceID, taskReviewPrefix) {
		return uuid.Nil, ErrInvalidReference
	}
	rawID := strings.TrimPrefix(sourceID, taskReviewPrefix)
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil || sourceID != taskReviewPrefix+id.String() {
		return uuid.Nil, ErrInvalidReference
	}
	return id, nil
}

func validateDecision(
	decision task.ReviewDecisionRecord,
	ownerIdentity string,
	sourceID string,
	reviewID uuid.UUID,
) error {
	decisionID, err := uuid.Parse(decision.ID)
	if err != nil || decisionID == uuid.Nil || decision.ID != decisionID.String() {
		return fmt.Errorf("%w: decision id is invalid", ErrInvalidDecision)
	}
	storedReviewID, err := uuid.Parse(decision.ReviewItemID)
	if err != nil ||
		storedReviewID == uuid.Nil ||
		decision.ReviewItemID != storedReviewID.String() ||
		storedReviewID != reviewID {
		return fmt.Errorf("%w: review item id does not match", ErrInvalidDecision)
	}
	if decision.ReviewRevision < 1 {
		return fmt.Errorf("%w: review revision is invalid", ErrInvalidDecision)
	}
	if decision.TaskPlanID == "" ||
		decision.TaskPlanID != strings.TrimSpace(decision.TaskPlanID) ||
		!utf8.ValidString(decision.TaskPlanID) ||
		utf8.RuneCountInString(decision.TaskPlanID) > maximumTaskPlanRunes {
		return fmt.Errorf("%w: task plan id is invalid", ErrInvalidDecision)
	}
	if decision.Decision != "approved" {
		return fmt.Errorf("%w: decision is not approved", ErrApprovalUnavailable)
	}
	if decision.ResolvedBy == "" ||
		decision.ResolvedBy != strings.TrimSpace(decision.ResolvedBy) ||
		!utf8.ValidString(decision.ResolvedBy) ||
		len(decision.ResolvedBy) > maximumOwnerBytes ||
		decision.ResolvedBy != ownerIdentity {
		return fmt.Errorf("%w: approver does not match the requested owner", ErrInvalidDecision)
	}
	if !utf8.ValidString(decision.ResolutionNote) ||
		utf8.RuneCountInString(decision.ResolutionNote) > maximumNoteRunes {
		return fmt.Errorf("%w: resolution note is invalid", ErrInvalidDecision)
	}
	if decision.ApprovalSource != taskReviewSource ||
		decision.ApprovalSourceID != sourceID {
		return fmt.Errorf("%w: approval provenance does not match", ErrInvalidDecision)
	}
	if !validSHA256Digest(decision.RequestDigest) {
		return fmt.Errorf("%w: request digest is invalid", ErrInvalidDecision)
	}
	if decision.ResolvedAt.IsZero() {
		return fmt.Errorf("%w: resolution timestamp is missing", ErrInvalidDecision)
	}
	return nil
}

func isNilRepository(repository TaskReviewDecisionRepository) bool {
	if repository == nil {
		return true
	}
	value := reflect.ValueOf(repository)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func digestReviewDecision(decision task.ReviewDecisionRecord) (string, error) {
	return executionauth.DigestTaskReviewDecision(executionauth.TaskReviewDecisionEvidence{
		ID:               decision.ID,
		ReviewItemID:     decision.ReviewItemID,
		ReviewRevision:   decision.ReviewRevision,
		TaskPlanID:       decision.TaskPlanID,
		Decision:         decision.Decision,
		ResolutionNote:   decision.ResolutionNote,
		ResolvedBy:       decision.ResolvedBy,
		ApprovalSource:   decision.ApprovalSource,
		ApprovalSourceID: decision.ApprovalSourceID,
		RequestDigest:    decision.RequestDigest,
		ResolvedAt:       decision.ResolvedAt,
	})
}
