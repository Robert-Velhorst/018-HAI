package executionauth

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryRepository struct {
	mu             sync.RWMutex
	receipts       map[string]Receipt
	byID           map[string]string
	consumptions   map[string]Consumption
	approvalClaims map[string]uuid.UUID
	exercises      map[string]FinalEffectExercise
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		receipts:       map[string]Receipt{},
		byID:           map[string]string{},
		consumptions:   map[string]Consumption{},
		approvalClaims: map[string]uuid.UUID{},
		exercises:      map[string]FinalEffectExercise{},
	}
}

func (r *MemoryRepository) ExerciseFinalEffect(
	ctx context.Context,
	value FinalEffectExercise,
) error {
	if err := memoryContextError(ctx); err != nil {
		return err
	}
	if err := validateFinalEffectExercise(value); err != nil {
		return err
	}
	if err := r.lockContext(ctx, true); err != nil {
		return err
	}
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	key := ownerKey(value.OwnerIdentity, value.ReceiptID.String())
	receiptKey, ok := r.byID[key]
	if !ok {
		return ErrNotFound
	}
	receipt := r.receipts[receiptKey]
	consumption, consumed := r.consumptions[key]
	if !consumed {
		return ErrNotAuthorized
	}
	if _, exists := r.exercises[key]; exists {
		return ErrAlreadyExercised
	}
	if !finalEffectMatches(receipt, consumption, value) {
		return ErrFinalEffectMismatch
	}
	if !finalEffectAuthorityFresh(receipt, consumption, value.ExercisedAt) {
		return ErrFinalEffectExpired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.exercises[key] = value
	return nil
}

func (r *MemoryRepository) GetFinalEffectExercise(
	ctx context.Context,
	owner string,
	receiptID uuid.UUID,
) (FinalEffectExercise, error) {
	if err := r.lockContext(ctx, false); err != nil {
		return FinalEffectExercise{}, err
	}
	defer r.mu.RUnlock()
	value, ok := r.exercises[ownerKey(owner, receiptID.String())]
	if !ok {
		return FinalEffectExercise{}, ErrNotFound
	}
	return value, nil
}

func (r *MemoryRepository) CreateOrGet(ctx context.Context, receipt Receipt) (Receipt, bool, error) {
	if err := memoryContextError(ctx); err != nil {
		return Receipt{}, false, err
	}
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, false, err
	}
	if err := r.lockContext(ctx, true); err != nil {
		return Receipt{}, false, err
	}
	defer r.mu.Unlock()
	key := ownerKey(receipt.OwnerIdentity, receipt.IdempotencyKey)
	if existing, ok := r.receipts[key]; ok {
		if existing.RequestDigest != receipt.RequestDigest {
			return Receipt{}, false, ErrIdempotencyConflict
		}
		return cloneReceipt(existing), false, nil
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, false, err
	}
	r.receipts[key] = cloneReceipt(receipt)
	r.byID[ownerKey(receipt.OwnerIdentity, receipt.ID.String())] = key
	return cloneReceipt(receipt), true, nil
}

func (r *MemoryRepository) Get(ctx context.Context, owner string, id uuid.UUID) (Receipt, error) {
	if err := r.lockContext(ctx, false); err != nil {
		return Receipt{}, err
	}
	defer r.mu.RUnlock()
	key, ok := r.byID[ownerKey(owner, id.String())]
	if !ok {
		return Receipt{}, ErrNotFound
	}
	return cloneReceipt(r.receipts[key]), nil
}

func (r *MemoryRepository) List(ctx context.Context, owner string, limit int) ([]Receipt, error) {
	if err := r.lockContext(ctx, false); err != nil {
		return nil, err
	}
	defer r.mu.RUnlock()
	result := make([]Receipt, 0)
	for _, receipt := range r.receipts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if receipt.OwnerIdentity == owner {
			result = append(result, cloneReceipt(receipt))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].EvaluatedAt.Equal(result[j].EvaluatedAt) {
			return result[i].ID.String() > result[j].ID.String()
		}
		return result[i].EvaluatedAt.After(result[j].EvaluatedAt)
	})
	limit = boundedLimit(limit)
	if len(result) > limit {
		result = result[:limit]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MemoryRepository) Consume(ctx context.Context, value Consumption) error {
	if err := memoryContextError(ctx); err != nil {
		return err
	}
	if err := validateConsumption(value); err != nil {
		return err
	}
	if err := r.lockContext(ctx, true); err != nil {
		return err
	}
	defer r.mu.Unlock()
	key := ownerKey(value.OwnerIdentity, value.ReceiptID.String())
	receiptKey, ok := r.byID[key]
	if !ok {
		return ErrNotFound
	}
	receipt := r.receipts[receiptKey]
	if receipt.Outcome != OutcomeAuthorized || receipt.DecisionDigest != value.ReceiptDigest {
		return ErrNotAuthorized
	}
	approvalKey := ""
	if receipt.ApprovalSourceID != "" {
		approval := receipt.Evidence.Approval
		if approval.SourceID != receipt.ApprovalSourceID || approval.DecisionID == "" ||
			approval.ExpiresAt.IsZero() || !value.ConsumedAt.Before(approval.ExpiresAt) {
			return ErrAuthorizationChanged
		}
		approvalKey = approvalClaimKey(receipt.OwnerIdentity, approval.SourceID, approval.DecisionID)
		if claimedReceipt, exists := r.approvalClaims[approvalKey]; exists && claimedReceipt != receipt.ID {
			return ErrApprovalAlreadyClaimed
		}
	}
	if _, exists := r.consumptions[key]; exists {
		return ErrAlreadyConsumed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if approvalKey != "" {
		r.approvalClaims[approvalKey] = receipt.ID
	}
	r.consumptions[key] = value
	return nil
}

func (r *MemoryRepository) GetConsumption(
	ctx context.Context,
	owner string,
	receiptID uuid.UUID,
) (Consumption, error) {
	if err := r.lockContext(ctx, false); err != nil {
		return Consumption{}, err
	}
	defer r.mu.RUnlock()
	value, ok := r.consumptions[ownerKey(owner, receiptID.String())]
	if !ok {
		return Consumption{}, ErrNotFound
	}
	return value, nil
}

func ownerKey(owner, id string) string { return owner + "\x00" + id }

func memoryContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("execution authorization: context is required")
	}
	return ctx.Err()
}

// Authorization waits must not outlive the operation's effect authority. No
// helper goroutine is left holding a lock after the caller has canceled.
func (r *MemoryRepository) lockContext(ctx context.Context, write bool) error {
	if err := memoryContextError(ctx); err != nil {
		return err
	}
	tryLock, unlock := r.mu.TryRLock, r.mu.RUnlock
	if write {
		tryLock, unlock = r.mu.TryLock, r.mu.Unlock
	}
	if tryLock() {
		if err := ctx.Err(); err != nil {
			unlock()
			return err
		}
		return nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if tryLock() {
				if err := ctx.Err(); err != nil {
					unlock()
					return err
				}
				return nil
			}
		}
	}
}

func approvalClaimKey(owner, sourceID, decisionID string) string {
	return ownerKey(owner, sourceID+"\x00"+decisionID)
}

func boundedLimit(value int) int {
	if value <= 0 {
		return 50
	}
	if value > 200 {
		return 200
	}
	return value
}

func cloneReceipt(value Receipt) Receipt {
	value.Evidence.Constitution.RequestedCapabilities = append(
		[]string(nil),
		value.Evidence.Constitution.RequestedCapabilities...,
	)
	value.Evidence.Constitution.DeniedCapabilities = append(
		[]string(nil),
		value.Evidence.Constitution.DeniedCapabilities...,
	)
	value.Evidence.Constitution.ApprovalRequiredCapabilities = append(
		[]string(nil),
		value.Evidence.Constitution.ApprovalRequiredCapabilities...,
	)
	if value.Evidence.Governance.FrameworkMaximumAutonomyLevel != nil {
		maximumAutonomy := *value.Evidence.Governance.FrameworkMaximumAutonomyLevel
		value.Evidence.Governance.FrameworkMaximumAutonomyLevel = &maximumAutonomy
	}
	if value.Evidence.Governance.FrameworkRequiresApproval != nil {
		requiresApproval := *value.Evidence.Governance.FrameworkRequiresApproval
		value.Evidence.Governance.FrameworkRequiresApproval = &requiresApproval
	}
	value.Evidence.Governance.EvidenceReferences = append(
		[]string(nil),
		value.Evidence.Governance.EvidenceReferences...,
	)
	value.Evidence.ReasonCodes = append([]string(nil), value.Evidence.ReasonCodes...)
	value.Evidence.Trace = append([]string(nil), value.Evidence.Trace...)
	return value
}
