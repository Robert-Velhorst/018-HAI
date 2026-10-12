package background

import (
	"context"
	"errors"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

var (
	_ operations.ContextIntakeRepository = (*outcomeTransitionFailureRepository)(nil)
	_ operations.ContextIntakeRepository = (*heartbeatLossRepository)(nil)
	_ operations.ContextIntakeRepository = (*failClaimRepository)(nil)
	_ operations.ContextIntakeRepository = (*failedDeferralRepository)(nil)
	_ operations.ContextIntakeRepository = (*failStatusUpdateOnceRepository)(nil)
	_ operations.ContextIntakeRepository = (*cancelOnStatusRepository)(nil)
)

func findByDedupeKeyContext(ctx context.Context, repo operations.Repository, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	contextual, ok := repo.(operations.ContextIntakeRepository)
	if !ok {
		return nil, false, operations.ErrContextIntakeUnsupported
	}
	return contextual.FindByDedupeKeyContext(ctx, ownerUserID, workspaceID, dedupeKey)
}

func createWithAuditContext(ctx context.Context, repo operations.Repository, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	contextual, ok := repo.(operations.ContextIntakeRepository)
	if !ok {
		return nil, operations.ErrContextIntakeUnsupported
	}
	return contextual.CreateWithEventContext(ctx, op, event)
}

func updateWithAuditContext(ctx context.Context, repo operations.Repository, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	contextual, ok := repo.(operations.ContextIntakeRepository)
	if !ok {
		return nil, operations.ErrContextIntakeUnsupported
	}
	return contextual.UpdateWithEventContext(ctx, op, event)
}

func (r *outcomeTransitionFailureRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *outcomeTransitionFailureRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *outcomeTransitionFailureRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAuditContext(ctx, r.Repository, op, event)
}

func (r *heartbeatLossRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *heartbeatLossRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *heartbeatLossRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failClaimRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *failClaimRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failClaimRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failedDeferralRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *failedDeferralRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failedDeferralRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.NextReviewAt != nil && r.saveErr != nil {
		return nil, r.saveErr
	}
	return updateWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failStatusUpdateOnceRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *failStatusUpdateOnceRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *failStatusUpdateOnceRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.Status == r.status && !r.failed {
		r.failed = true
		return nil, errors.New("simulated transient update failure")
	}
	return updateWithAuditContext(ctx, r.Repository, op, event)
}

func (r *cancelOnStatusRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return findByDedupeKeyContext(ctx, r.Repository, ownerUserID, workspaceID, dedupeKey)
}

func (r *cancelOnStatusRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAuditContext(ctx, r.Repository, op, event)
}

func (r *cancelOnStatusRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	updated, err := updateWithAuditContext(ctx, r.Repository, op, event)
	if err == nil && op.Status == r.status {
		r.cancel()
	}
	return updated, err
}

func createWithAudit(repo operations.Repository, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := repo.(operations.AtomicCreationRepository)
	if !ok {
		return nil, operations.ErrAtomicCreationUnsupported
	}
	return atomic.CreateWithEvent(op, event)
}

func (r *outcomeTransitionFailureRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func (r *heartbeatLossRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func (r *failClaimRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func (r *failedDeferralRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func (r *failStatusUpdateOnceRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func (r *cancelOnStatusRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return createWithAudit(r.Repository, op, event)
}

func updateWithAudit(repo operations.Repository, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := repo.(operations.AtomicMutationRepository)
	if !ok {
		return nil, operations.ErrAtomicMutationsUnsupported
	}
	return atomic.UpdateWithEvent(op, event)
}

func (r *outcomeTransitionFailureRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAudit(r.Repository, op, event)
}

func (r *heartbeatLossRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAudit(r.Repository, op, event)
}

func (r *failClaimRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return updateWithAudit(r.Repository, op, event)
}

func (r *failedDeferralRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if op.NextReviewAt != nil && r.saveErr != nil {
		return nil, r.saveErr
	}
	return updateWithAudit(r.Repository, op, event)
}

func (r *failStatusUpdateOnceRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if op.Status == r.status && !r.failed {
		r.failed = true
		return nil, errors.New("simulated transient update failure")
	}
	return updateWithAudit(r.Repository, op, event)
}

func (r *cancelOnStatusRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	updated, err := updateWithAudit(r.Repository, op, event)
	if err == nil && op.Status == r.status {
		r.cancel()
	}
	return updated, err
}
