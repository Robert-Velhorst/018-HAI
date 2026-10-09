package phase2

import (
	"context"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func (r *phase2OutcomeFailureRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	fenced, ok := r.Repository.(operations.SafeEffectRepository)
	if !ok {
		return operations.ErrSafeEffectUnsupported
	}
	return fenced.WithClaimedSafeEffect(ctx, claim, op, effect)
}
