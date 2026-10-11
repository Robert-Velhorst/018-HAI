package runtimelab

import (
	"context"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func (r *labOutcomeFailureRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
	repo, ok := r.Repository.(operations.SafeEffectRepository)
	if !ok {
		return operations.ErrSafeEffectUnsupported
	}
	return repo.WithClaimedSafeEffect(ctx, claim, expected, effect)
}
