package background

import (
	"context"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func forwardSafeEffect(ctx context.Context, repository operations.Repository, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	fenced, ok := repository.(operations.SafeEffectRepository)
	if !ok {
		return operations.ErrSafeEffectUnsupported
	}
	return fenced.WithClaimedSafeEffect(ctx, claim, op, effect)
}

func (r *outcomeTransitionFailureRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}

func (r *cancelOnStatusRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}

func (r *failStatusUpdateOnceRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}

func (r *heartbeatLossRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}

func (r *failedDeferralRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}

func (r *failClaimRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	return forwardSafeEffect(ctx, r.Repository, claim, op, effect)
}
