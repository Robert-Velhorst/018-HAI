package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type memoryExecutionClaim struct {
	owner      uuid.UUID
	generation int64
	expiresAt  time.Time
}

func (r *MemoryRepository) ClaimNext(ctx context.Context, ownerUserID, workspaceID string, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	if _, err := claimLeaseMicros(lease); err != nil {
		return nil, err
	}
	if ownerUserID == "" || workspaceID == "" || workerID == uuid.Nil {
		return nil, errors.New("operations: owner, workspace, and worker identity are required for a claim")
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var candidates []models.Operation
	for _, op := range r.ops {
		if err := intakeContextError(ctx); err != nil {
			return nil, err
		}
		if op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID || !memoryClaimable(op, now, r.events) {
			continue
		}
		if op.SourceIdentityHash != "" && r.claimSourceHeadError(op) != nil {
			continue
		}
		if claim, ok := r.claims[op.ID]; ok && claim.owner != uuid.Nil && claim.expiresAt.After(now) {
			continue
		}
		candidates = append(candidates, op)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		if !candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].UpdatedAt.Before(candidates[j].UpdatedAt)
		}
		return candidates[i].ID.String() < candidates[j].ID.String()
	})
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	op := candidates[0]
	claim := r.claims[op.ID]
	claim.owner = workerID
	claim.generation++
	claim.expiresAt = now.Add(lease)
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	r.claims[op.ID] = claim
	return &ClaimedOperation{Operation: cloneOperation(op), Claim: ExecutionClaim{
		OperationID: op.ID,
		OwnerUserID: ownerUserID,
		WorkspaceID: workspaceID,
		Owner:       workerID,
		Generation:  claim.generation,
	}}, nil
}

func (r *MemoryRepository) ClaimOperation(ctx context.Context, ownerUserID, workspaceID string, operationID, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	if _, err := claimLeaseMicros(lease); err != nil {
		return nil, err
	}
	if ownerUserID == "" || workspaceID == "" || operationID == uuid.Nil || workerID == uuid.Nil {
		return nil, errors.New("operations: owner, workspace, operation, and worker identity are required for a claim")
	}

	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	op, ok := r.ops[operationID]
	if !ok || op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	if !safeOperationClaimable(op, now) {
		return nil, ErrOperationNotClaimable
	}
	if OperationStatus(op.Status) == StatusApproved && !hasSourceApprovalReceipt(r.events, op.ID) {
		return nil, ErrOperationNotClaimable
	}
	if err := r.claimSourceHeadError(op); err != nil {
		return nil, err
	}
	claim := r.claims[operationID]
	if claim.owner != uuid.Nil && claim.expiresAt.After(now) {
		return nil, ErrOperationClaimed
	}
	claim.owner = workerID
	claim.generation++
	claim.expiresAt = now.Add(lease)
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	r.claims[operationID] = claim

	return &ClaimedOperation{Operation: cloneOperation(op), Claim: ExecutionClaim{
		OperationID: operationID,
		OwnerUserID: ownerUserID,
		WorkspaceID: workspaceID,
		Owner:       workerID,
		Generation:  claim.generation,
	}}, nil
}

func (r *MemoryRepository) RenewClaim(ctx context.Context, claim ExecutionClaim, lease time.Duration) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return ErrAtomicClaimsUnsupported
	}
	if _, err := claimLeaseMicros(lease); err != nil {
		return err
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	current, ok := r.claims[claim.OperationID]
	if !ok || !matchesMemoryClaim(current, claim) || !current.expiresAt.After(time.Now().UTC()) {
		return ErrClaimLost
	}
	current.expiresAt = time.Now().UTC().Add(lease)
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	r.claims[claim.OperationID] = current
	return nil
}

func (r *MemoryRepository) TransitionClaimed(ctx context.Context, claim ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	current, ok := r.ops[claim.OperationID]
	if !ok || current.OwnerUserID != claim.OwnerUserID || current.WorkspaceID != claim.WorkspaceID {
		return nil, ErrClaimLost
	}
	if op.ID != claim.OperationID || op.OwnerUserID != current.OwnerUserID || op.WorkspaceID != current.WorkspaceID {
		return nil, errors.New("operations: operation identity is immutable")
	}
	if current.Version != op.Version-1 {
		return nil, ErrStaleOperation
	}
	if err := validateMutationState(current, op, &event); err != nil {
		return nil, err
	}
	currentClaim, ok := r.claims[claim.OperationID]
	if !ok || !matchesMemoryClaim(currentClaim, claim) || !currentClaim.expiresAt.After(time.Now().UTC()) {
		return nil, ErrClaimLost
	}
	if event.ID != uuid.Nil {
		for _, existing := range r.events {
			if existing.ID == event.ID {
				return nil, errors.New("operations: duplicate mutation event ID")
			}
		}
	}
	op.CreatedAt = current.CreatedAt
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	r.ops[op.ID] = cloneOperation(op)
	event.OperationID = op.ID
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	r.events = append(r.events, event)
	if release {
		currentClaim.generation++
		currentClaim.owner = uuid.Nil
		currentClaim.expiresAt = time.Time{}
		r.claims[claim.OperationID] = currentClaim
	}
	copyOp := cloneOperation(op)
	return &copyOp, nil
}

func (r *MemoryRepository) ReleaseClaim(ctx context.Context, claim ExecutionClaim) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return ErrAtomicClaimsUnsupported
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	current, ok := r.claims[claim.OperationID]
	if !ok || !matchesMemoryClaim(current, claim) || !current.expiresAt.After(time.Now().UTC()) {
		return ErrClaimLost
	}
	current.generation++
	current.owner = uuid.Nil
	current.expiresAt = time.Time{}
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	r.claims[claim.OperationID] = current
	return nil
}

func (r *MemoryRepository) RecoverExpiredClaims(ctx context.Context, ownerUserID, workspaceID string, limit int) (RecoveryResult, error) {
	if err := intakeContextError(ctx); err != nil {
		return RecoveryResult{}, err
	}
	if r == nil {
		return RecoveryResult{}, ErrAtomicClaimsUnsupported
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return RecoveryResult{}, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return RecoveryResult{}, err
	}
	now := time.Now().UTC()
	result := RecoveryResult{}
	// Stage the batch so ended authority cannot leave a partial memory recovery.
	stagedOps := make(map[uuid.UUID]models.Operation)
	stagedClaims := make(map[uuid.UUID]memoryExecutionClaim)
	var stagedEvents []models.OperationEvent
	var candidates []models.Operation
	for _, op := range r.ops {
		if err := intakeContextError(ctx); err != nil {
			return RecoveryResult{}, err
		}
		if op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID {
			continue
		}
		switch OperationStatus(op.Status) {
		case StatusRunning:
			result.ScannedRunning++
		case StatusVerifying:
			result.ScannedVerifying++
		default:
			continue
		}
		candidates = append(candidates, op)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].UpdatedAt.Before(candidates[j].UpdatedAt)
		}
		return candidates[i].ID.String() < candidates[j].ID.String()
	})
	for _, op := range candidates {
		if err := intakeContextError(ctx); err != nil {
			return RecoveryResult{}, err
		}
		claim, ok := r.claims[op.ID]
		if !ok || claim.owner == uuid.Nil {
			if OperationStatus(op.Status) == StatusRunning {
				result.UnleasedRunning++
			} else {
				result.UnleasedVerifying++
			}
			continue
		}
		if claim.expiresAt.After(now) {
			if OperationStatus(op.Status) == StatusRunning {
				result.LiveRunning++
			} else {
				result.LiveVerifying++
			}
			continue
		}
		if result.Recovered >= limit {
			result.ExpiredClaimsRemain++
			continue
		}
		to := StatusInterrupted
		message := "recovered after expired worker lease: side effect outcome is uncertain; no automatic replay"
		if OperationStatus(op.Status) == StatusVerifying {
			to = StatusAwaitingApproval
			message = "recovered after expired worker lease: verification is incomplete; human confirmation required; no automatic replay"
		}
		updated, event, err := ApplyTransition(op, to, "recovery", "", message, now)
		if err != nil {
			return RecoveryResult{}, fmt.Errorf("build recovery transition for operation %s: %w", op.ID, err)
		}
		stagedOps[op.ID] = cloneOperation(updated)
		stagedEvents = append(stagedEvents, event)
		claim.generation++
		claim.owner = uuid.Nil
		claim.expiresAt = time.Time{}
		stagedClaims[op.ID] = claim
		result.Recovered++
		result.Details = append(result.Details, fmt.Sprintf("op %s: %s -> %s", op.ID, op.Status, to))
	}
	if err := intakeContextError(ctx); err != nil {
		return RecoveryResult{}, err
	}
	for id, op := range stagedOps {
		r.ops[id] = op
		r.claims[id] = stagedClaims[id]
	}
	r.events = append(r.events, stagedEvents...)
	return result, nil
}

func memoryClaimable(op models.Operation, now time.Time, events []models.OperationEvent) bool {
	if op.NextReviewAt != nil && op.NextReviewAt.After(now) {
		return false
	}
	switch OperationStatus(op.Status) {
	case StatusNew, StatusReady, StatusDrafting:
		return true
	case StatusApproved:
		return hasSourceApprovalReceipt(events, op.ID)
	case StatusClassified:
		decision := CurrentDecision(op.CurrentDecision)
		return decision == DecisionRunSafeLocalWorker || decision == DecisionCreateDraft || decision == DecisionBlock || op.RequiresApproval
	default:
		return false
	}
}

func hasSourceApprovalReceipt(events []models.OperationEvent, operationID uuid.UUID) bool {
	for _, event := range events {
		if event.OperationID != operationID || event.EventType != "status_change" || event.AfterStatus != string(StatusApproved) {
			continue
		}
		var payload sourceApprovalEventPayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload.SourceApproval != nil {
			return true
		}
	}
	return false
}

func matchesMemoryClaim(current memoryExecutionClaim, claim ExecutionClaim) bool {
	return current.owner == claim.Owner && current.generation == claim.Generation
}
