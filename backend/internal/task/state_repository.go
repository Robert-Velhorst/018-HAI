package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	taskStateDefaultLimit                = 50
	taskStateMaximumLimit                = 200
	taskStateMaximumPayloadSize          = 2 * 1024 * 1024
	taskStateMaximumStringRunes          = 16 * 1024
	taskStateMaximumReasonRunes          = 4096
	taskStateMaximumResolutionNoteRunes  = 512
	taskStateStorageTimestampGranularity = time.Microsecond

	taskCompletionProvenance = "task-success-engine"
	taskReviewApprovalSource = "task-review"
)

var (
	ErrTaskStateNotFound              = errors.New("task state record not found")
	ErrTaskStateConflict              = errors.New("task state conflicts with existing immutable state")
	ErrTaskReviewAlreadyResolved      = errors.New("task review item is already resolved")
	ErrTaskReviewBindingMismatch      = errors.New("task review request provenance does not match")
	ErrTaskReviewInvalidTransition    = errors.New("invalid task review state transition")
	ErrTaskOperationInProgress        = errors.New("task operation is already in progress")
	ErrTaskOperationNeedsReview       = errors.New("task operation outcome requires review")
	ErrTaskOperationCanceled          = errors.New("task operation was canceled before execution")
	ErrTaskOperationRetryConfirmation = errors.New("uncertain task operation retry requires explicit confirmation")
)

const (
	TaskOperationAcquired    = "acquired"
	TaskOperationReplay      = "replay"
	TaskOperationInProgress  = "in_progress"
	TaskOperationNeedsReview = "needs_review"
	TaskOperationCanceled    = "canceled"
)

type TaskOperationClaim struct {
	Operation   models.TaskOperationRecord
	Disposition string
}

// ContextTaskOperationClaimer allows request-scoped claim acquisition to stop
// promptly when the HTTP caller goes away. The legacy repository contract stays
// valid for external implementations, while production storage implements this
// context-aware extension.
type ContextTaskOperationClaimer interface {
	ClaimTaskOperationContext(context.Context, string, string, string, string, string, time.Time, time.Duration) (TaskOperationClaim, error)
}

type ContextTaskOperationHeartbeater interface {
	HeartbeatTaskOperationContext(context.Context, string, uuid.UUID, string, int64, time.Time) (bool, error)
}

type ContextTaskOperationCompleter interface {
	CompleteTaskOperationContext(context.Context, string, uuid.UUID, string, int64, string, time.Time) (bool, error)
}

type ContextTaskOperationReviewer interface {
	MarkTaskOperationNeedsReviewContext(context.Context, string, uuid.UUID, string, int64, string, time.Time) (bool, error)
}

type TaskOperationCanceler interface {
	CancelTaskOperation(string, uuid.UUID, string, int64, string, time.Time) (bool, error)
}

type ContextTaskOperationCanceler interface {
	CancelTaskOperationContext(context.Context, string, uuid.UUID, string, int64, string, time.Time) (bool, error)
}

type ContextCompletionPlanFinder interface {
	FindCompletionPlanContext(context.Context, string, string) (*CompletionPlan, error)
}

// TaskStateRepository is the durable boundary for task completion history and
// human review decisions. Implementations must owner-scope every read/write and
// preserve append-only completion and decision records.
type TaskStateRepository interface {
	ClaimTaskOperation(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, error)
	HeartbeatTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, now time.Time) (bool, error)
	CompleteTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, taskPlanID string, now time.Time) (bool, error)
	MarkTaskOperationNeedsReview(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error)

	AppendCompletionPlan(ownerIdentity string, plan CompletionPlan) error
	ListCompletionPlans(ownerIdentity string, limit int) ([]CompletionPlan, error)
	FindCompletionPlan(ownerIdentity, taskPlanID string) (*CompletionPlan, error)

	CreateReviewItem(ownerIdentity string, item ReviewQueueItem) (*ReviewQueueItem, error)
	ListReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error)
	FindReviewItem(ownerIdentity, reviewItemID string) (*ReviewQueueItem, error)
	ResolveReviewItem(ownerIdentity, reviewItemID string, resolution ReviewResolution) (*PersistedReviewResolution, error)
	MarkReviewOutcome(ownerIdentity, reviewItemID string, outcome ReviewOutcome) (*ReviewQueueItem, error)
	ListReviewDecisions(ownerIdentity, reviewItemID string, limit int) ([]ReviewDecisionRecord, error)
	FindApprovedReviewDecision(ownerIdentity, reviewItemID string) (*ReviewDecisionRecord, error)
}

// TaskHistoryRepository exposes the small, owner-scoped projection required
// by list views. Durable implementations must bind every projection to an
// immutable completion record instead of trusting client-supplied summaries.
type TaskHistoryRepository interface {
	ListCompletionPlanHistory(ownerIdentity string, limit int) ([]CompletionPlanHistoryItem, error)
}

type CompletionPlanHistoryItem struct {
	ID               string                      `json:"id"`
	CreatedAt        time.Time                   `json:"createdAt"`
	Request          string                      `json:"request"`
	ProjectKey       string                      `json:"projectKey,omitempty"`
	Intake           CompletionPlanHistoryIntake `json:"intake"`
	CompletionStatus string                      `json:"completionStatus"`
}

type CompletionPlanHistoryIntake struct {
	TaskType        string   `json:"taskType"`
	SuccessCriteria []string `json:"successCriteria"`
}

// PendingReviewStateRepository lets the interactive queue read only work that
// still requires a decision. Historical outcomes remain available to audit and
// reconciliation paths, but an unreadable legacy outcome cannot take the live
// approval queue offline.
type PendingReviewStateRepository interface {
	ListPendingReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error)
}

// ReviewResolution is the only input required to resolve an open review item.
// The repository derives ResolvedBy and the trusted approval source from the
// authenticated owner and stored item; callers cannot spoof either field.
type ReviewResolution struct {
	Decision   string
	Note       string
	ResolvedAt time.Time
}

// ReviewOutcome records what happened after an approved task was attempted.
// It never removes the immutable approval decision that authorized the attempt.
type ReviewOutcome struct {
	TaskPlanID string
	Status     string
	Reason     string
	At         time.Time
}

// ReviewDecisionRecord is the public, immutable decision projection used to
// validate later execution against the reviewed action.
type ReviewDecisionRecord struct {
	ID               string    `json:"id"`
	ReviewItemID     string    `json:"reviewItemId"`
	ReviewRevision   int       `json:"reviewRevision"`
	TaskPlanID       string    `json:"taskPlanId"`
	Decision         string    `json:"decision"`
	ResolutionNote   string    `json:"resolutionNote,omitempty"`
	ResolvedBy       string    `json:"resolvedBy"`
	ApprovalSource   string    `json:"approvalSource"`
	ApprovalSourceID string    `json:"approvalSourceId"`
	RequestDigest    string    `json:"requestDigest"`
	ResolvedAt       time.Time `json:"resolvedAt"`
}

type PersistedReviewResolution struct {
	Item     ReviewQueueItem      `json:"item"`
	Decision ReviewDecisionRecord `json:"decision"`
}

// storedReviewRequest is the durable, redacted projection of an IntakeRequest.
// WorkflowID is intentionally absent from the public IntakeRequest JSON
// contract, but remains part of the reviewed action identity so workflow-owned
// attempts cannot be reclassified as direct pursuit attempts after a restart.
type storedReviewRequest struct {
	PursuitID        string                              `json:"pursuitId,omitempty"`
	WorkflowID       string                              `json:"workflowId,omitempty"`
	Request          string                              `json:"request"`
	ProjectKey       string                              `json:"projectKey,omitempty"`
	AutomationID     string                              `json:"automationId,omitempty"`
	MandateID        string                              `json:"mandateId,omitempty"`
	SuccessCriteria  []string                            `json:"successCriteria,omitempty"`
	ExecuteAllowed   bool                                `json:"executeAllowed,omitempty"`
	HumanApproved    bool                                `json:"humanApproved,omitempty"`
	ApprovalNote     string                              `json:"approvalNote,omitempty"`
	ApprovalSource   string                              `json:"approvalSourceId,omitempty"`
	CoordinationPlan plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
}

// ReviewRequestDigest hashes the redacted, action-defining request projection.
// Approval flags and notes are deliberately excluded so an approved rerun maps
// to the same reviewed action. Secret values are never retained in the digest
// input; credentials must come from the controlled credential boundary.
func ReviewRequestDigest(ownerIdentity string, request IntakeRequest) (string, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return "", err
	}
	if requestOwner := strings.TrimSpace(request.OwnerIdentity); requestOwner != "" && requestOwner != ownerIdentity {
		return "", fmt.Errorf("%w: request owner differs from repository owner", ErrTaskReviewBindingMismatch)
	}
	requestText := strings.TrimSpace(request.Request)
	if requestText == "" {
		return "", fmt.Errorf("review request is required")
	}
	projection := struct {
		OwnerIdentity    string                              `json:"ownerIdentity"`
		PursuitID        string                              `json:"pursuitId,omitempty"`
		WorkflowID       string                              `json:"workflowId,omitempty"`
		Request          string                              `json:"request"`
		ProjectKey       string                              `json:"projectKey,omitempty"`
		AutomationID     string                              `json:"automationId,omitempty"`
		MandateID        string                              `json:"mandateId,omitempty"`
		SuccessCriteria  []string                            `json:"successCriteria,omitempty"`
		CoordinationPlan plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
	}{
		OwnerIdentity:    ownerIdentity,
		PursuitID:        strings.TrimSpace(request.PursuitID),
		WorkflowID:       strings.TrimSpace(request.WorkflowID),
		Request:          requestText,
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		AutomationID:     strings.TrimSpace(request.AutomationID),
		MandateID:        strings.TrimSpace(request.MandateID),
		SuccessCriteria:  append([]string(nil), request.SuccessCriteria...),
		CoordinationPlan: request.CoordinationPlan,
	}
	payload, _, err := marshalSanitizedJSONObject(projection)
	if err != nil {
		return "", fmt.Errorf("encode review request digest: %w", err)
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

// reviewRequestDigestV1 verifies records written before mandate and accepted
// plan-revision provenance became part of the reviewed action identity. It may
// only be used when those newer fields are absent, otherwise accepting the old
// digest would leave action-defining data outside the integrity boundary.
func reviewRequestDigestV1(ownerIdentity string, request IntakeRequest) (string, error) {
	if strings.TrimSpace(request.MandateID) != "" || !request.CoordinationPlan.IsZero() {
		return "", fmt.Errorf("legacy review digest cannot cover mandate or coordination provenance")
	}
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return "", err
	}
	if requestOwner := strings.TrimSpace(request.OwnerIdentity); requestOwner != "" && requestOwner != ownerIdentity {
		return "", fmt.Errorf("%w: request owner differs from repository owner", ErrTaskReviewBindingMismatch)
	}
	requestText := strings.TrimSpace(request.Request)
	if requestText == "" {
		return "", fmt.Errorf("review request is required")
	}
	projection := struct {
		OwnerIdentity   string   `json:"ownerIdentity"`
		PursuitID       string   `json:"pursuitId,omitempty"`
		WorkflowID      string   `json:"workflowId,omitempty"`
		Request         string   `json:"request"`
		ProjectKey      string   `json:"projectKey,omitempty"`
		AutomationID    string   `json:"automationId,omitempty"`
		SuccessCriteria []string `json:"successCriteria,omitempty"`
	}{
		OwnerIdentity:   ownerIdentity,
		PursuitID:       strings.TrimSpace(request.PursuitID),
		WorkflowID:      strings.TrimSpace(request.WorkflowID),
		Request:         requestText,
		ProjectKey:      strings.TrimSpace(request.ProjectKey),
		AutomationID:    strings.TrimSpace(request.AutomationID),
		SuccessCriteria: append([]string(nil), request.SuccessCriteria...),
	}
	payload, _, err := marshalSanitizedJSONObject(projection)
	if err != nil {
		return "", fmt.Errorf("encode legacy review request digest: %w", err)
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

func completionPlanToModel(ownerIdentity string, plan CompletionPlan) (models.TaskCompletionPlanLog, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return models.TaskCompletionPlanLog{}, err
	}
	if planOwner := strings.TrimSpace(plan.OwnerIdentity); planOwner != "" && planOwner != ownerIdentity {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("completion plan owner differs from repository owner")
	}
	plan.ID = strings.TrimSpace(plan.ID)
	if plan.ID == "" || len([]rune(plan.ID)) > 160 {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("task plan id must contain 1 to 160 characters")
	}
	plan.ReviewItemID = strings.TrimSpace(plan.ReviewItemID)
	if plan.ReviewItemID != "" {
		if reviewID, parseErr := uuid.Parse(plan.ReviewItemID); parseErr != nil || reviewID == uuid.Nil {
			return models.TaskCompletionPlanLog{}, fmt.Errorf("completion plan review item id must be a UUID")
		}
	}
	plan.CompletionStatus = strings.TrimSpace(plan.CompletionStatus)
	if plan.CompletionStatus == "" || len([]rune(plan.CompletionStatus)) > 80 {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("completion status must contain 1 to 80 characters")
	}
	if plan.CreatedAt.IsZero() {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("completion plan createdAt is required")
	}
	plan.CreatedAt = normalizeTaskStateTimestamp(plan.CreatedAt)
	plan.OwnerIdentity = ownerIdentity
	payload, digest, err := marshalSanitizedJSONObject(plan)
	if err != nil {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("encode completion plan: %w", err)
	}
	var roundTrip CompletionPlan
	if err := decodeStrictJSONObject(payload, &roundTrip); err != nil {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("validate completion plan payload: %w", err)
	}
	if roundTrip.ID != plan.ID ||
		roundTrip.ReviewItemID != plan.ReviewItemID ||
		roundTrip.CompletionStatus != plan.CompletionStatus ||
		!roundTrip.CreatedAt.Equal(plan.CreatedAt) {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("completion plan identity metadata was altered during safe serialization")
	}
	verificationStatus := completionPlanVerificationStatus(roundTrip)
	if verificationStatus == "" || len([]rune(verificationStatus)) > 80 {
		return models.TaskCompletionPlanLog{}, fmt.Errorf("verification status must contain 1 to 80 characters")
	}
	return models.TaskCompletionPlanLog{
		ID:                 uuid.New(),
		OwnerIdentity:      ownerIdentity,
		TaskPlanID:         plan.ID,
		CompletionStatus:   plan.CompletionStatus,
		VerificationStatus: verificationStatus,
		PayloadJSON:        payload,
		PayloadDigest:      digest,
		ProvenanceSource:   taskCompletionProvenance,
		CreatedAt:          plan.CreatedAt.UTC(),
	}, nil
}

func completionPlanFromModel(row models.TaskCompletionPlanLog) (CompletionPlan, error) {
	if err := validateCompletionPlanRow(row); err != nil {
		return CompletionPlan{}, err
	}
	if err := validateStoredDigest(row.PayloadJSON, row.PayloadDigest); err != nil {
		return CompletionPlan{}, fmt.Errorf("completion plan %s: %w", row.TaskPlanID, err)
	}
	var plan CompletionPlan
	if err := decodeStrictJSONObject(row.PayloadJSON, &plan); err != nil {
		return CompletionPlan{}, fmt.Errorf("decode completion plan %s: %w", row.TaskPlanID, err)
	}
	if strings.TrimSpace(plan.ID) != row.TaskPlanID ||
		strings.TrimSpace(plan.CompletionStatus) != row.CompletionStatus ||
		completionPlanVerificationStatus(plan) != row.VerificationStatus ||
		!plan.CreatedAt.Equal(row.CreatedAt.UTC()) ||
		row.ProvenanceSource != taskCompletionProvenance {
		return CompletionPlan{}, fmt.Errorf("completion plan %s metadata does not match its immutable payload", row.TaskPlanID)
	}
	plan.OwnerIdentity = row.OwnerIdentity
	return plan, nil
}

func reviewItemToModel(ownerIdentity string, item ReviewQueueItem) (models.TaskReviewItemRecord, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return models.TaskReviewItemRecord{}, err
	}
	id, err := uuid.Parse(strings.TrimSpace(item.ID))
	if err != nil || id == uuid.Nil {
		return models.TaskReviewItemRecord{}, fmt.Errorf("review item id must be a UUID")
	}
	taskPlanID := strings.TrimSpace(item.TaskID)
	if taskPlanID == "" || len([]rune(taskPlanID)) > 160 {
		return models.TaskReviewItemRecord{}, fmt.Errorf("review task plan id must contain 1 to 160 characters")
	}
	if item.CreatedAt.IsZero() {
		return models.TaskReviewItemRecord{}, fmt.Errorf("review item createdAt is required")
	}
	item.CreatedAt = normalizeTaskStateTimestamp(item.CreatedAt)
	status := strings.TrimSpace(item.Status)
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "needs_review" {
		return models.TaskReviewItemRecord{}, fmt.Errorf("new review item must be open or needs_review")
	}
	priority, err := normalizeReviewPriority(item.Priority)
	if err != nil {
		return models.TaskReviewItemRecord{}, err
	}
	reason := sanitizeTaskOperationalText(item.Reason, 4096)
	if reason == "" {
		return models.TaskReviewItemRecord{}, fmt.Errorf("review reason is required")
	}
	request := item.Request
	if requestOwner := strings.TrimSpace(request.OwnerIdentity); requestOwner != "" && requestOwner != ownerIdentity {
		return models.TaskReviewItemRecord{}, fmt.Errorf("%w: request owner differs from repository owner", ErrTaskReviewBindingMismatch)
	}
	request.OwnerIdentity = ownerIdentity
	request.ApprovalNote = ""
	request.ApprovalSourceID = ""
	request.HumanApproved = false
	request.ExecuteAllowed = false
	request.reviewItemID = ""
	requestDigest, err := ReviewRequestDigest(ownerIdentity, request)
	if err != nil {
		return models.TaskReviewItemRecord{}, err
	}
	requestJSON, err := encodeStoredReviewRequest(request)
	if err != nil {
		return models.TaskReviewItemRecord{}, fmt.Errorf("encode review request: %w", err)
	}
	roundTrip, err := decodeStoredReviewRequest(requestJSON)
	if err != nil {
		return models.TaskReviewItemRecord{}, fmt.Errorf("validate review request payload: %w", err)
	}
	if err := validateStoredReviewRequest(roundTrip); err != nil {
		return models.TaskReviewItemRecord{}, err
	}
	now := item.CreatedAt.UTC()
	return models.TaskReviewItemRecord{
		ID:                 id,
		OwnerIdentity:      ownerIdentity,
		OriginalTaskPlanID: taskPlanID,
		CurrentTaskPlanID:  taskPlanID,
		RequestDigest:      requestDigest,
		RequestJSON:        requestJSON,
		Reason:             reason,
		Priority:           priority,
		Status:             status,
		ReviewRevision:     1,
		CreatedAt:          now,
		UpdatedAt:          now,
		ResolvedAt:         nil,
	}, nil
}

func reviewItemFromModel(row models.TaskReviewItemRecord, latest *models.TaskReviewDecisionRecord) (ReviewQueueItem, error) {
	if err := validateReviewItemRow(row); err != nil {
		return ReviewQueueItem{}, err
	}
	request, err := decodeStoredReviewRequest(row.RequestJSON)
	if err != nil {
		return ReviewQueueItem{}, fmt.Errorf("decode review item %s request: %w", row.ID, err)
	}
	if err := validateStoredReviewRequest(request); err != nil {
		return ReviewQueueItem{}, fmt.Errorf("review item %s: %w", row.ID, err)
	}
	request.OwnerIdentity = row.OwnerIdentity
	digest, err := ReviewRequestDigest(row.OwnerIdentity, request)
	if err != nil {
		return ReviewQueueItem{}, fmt.Errorf("verify review item %s request: %w", row.ID, err)
	}
	if digest != row.RequestDigest {
		legacyDigest, legacyErr := reviewRequestDigestV1(row.OwnerIdentity, request)
		if legacyErr != nil || legacyDigest != row.RequestDigest {
			return ReviewQueueItem{}, fmt.Errorf("review item %s request digest mismatch", row.ID)
		}
	}
	item := ReviewQueueItem{
		ID:         row.ID.String(),
		TaskID:     row.CurrentTaskPlanID,
		Request:    request,
		Reason:     row.Reason,
		Priority:   row.Priority,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt.UTC(),
		ResolvedAt: cloneTaskStateTime(row.ResolvedAt),
	}
	if latest != nil {
		if _, err := reviewDecisionFromModel(*latest); err != nil {
			return ReviewQueueItem{}, err
		}
		if latest.OwnerIdentity != row.OwnerIdentity ||
			latest.ReviewItemID != row.ID ||
			latest.RequestDigest != row.RequestDigest ||
			latest.ReviewRevision > row.ReviewRevision {
			return ReviewQueueItem{}, fmt.Errorf("review item %s latest decision provenance mismatch", row.ID)
		}
	}
	switch row.Status {
	case "open":
		if latest != nil {
			return ReviewQueueItem{}, fmt.Errorf("review item %s has a decision while still open", row.ID)
		}
	case "approved", "rejected":
		if latest == nil ||
			latest.Decision != row.Status ||
			latest.ReviewRevision != row.ReviewRevision ||
			latest.TaskPlanID != row.CurrentTaskPlanID ||
			row.ResolvedAt == nil ||
			!row.ResolvedAt.Equal(latest.ResolvedAt) {
			return ReviewQueueItem{}, fmt.Errorf("review item %s resolution does not match its immutable decision", row.ID)
		}
		item.Decision = latest.Decision
		item.ResolutionNote = latest.ResolutionNote
	case "completed":
		if latest == nil ||
			latest.Decision != "approved" ||
			latest.ReviewRevision != row.ReviewRevision ||
			row.ResolvedAt == nil ||
			row.ResolvedAt.Before(latest.ResolvedAt) {
			return ReviewQueueItem{}, fmt.Errorf("review item %s completion is not backed by its active approval", row.ID)
		}
		item.Decision = latest.Decision
		item.ResolutionNote = latest.ResolutionNote
	case "needs_review":
		if row.ReviewRevision > 1 &&
			(latest == nil ||
				latest.Decision != "approved" ||
				latest.ReviewRevision != row.ReviewRevision-1) {
			return ReviewQueueItem{}, fmt.Errorf("review item %s retry state is not backed by the prior approval", row.ID)
		}
	}
	return item, nil
}

func reviewDecisionFromModel(row models.TaskReviewDecisionRecord) (ReviewDecisionRecord, error) {
	if row.ID == uuid.Nil || row.ReviewItemID == uuid.Nil {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision has an invalid id")
	}
	ownerIdentity, err := normalizeTaskStateOwner(row.OwnerIdentity)
	if err != nil {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s: %w", row.ID, err)
	}
	if row.ReviewRevision < 1 {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has an invalid review revision", row.ID)
	}
	taskPlanID := strings.TrimSpace(row.TaskPlanID)
	if taskPlanID == "" || taskPlanID != row.TaskPlanID || len([]rune(taskPlanID)) > 160 {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has an invalid task plan id", row.ID)
	}
	if row.Decision != "approved" && row.Decision != "rejected" {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has invalid decision %q", row.ID, row.Decision)
	}
	if row.ResolvedBy != ownerIdentity {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s resolver does not match its owner", row.ID)
	}
	if len([]rune(row.ResolutionNote)) > taskStateMaximumResolutionNoteRunes {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s resolution note exceeds the storage limit", row.ID)
	}
	if row.ApprovalSource != taskReviewApprovalSource ||
		row.ApprovalSourceID != taskReviewApprovalSource+":"+row.ReviewItemID.String() {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has invalid approval provenance", row.ID)
	}
	if !validSHA256Digest(row.RequestDigest) {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has invalid request digest", row.ID)
	}
	if row.ResolvedAt.IsZero() {
		return ReviewDecisionRecord{}, fmt.Errorf("review decision %s has no resolution timestamp", row.ID)
	}
	return ReviewDecisionRecord{
		ID:               row.ID.String(),
		ReviewItemID:     row.ReviewItemID.String(),
		ReviewRevision:   row.ReviewRevision,
		TaskPlanID:       row.TaskPlanID,
		Decision:         row.Decision,
		ResolutionNote:   row.ResolutionNote,
		ResolvedBy:       row.ResolvedBy,
		ApprovalSource:   row.ApprovalSource,
		ApprovalSourceID: row.ApprovalSourceID,
		RequestDigest:    row.RequestDigest,
		ResolvedAt:       row.ResolvedAt.UTC(),
	}, nil
}

func newReviewDecisionModel(ownerIdentity string, item models.TaskReviewItemRecord, resolution ReviewResolution) (models.TaskReviewDecisionRecord, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return models.TaskReviewDecisionRecord{}, err
	}
	if item.OwnerIdentity != ownerIdentity {
		return models.TaskReviewDecisionRecord{}, ErrTaskReviewBindingMismatch
	}
	decision := strings.ToLower(strings.TrimSpace(resolution.Decision))
	if decision != "approved" && decision != "rejected" {
		return models.TaskReviewDecisionRecord{}, fmt.Errorf("review decision must be approved or rejected")
	}
	resolvedAt := resolution.ResolvedAt
	if resolvedAt.IsZero() {
		resolvedAt = time.Now().UTC()
	}
	resolvedAt = normalizeTaskStateTimestamp(resolvedAt)
	if resolvedAt.Before(item.UpdatedAt.UTC()) {
		return models.TaskReviewDecisionRecord{}, fmt.Errorf("review resolution cannot predate the current review state")
	}
	return models.TaskReviewDecisionRecord{
		ID:               uuid.New(),
		ReviewItemID:     item.ID,
		ReviewRevision:   item.ReviewRevision,
		OwnerIdentity:    ownerIdentity,
		TaskPlanID:       item.CurrentTaskPlanID,
		Decision:         decision,
		ResolutionNote:   sanitizeApprovalNote(resolution.Note),
		ResolvedBy:       ownerIdentity,
		ApprovalSource:   taskReviewApprovalSource,
		ApprovalSourceID: taskReviewApprovalSource + ":" + item.ID.String(),
		RequestDigest:    item.RequestDigest,
		ResolvedAt:       resolvedAt,
	}, nil
}

func completionPlanVerificationStatus(plan CompletionPlan) string {
	if plan.ExecutionResult != nil && strings.TrimSpace(plan.ExecutionResult.VerificationStatus) != "" {
		return sanitizeTaskStateMetadata(plan.ExecutionResult.VerificationStatus)
	}
	if strings.TrimSpace(plan.ValidationResult.Status) != "" {
		return sanitizeTaskStateMetadata(plan.ValidationResult.Status)
	}
	return "not_run"
}

func normalizeTaskStateOwner(ownerIdentity string) (string, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return "", fmt.Errorf("owner identity is required")
	}
	if len([]rune(ownerIdentity)) > 255 {
		return "", fmt.Errorf("owner identity exceeds 255 characters")
	}
	return ownerIdentity, nil
}

func normalizeTaskStateLimit(limit int) int {
	if limit <= 0 {
		return taskStateDefaultLimit
	}
	if limit > taskStateMaximumLimit {
		return taskStateMaximumLimit
	}
	return limit
}

func normalizeReviewPriority(priority string) (string, error) {
	priority = strings.ToLower(strings.TrimSpace(priority))
	if priority == "" {
		priority = "normal"
	}
	switch priority {
	case "low", "normal", "high", "critical":
		return priority, nil
	default:
		return "", fmt.Errorf("invalid review priority %q", priority)
	}
}

func marshalSanitizedJSONObject(value interface{}) (string, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", "", err
	}
	if len(raw) > taskStateMaximumPayloadSize {
		return "", "", fmt.Errorf("payload exceeds %d bytes", taskStateMaximumPayloadSize)
	}
	var decoded interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "", "", err
	}
	if _, ok := decoded.(map[string]interface{}); !ok {
		return "", "", fmt.Errorf("payload must be a JSON object")
	}
	redactTaskStateJSONStrings(decoded)
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", "", err
	}
	if len(canonical) > taskStateMaximumPayloadSize {
		return "", "", fmt.Errorf("payload exceeds %d bytes", taskStateMaximumPayloadSize)
	}
	digest := sha256.Sum256(canonical)
	return string(canonical), hex.EncodeToString(digest[:]), nil
}

func redactTaskStateJSONStrings(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			if text, ok := child.(string); ok {
				typed[key] = boundedTaskStateText(safety.RedactSecrets(text))
				continue
			}
			redactTaskStateJSONStrings(child)
		}
	case []interface{}:
		for index, child := range typed {
			if text, ok := child.(string); ok {
				typed[index] = boundedTaskStateText(safety.RedactSecrets(text))
				continue
			}
			redactTaskStateJSONStrings(child)
		}
	}
}

func boundedTaskStateText(value string) string {
	runes := []rune(value)
	if len(runes) <= taskStateMaximumStringRunes {
		return value
	}
	return string(runes[:taskStateMaximumStringRunes-3]) + "..."
}

func decodeStrictJSONObject(payload string, destination interface{}) error {
	if _, err := canonicalTaskStateJSONObject(payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func validateStoredDigest(payload, digest string) error {
	if !validSHA256Digest(digest) {
		return fmt.Errorf("stored payload digest is invalid")
	}
	canonical, err := canonicalTaskStateJSONObject(payload)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != digest {
		return fmt.Errorf("stored payload digest mismatch")
	}
	return nil
}

func canonicalTaskStateJSONObject(payload string) ([]byte, error) {
	if len(payload) == 0 || len(payload) > taskStateMaximumPayloadSize {
		return nil, fmt.Errorf("JSON payload size is invalid")
	}
	if err := rejectDuplicateJSONKeys([]byte(payload)); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("malformed JSON object: %w", err)
	}
	object, ok := decoded.(map[string]interface{})
	if !ok || object == nil {
		return nil, fmt.Errorf("payload must be a JSON object")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON object: %w", err)
	}
	if len(canonical) > taskStateMaximumPayloadSize {
		return nil, fmt.Errorf("payload exceeds %d bytes", taskStateMaximumPayloadSize)
	}
	return canonical, nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeTaskStateJSONValue(decoder); err != nil {
		return fmt.Errorf("malformed JSON object: %w", err)
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed after %v", token)
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func consumeTaskStateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeTaskStateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object is not terminated")
		}
	case '[':
		for decoder.More() {
			if err := consumeTaskStateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array is not terminated")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func cloneTaskStateTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func validateCompletionPlanRow(row models.TaskCompletionPlanLog) error {
	if row.ID == uuid.Nil {
		return fmt.Errorf("completion plan row has an invalid id")
	}
	ownerIdentity, err := normalizeTaskStateOwner(row.OwnerIdentity)
	if err != nil || ownerIdentity != row.OwnerIdentity {
		return fmt.Errorf("completion plan %s has an invalid owner", row.TaskPlanID)
	}
	taskPlanID := strings.TrimSpace(row.TaskPlanID)
	if taskPlanID == "" || taskPlanID != row.TaskPlanID || len([]rune(taskPlanID)) > 160 {
		return fmt.Errorf("completion plan row has an invalid task plan id")
	}
	status := strings.TrimSpace(row.CompletionStatus)
	if status == "" || status != row.CompletionStatus || len([]rune(status)) > 80 {
		return fmt.Errorf("completion plan %s has an invalid completion status", row.TaskPlanID)
	}
	verificationStatus := strings.TrimSpace(row.VerificationStatus)
	if verificationStatus == "" || verificationStatus != row.VerificationStatus || len([]rune(verificationStatus)) > 80 {
		return fmt.Errorf("completion plan %s has an invalid verification status", row.TaskPlanID)
	}
	if row.ProvenanceSource != taskCompletionProvenance {
		return fmt.Errorf("completion plan %s has invalid provenance", row.TaskPlanID)
	}
	if row.CreatedAt.IsZero() {
		return fmt.Errorf("completion plan %s has no creation timestamp", row.TaskPlanID)
	}
	return nil
}

func validateReviewItemRow(row models.TaskReviewItemRecord) error {
	if row.ID == uuid.Nil {
		return fmt.Errorf("review item has an invalid id")
	}
	ownerIdentity, err := normalizeTaskStateOwner(row.OwnerIdentity)
	if err != nil || ownerIdentity != row.OwnerIdentity {
		return fmt.Errorf("review item %s has an invalid owner", row.ID)
	}
	for label, value := range map[string]string{
		"original task plan id": row.OriginalTaskPlanID,
		"current task plan id":  row.CurrentTaskPlanID,
	} {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed != value || len([]rune(trimmed)) > 160 {
			return fmt.Errorf("review item %s has an invalid %s", row.ID, label)
		}
	}
	if !validSHA256Digest(row.RequestDigest) {
		return fmt.Errorf("review item %s has an invalid request digest", row.ID)
	}
	if strings.TrimSpace(row.Reason) == "" || len([]rune(row.Reason)) > taskStateMaximumReasonRunes {
		return fmt.Errorf("review item %s has an invalid reason", row.ID)
	}
	if _, err := normalizeReviewPriority(row.Priority); err != nil {
		return fmt.Errorf("review item %s: %w", row.ID, err)
	}
	if row.ReviewRevision < 1 {
		return fmt.Errorf("review item %s has an invalid review revision", row.ID)
	}
	if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || row.UpdatedAt.Before(row.CreatedAt) {
		return fmt.Errorf("review item %s has invalid timestamps", row.ID)
	}
	switch row.Status {
	case "open", "needs_review":
		if row.ResolvedAt != nil {
			return fmt.Errorf("review item %s is unresolved but has a resolution timestamp", row.ID)
		}
	case "approved", "rejected", "completed":
		if row.ResolvedAt == nil || row.ResolvedAt.Before(row.CreatedAt) || row.UpdatedAt.Before(*row.ResolvedAt) {
			return fmt.Errorf("review item %s has an invalid resolution timestamp", row.ID)
		}
	default:
		return fmt.Errorf("review item %s has invalid status %q", row.ID, row.Status)
	}
	return nil
}

func validateStoredReviewRequest(request IntakeRequest) error {
	if request.ExecuteAllowed ||
		request.HumanApproved ||
		strings.TrimSpace(request.ApprovalNote) != "" ||
		strings.TrimSpace(request.ApprovalSourceID) != "" ||
		strings.TrimSpace(request.reviewItemID) != "" {
		return fmt.Errorf("stored review request contains transient approval state")
	}
	return nil
}

func encodeStoredReviewRequest(request IntakeRequest) (string, error) {
	payload, _, err := marshalSanitizedJSONObject(storedReviewRequest{
		PursuitID:        request.PursuitID,
		WorkflowID:       request.WorkflowID,
		Request:          request.Request,
		ProjectKey:       request.ProjectKey,
		AutomationID:     request.AutomationID,
		MandateID:        request.MandateID,
		SuccessCriteria:  append([]string(nil), request.SuccessCriteria...),
		ExecuteAllowed:   request.ExecuteAllowed,
		HumanApproved:    request.HumanApproved,
		ApprovalNote:     request.ApprovalNote,
		ApprovalSource:   request.ApprovalSourceID,
		CoordinationPlan: request.CoordinationPlan,
	})
	if err != nil {
		return "", err
	}
	return payload, nil
}

func decodeStoredReviewRequest(payload string) (IntakeRequest, error) {
	var stored storedReviewRequest
	if err := decodeStrictJSONObject(payload, &stored); err != nil {
		return IntakeRequest{}, err
	}
	return IntakeRequest{
		PursuitID:        stored.PursuitID,
		WorkflowID:       stored.WorkflowID,
		Request:          stored.Request,
		ProjectKey:       stored.ProjectKey,
		AutomationID:     stored.AutomationID,
		MandateID:        stored.MandateID,
		SuccessCriteria:  append([]string(nil), stored.SuccessCriteria...),
		ExecuteAllowed:   stored.ExecuteAllowed,
		HumanApproved:    stored.HumanApproved,
		ApprovalNote:     stored.ApprovalNote,
		ApprovalSourceID: stored.ApprovalSource,
		CoordinationPlan: stored.CoordinationPlan,
	}, nil
}

func validateReviewDecisionBinding(
	decision models.TaskReviewDecisionRecord,
	item models.TaskReviewItemRecord,
) error {
	if decision.OwnerIdentity != item.OwnerIdentity ||
		decision.ReviewItemID != item.ID ||
		decision.RequestDigest != item.RequestDigest ||
		decision.ReviewRevision < 1 ||
		decision.ReviewRevision > item.ReviewRevision ||
		decision.ResolvedAt.Before(item.CreatedAt) {
		return fmt.Errorf("%w: decision does not match its review item", ErrTaskReviewBindingMismatch)
	}
	return nil
}

func normalizeTaskStateTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(taskStateStorageTimestampGranularity)
}

func sanitizeTaskStateMetadata(value string) string {
	return strings.TrimSpace(boundedTaskStateText(safety.RedactSecrets(value)))
}
