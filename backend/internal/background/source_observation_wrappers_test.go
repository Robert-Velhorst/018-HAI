package background

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"context"
)

func forwardSourceHeadPublication(repo operations.Repository, ctx context.Context, op models.Operation) error {
	publisher, ok := repo.(operations.SourceHeadRepository)
	if !ok {
		return operations.ErrSourceHeadUnsupported
	}
	return publisher.PublishSourceHead(ctx, op)
}

func (r *outcomeTransitionFailureRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}
func (r *heartbeatLossRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}
func (r *failClaimRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}
func (r *failedDeferralRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}
func (r *failStatusUpdateOnceRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}
func (r *cancelOnStatusRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	return forwardSourceHeadPublication(r.Repository, ctx, op)
}

// Fault adapters preserve the real store's observation boundary so existing
// cancellation/claim tests still reach the fault they are intended to exercise.
func beginTestSourceObservation(ctx context.Context, repo operations.Repository, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	observations, ok := repo.(operations.SourceObservationRepository)
	if !ok {
		return operations.SourceObservation{}, operations.ErrSourceObservationUnsupported
	}
	return observations.BeginSourceObservation(ctx, start)
}

func (r *outcomeTransitionFailureRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
func (r *heartbeatLossRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
func (r *failClaimRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
func (r *failedDeferralRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
func (r *failStatusUpdateOnceRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
func (r *cancelOnStatusRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	return beginTestSourceObservation(ctx, r.Repository, start)
}
