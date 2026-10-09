package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

const sourceApprovalReceiptSchema = "hai.source-operation-approval.v1"
const sourceApprovalSafeRuntimeID = "hai-local-safe-worker"
const sourceApprovalEventPageSize = 100
const sourceApprovalEventHistoryLimit = 10000

var (
	ErrSourceApprovalRequired = errors.New("operations: exact owner approval receipt required")
	ErrSourceApprovalBinding  = errors.New("operations: reviewed operation version and digest are required")
	ErrSourceApprovalStale    = errors.New("operations: source approval is stale for this operation revision")
	ErrSourceApprovalReplayed = errors.New("operations: source approval receipt was already consumed")
	ErrSourceApprovalHistory  = errors.New("operations: source approval audit history is unavailable or exceeds the bounded verification limit")
)

// SourceApprovalRepository fences source-derived approval against the
// currently accepted source authority. Implementations lock origin, head,
// then operation, matching publication and the final safe-effect boundary.
type SourceApprovalRepository interface {
	ValidateSourceApproval(context.Context, models.Operation, int64, string) error
	ApproveSourceDerived(context.Context, models.Operation, models.Operation, models.OperationEvent, string) (*models.Operation, error)
}

// SourceApprovalReceipt is embedded in immutable operation audit events. Its
// digest binds the reviewed action and source revision; Version binds the
// ledger revision presented to the owner.
type SourceApprovalReceipt struct {
	Schema            string    `json:"schema"`
	ID                uuid.UUID `json:"id"`
	OperationID       uuid.UUID `json:"operationId"`
	OwnerUserID       string    `json:"ownerUserId"`
	WorkspaceID       string    `json:"workspaceId"`
	ApprovedBy        string    `json:"approvedBy"`
	ReviewedOwnerType string    `json:"reviewedOwnerType,omitempty"`
	ApprovedAt        time.Time `json:"approvedAt"`
	ReviewedUpdatedAt time.Time `json:"reviewedUpdatedAt"`
	Version           int64     `json:"version"`
	RevisionDigest    string    `json:"revisionDigest"`
}

type sourceApprovalEventPayload struct {
	SourceApproval          *SourceApprovalReceipt `json:"sourceApproval,omitempty"`
	ConsumedReceipt         string                 `json:"consumedReceipt,omitempty"`
	RevisionDigest          string                 `json:"revisionDigest,omitempty"`
	ExecutionRevisionDigest string                 `json:"executionRevisionDigest,omitempty"`
}

// IsSourceDerived reports whether any immutable source identity binds the
// operation to externally supplied content.
func IsSourceDerived(op models.Operation) bool {
	return op.SourceIdentityHash != "" || op.SourceID != nil || op.AccountFeedID != nil ||
		op.SourceObservationID != nil || op.SourceRevisionHash != ""
}

// SourceApprovalPreview provides a stable version/digest pair for an
// authenticated owner to review before approving a source-derived operation.
type SourceApprovalPreview struct {
	OperationID    uuid.UUID `json:"operationId"`
	Version        int64     `json:"version"`
	RevisionDigest string    `json:"revisionDigest"`
}

// PreviewSourceApproval computes the operation-only review binding. Callers
// serving a live approval preview must use Service.PreviewSourceApproval,
// which also checks accepted source authority for identified operations.
func PreviewSourceApproval(op models.Operation) (SourceApprovalPreview, error) {
	if !IsSourceDerived(op) || op.Status != string(StatusAwaitingApproval) || !op.RequiresApproval {
		return SourceApprovalPreview{}, ErrSourceApprovalRequired
	}
	digest, err := sourceOperationRevisionDigest(op)
	if err != nil {
		return SourceApprovalPreview{}, err
	}
	return SourceApprovalPreview{OperationID: op.ID, Version: op.Version, RevisionDigest: digest}, nil
}

// Service.PreviewSourceApproval should be used for current approval previews;
// it additionally checks identified operations against their accepted head.
func (s *Service) PreviewSourceApproval(op models.Operation) (SourceApprovalPreview, error) {
	return s.PreviewSourceApprovalContext(context.Background(), op)
}

func (s *Service) PreviewSourceApprovalContext(ctx context.Context, op models.Operation) (SourceApprovalPreview, error) {
	preview, err := PreviewSourceApproval(op)
	if err != nil {
		return SourceApprovalPreview{}, err
	}
	if op.SourceIdentityHash == "" {
		return preview, nil
	}
	if s == nil || s.repo == nil {
		return SourceApprovalPreview{}, ErrSourceHeadUnsupported
	}
	repository, ok := s.repo.(SourceApprovalRepository)
	if !ok {
		return SourceApprovalPreview{}, ErrSourceHeadUnsupported
	}
	if err := repository.ValidateSourceApproval(ctx, op, preview.Version, preview.RevisionDigest); err != nil {
		if errors.Is(err, ErrSourceHeadSuperseded) || errors.Is(err, ErrSourceHeadReconciliation) || errors.Is(err, ErrStaleOperation) {
			return SourceApprovalPreview{}, ErrSourceApprovalStale
		}
		return SourceApprovalPreview{}, err
	}
	return preview, nil
}

func validateSourceApprovalReceiptEvent(expected, updated models.Operation, event models.OperationEvent, digest string) error {
	var payload sourceApprovalEventPayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil || payload.SourceApproval == nil {
		return ErrSourceApprovalStale
	}
	receipt := payload.SourceApproval
	if event.EventType != "status_change" || event.OperationID != expected.ID || event.BeforeStatus != string(StatusAwaitingApproval) ||
		event.AfterStatus != string(StatusApproved) || event.ActorType != string(OwnerRobert) || event.ActorID != receipt.ApprovedBy ||
		receipt.Schema != sourceApprovalReceiptSchema || receipt.ID == uuid.Nil || receipt.OperationID != expected.ID ||
		receipt.OwnerUserID != expected.OwnerUserID || receipt.WorkspaceID != expected.WorkspaceID || receipt.ApprovedBy == "" ||
		(receipt.ReviewedOwnerType != "" && receipt.ReviewedOwnerType != expected.OwnerType) ||
		receipt.Version != expected.Version || !strings.EqualFold(receipt.RevisionDigest, digest) ||
		!receipt.ReviewedUpdatedAt.Equal(expected.UpdatedAt.UTC()) || !receipt.ApprovedAt.Equal(event.CreatedAt) ||
		!updated.UpdatedAt.Equal(event.CreatedAt) || updated.Status != string(StatusApproved) || updated.Version != expected.Version+1 {
		return ErrSourceApprovalStale
	}
	normalized := updated
	normalized.Status = expected.Status
	normalized.OwnerType = expected.OwnerType
	normalized.UpdatedAt = expected.UpdatedAt
	normalized.Version = expected.Version
	if !sourceOperationRevisionDigestMatches(normalized, expected) {
		return ErrSourceApprovalStale
	}
	return nil
}

// ApproveSourceDerived atomically transitions one exact revision and appends
// its durable approval receipt as the status-change audit event.
func (s *Service) ApproveSourceDerived(op models.Operation, actorID string, expectedVersion int64, expectedDigest string) (*models.Operation, SourceApprovalReceipt, error) {
	return s.ApproveSourceDerivedContext(context.Background(), op, actorID, expectedVersion, expectedDigest)
}

func (s *Service) ApproveSourceDerivedContext(ctx context.Context, op models.Operation, actorID string, expectedVersion int64, expectedDigest string) (*models.Operation, SourceApprovalReceipt, error) {
	return s.approveSourceDerived(ctx, op, actorID, expectedVersion, expectedDigest, false)
}

// ApproveSourceDerivedAsOwner is for an HTTP handler that has already verified
// the installation owner role and loaded an operation from its configured
// worker scope. The verified principal is retained as the audit actor; it need
// not equal the worker's static owner identifier.
func (s *Service) ApproveSourceDerivedAsOwner(op models.Operation, actorID string, expectedVersion int64, expectedDigest string) (*models.Operation, SourceApprovalReceipt, error) {
	return s.ApproveSourceDerivedAsOwnerContext(context.Background(), op, actorID, expectedVersion, expectedDigest)
}

func (s *Service) ApproveSourceDerivedAsOwnerContext(ctx context.Context, op models.Operation, actorID string, expectedVersion int64, expectedDigest string) (*models.Operation, SourceApprovalReceipt, error) {
	return s.approveSourceDerived(ctx, op, actorID, expectedVersion, expectedDigest, true)
}

func (s *Service) approveSourceDerived(ctx context.Context, op models.Operation, actorID string, expectedVersion int64, expectedDigest string, ownerAuthorized bool) (*models.Operation, SourceApprovalReceipt, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || (!ownerAuthorized && actorID != op.OwnerUserID) || !IsSourceDerived(op) ||
		op.Status != string(StatusAwaitingApproval) || !op.RequiresApproval {
		return nil, SourceApprovalReceipt{}, ErrSourceApprovalRequired
	}
	preview, err := s.PreviewSourceApprovalContext(ctx, op)
	if err != nil {
		return nil, SourceApprovalReceipt{}, err
	}
	if expectedVersion <= 0 || strings.TrimSpace(expectedDigest) == "" {
		return nil, SourceApprovalReceipt{}, ErrSourceApprovalBinding
	}
	if expectedVersion != preview.Version || !strings.EqualFold(expectedDigest, preview.RevisionDigest) {
		return nil, SourceApprovalReceipt{}, ErrSourceApprovalStale
	}
	now := sourceLedgerTime(s.now())
	receipt := SourceApprovalReceipt{
		Schema: sourceApprovalReceiptSchema, ID: uuid.New(), OperationID: op.ID,
		OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID,
		ApprovedBy: actorID, ReviewedOwnerType: op.OwnerType, ApprovedAt: now,
		ReviewedUpdatedAt: op.UpdatedAt.UTC(), Version: op.Version,
		RevisionDigest: preview.RevisionDigest,
	}
	reviewed := op
	op.OwnerType = string(OwnerRobert)
	updated, event, err := ApplyTransition(op, StatusApproved, string(OwnerRobert), actorID, "owner approved exact source-derived operation revision", now)
	if err != nil {
		return nil, SourceApprovalReceipt{}, err
	}
	updated.OwnerType = string(OwnerRobert)
	payload, err := json.Marshal(sourceApprovalEventPayload{SourceApproval: &receipt})
	if err != nil {
		return nil, SourceApprovalReceipt{}, err
	}
	event.PayloadJSON = string(payload)
	var saved *models.Operation
	if op.SourceIdentityHash != "" {
		repository, ok := s.repo.(SourceApprovalRepository)
		if !ok {
			return nil, SourceApprovalReceipt{}, ErrSourceHeadUnsupported
		}
		saved, err = repository.ApproveSourceDerived(ctx, reviewed, updated, event, preview.RevisionDigest)
	} else {
		repository, repositoryErr := s.atomicMutationRepository()
		if repositoryErr != nil {
			return nil, SourceApprovalReceipt{}, repositoryErr
		}
		saved, err = repository.UpdateWithEvent(&updated, &event)
	}
	if err != nil {
		if errors.Is(err, ErrStaleOperation) || errors.Is(err, ErrSourceHeadSuperseded) || errors.Is(err, ErrSourceHeadReconciliation) {
			return nil, SourceApprovalReceipt{}, ErrSourceApprovalStale
		}
		return nil, SourceApprovalReceipt{}, err
	}
	return saved, receipt, nil
}

// SourceApprovalForExecution validates the durable approval against the
// currently approved operation. Any post-approval mutation invalidates it.
func (s *Service) SourceApprovalForExecution(op models.Operation) (SourceApprovalReceipt, error) {
	receipt, err := s.findSourceApproval(op)
	if err != nil {
		return SourceApprovalReceipt{}, err
	}
	if op.Status != string(StatusApproved) || op.Version != receipt.Version+1 || !sourceRevisionMatches(op, receipt) {
		return SourceApprovalReceipt{}, ErrSourceApprovalStale
	}
	return receipt, nil
}

// ConsumeSourceApprovalClaimed records the single-use receipt in the same
// fenced transaction as approved->running. A retry or replay is rejected.
func (s *Service) ConsumeSourceApprovalClaimed(ctx context.Context, claim ExecutionClaim, approvedSnapshot, prepared models.Operation, actorType, actorID string) (*models.Operation, error) {
	receipt, err := s.SourceApprovalForExecution(approvedSnapshot)
	if err != nil {
		return nil, err
	}
	if !sourceExecutionPreparationMatches(approvedSnapshot, prepared) {
		return nil, ErrSourceApprovalStale
	}
	events, err := s.listSourceApprovalEvents(approvedSnapshot.ID)
	if err != nil {
		return nil, err
	}
	if sourceApprovalConsumed(events, receipt) {
		return nil, ErrSourceApprovalReplayed
	}
	updated, event, err := ApplyTransition(prepared, StatusRunning, actorType, actorID, "consuming exact source approval before host effect", sourceLedgerTime(s.now()))
	if err != nil {
		return nil, err
	}
	executionDigest, err := sourceOperationRevisionDigest(updated)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(sourceApprovalEventPayload{
		ConsumedReceipt: receipt.ID.String(), RevisionDigest: receipt.RevisionDigest,
		ExecutionRevisionDigest: executionDigest,
	})
	if err != nil {
		return nil, err
	}
	event.PayloadJSON = string(payload)
	repository, ok := s.repo.(ClaimRepository)
	if !ok {
		return nil, ErrAtomicClaimsUnsupported
	}
	saved, err := repository.TransitionClaimed(ctx, claim, updated, event, false)
	if errors.Is(err, ErrStaleOperation) || errors.Is(err, ErrClaimLost) {
		return nil, ErrSourceApprovalStale
	}
	return saved, err
}

// ValidateConsumedSourceApproval is the final source-derived host-effect
// gate. It requires the exact receipt and its one-time consumption event.
func (s *Service) ValidateConsumedSourceApproval(op models.Operation) error {
	receipt, err := s.findSourceApproval(op)
	if err != nil {
		return err
	}
	if op.Status != string(StatusRunning) || op.Version != receipt.Version+2 || !sourceExecutionRuntimeValid(op) {
		return ErrSourceApprovalStale
	}
	events, err := s.listSourceApprovalEvents(op.ID)
	if err != nil {
		return err
	}
	consumed, ok := sourceApprovalConsumption(events, receipt)
	if !ok {
		return ErrSourceApprovalRequired
	}
	digest, err := sourceOperationRevisionDigest(op)
	if err != nil || digest != consumed.ExecutionRevisionDigest {
		return ErrSourceApprovalStale
	}
	return nil
}

func (s *Service) listSourceApprovalEvents(operationID uuid.UUID) ([]models.OperationEvent, error) {
	repository, ok := s.repo.(PaginatedEventRepository)
	if !ok {
		return nil, ErrSourceApprovalHistory
	}
	events := make([]models.OperationEvent, 0, sourceApprovalEventPageSize)
	for offset := 0; offset < sourceApprovalEventHistoryLimit; offset += sourceApprovalEventPageSize {
		page, err := repository.ListEventsPage(operationID, offset, sourceApprovalEventPageSize)
		if err != nil {
			return nil, err
		}
		if len(page) > sourceApprovalEventPageSize {
			return nil, ErrSourceApprovalHistory
		}
		events = append(events, page...)
		if len(page) < sourceApprovalEventPageSize {
			return events, nil
		}
	}
	// Refuse rather than make a decision from an incomplete audit history.
	return nil, ErrSourceApprovalHistory
}

func (s *Service) findSourceApproval(op models.Operation) (SourceApprovalReceipt, error) {
	if s == nil || s.repo == nil || !IsSourceDerived(op) || op.ID == uuid.Nil {
		return SourceApprovalReceipt{}, ErrSourceApprovalRequired
	}
	events, err := s.listSourceApprovalEvents(op.ID)
	if err != nil {
		return SourceApprovalReceipt{}, err
	}
	var found *SourceApprovalReceipt
	for _, event := range events {
		var payload sourceApprovalEventPayload
		if event.EventType != "status_change" || json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.SourceApproval == nil {
			continue
		}
		receipt := payload.SourceApproval
		if receipt.Schema != sourceApprovalReceiptSchema || receipt.ID == uuid.Nil || receipt.OperationID != op.ID ||
			receipt.OwnerUserID != op.OwnerUserID || receipt.WorkspaceID != op.WorkspaceID || strings.TrimSpace(receipt.ApprovedBy) == "" ||
			(receipt.ReviewedOwnerType != "" && !OwnerType(receipt.ReviewedOwnerType).IsValid()) ||
			receipt.Version <= 0 || !validSHA256(receipt.RevisionDigest) || receipt.ApprovedAt.IsZero() || receipt.ReviewedUpdatedAt.IsZero() ||
			event.ActorID != receipt.ApprovedBy || event.ActorType != string(OwnerRobert) ||
			event.BeforeStatus != string(StatusAwaitingApproval) || event.AfterStatus != string(StatusApproved) ||
			!event.CreatedAt.Equal(receipt.ApprovedAt) {
			return SourceApprovalReceipt{}, ErrSourceApprovalStale
		}
		if found != nil {
			return SourceApprovalReceipt{}, ErrSourceApprovalStale
		}
		copy := *receipt
		found = &copy
	}
	if found == nil {
		return SourceApprovalReceipt{}, ErrSourceApprovalRequired
	}
	return *found, nil
}

func sourceApprovalConsumed(events []models.OperationEvent, receipt SourceApprovalReceipt) bool {
	for _, event := range events {
		var payload sourceApprovalEventPayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload.ConsumedReceipt == receipt.ID.String() {
			return true
		}
	}
	return false
}

func sourceApprovalConsumption(events []models.OperationEvent, receipt SourceApprovalReceipt) (sourceApprovalEventPayload, bool) {
	var found *sourceApprovalEventPayload
	for _, event := range events {
		var payload sourceApprovalEventPayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.ConsumedReceipt != receipt.ID.String() {
			continue
		}
		if event.EventType != "status_change" || event.BeforeStatus != string(StatusApproved) || event.AfterStatus != string(StatusRunning) ||
			payload.RevisionDigest != receipt.RevisionDigest || !validSHA256(payload.ExecutionRevisionDigest) ||
			event.ActorType != string(OwnerHAI) || strings.TrimSpace(event.ActorID) == "" || found != nil {
			return sourceApprovalEventPayload{}, false
		}
		copy := payload
		found = &copy
	}
	if found == nil {
		return sourceApprovalEventPayload{}, false
	}
	return *found, true
}

func sourceRevisionMatches(op models.Operation, receipt SourceApprovalReceipt) bool {
	if op.Version != receipt.Version+1 || receipt.ReviewedUpdatedAt.IsZero() || !op.UpdatedAt.Equal(receipt.ApprovedAt) {
		return false
	}
	revision := op
	revision.Version = receipt.Version
	revision.Status = string(StatusAwaitingApproval)
	revision.UpdatedAt = receipt.ReviewedUpdatedAt
	owners := []OwnerType{OwnerHAI, OwnerRobert, OwnerVA, OwnerExternal, OwnerRuntime}
	if receipt.ReviewedOwnerType != "" {
		owners = []OwnerType{OwnerType(receipt.ReviewedOwnerType)}
	}
	for _, owner := range owners {
		revision.OwnerType = string(owner)
		digest, err := sourceOperationRevisionDigest(revision)
		if err == nil && digest == receipt.RevisionDigest {
			return true
		}
	}
	return false
}

func sourceExecutionPreparationMatches(approved, prepared models.Operation) bool {
	if prepared.ID != approved.ID || prepared.OwnerUserID != approved.OwnerUserID || prepared.WorkspaceID != approved.WorkspaceID ||
		prepared.Status != string(StatusApproved) || prepared.Version != approved.Version ||
		prepared.RuntimeID != sourceApprovalSafeRuntimeID || prepared.VerificationStatus != string(VerificationPending) {
		return false
	}
	approvedState, approvedErr := sourceWorldModelWithoutExecutionEvidence(approved.WorldModelStateJSON)
	preparedState, preparedErr := sourceWorldModelWithoutExecutionEvidence(prepared.WorldModelStateJSON)
	if approvedErr != nil || preparedErr != nil || approvedState != preparedState {
		return false
	}
	normalized := prepared
	normalized.RuntimeID = approved.RuntimeID
	normalized.VerificationStatus = approved.VerificationStatus
	normalized.WorldModelStateJSON = approved.WorldModelStateJSON
	return sourceOperationRevisionDigestMatches(normalized, approved)
}

func sourceWorldModelWithoutExecutionEvidence(raw string) (string, error) {
	state := make(map[string]json.RawMessage)
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil || state == nil {
			return "", ErrSourceApprovalStale
		}
	}
	delete(state, "safeWorkerExecution")
	encoded, err := json.Marshal(state)
	return string(encoded), err
}

func sourceExecutionRuntimeValid(op models.Operation) bool {
	return op.RuntimeID == sourceApprovalSafeRuntimeID && op.VerificationStatus == string(VerificationPending)
}

func sourceOperationRevisionDigestMatches(left, right models.Operation) bool {
	leftDigest, leftErr := sourceOperationRevisionDigest(left)
	rightDigest, rightErr := sourceOperationRevisionDigest(right)
	return leftErr == nil && rightErr == nil && leftDigest == rightDigest
}

func sourceOperationRevisionDigest(op models.Operation) (string, error) {
	// Explicitly bind every operation input, policy field, execution target,
	// timestamp, and prior context. Status is separately fenced by the state
	// machine and receipt transition; all other persisted fields are included.
	type sourceRevision struct {
		ID, OwnerUserID, WorkspaceID, Title, Description                      string
		SourceType, SourceID, SourceURI, SourceReceivedAt, SourceRevisionHash string
		SourceProvider, SourceAccount, SourceExternalID, SourceIdentityHash   string
		SourceObservationID                                                   string
		SourceObservationGeneration                                           int64
		ProjectKey, PursuitID, WorkflowID, AccountFeedID                      string
		OperationType, RiskLevel, AutonomyLevel, OwnerType, CurrentDecision   string
		RequiresApproval                                                      bool
		ApprovalID, RecommendedAction, EvidenceJSON, WorldModelStateJSON      string
		RuntimeID, ModelProviderID, ModelID, VerificationStatus               string
		ResultSummary, LastError, DedupeKey                                   string
		CreatedAt, UpdatedAt, CompletedAt, NextReviewAt                       string
		Version                                                               int64
	}
	text := func(value *uuid.UUID) string {
		if value == nil {
			return ""
		}
		return value.String()
	}
	timestamp := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	value := sourceRevision{
		ID: op.ID.String(), OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID,
		Title: op.Title, Description: op.Description, SourceType: op.SourceType, SourceID: text(op.SourceID),
		SourceURI: op.SourceURI, SourceReceivedAt: timestamp(op.SourceReceivedAt), SourceRevisionHash: op.SourceRevisionHash,
		SourceProvider: op.SourceProvider, SourceAccount: op.SourceAccount, SourceExternalID: op.SourceExternalID,
		SourceIdentityHash: op.SourceIdentityHash, SourceObservationID: text(op.SourceObservationID),
		SourceObservationGeneration: op.SourceObservationGeneration, ProjectKey: op.ProjectKey,
		PursuitID: text(op.PursuitID), WorkflowID: text(op.WorkflowID), AccountFeedID: text(op.AccountFeedID),
		OperationType: op.OperationType, RiskLevel: op.RiskLevel, AutonomyLevel: op.AutonomyLevel,
		OwnerType: op.OwnerType, CurrentDecision: op.CurrentDecision, RequiresApproval: op.RequiresApproval,
		ApprovalID: text(op.ApprovalID), RecommendedAction: op.RecommendedAction, EvidenceJSON: op.EvidenceJSON,
		WorldModelStateJSON: op.WorldModelStateJSON, RuntimeID: op.RuntimeID, ModelProviderID: op.ModelProviderID,
		ModelID: op.ModelID, VerificationStatus: op.VerificationStatus, ResultSummary: op.ResultSummary,
		LastError: op.LastError, DedupeKey: op.DedupeKey, CreatedAt: op.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: op.UpdatedAt.UTC().Format(time.RFC3339Nano), CompletedAt: timestamp(op.CompletedAt),
		NextReviewAt: timestamp(op.NextReviewAt), Version: op.Version,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode source operation revision: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func sourceLedgerTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
