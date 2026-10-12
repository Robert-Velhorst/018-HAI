package phase2

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func (r *phase2OutcomeFailureRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := r.Repository.(operations.AtomicCreationRepository)
	if !ok {
		return nil, operations.ErrAtomicCreationUnsupported
	}
	return atomic.CreateWithEvent(op, event)
}

func (r *phase2OutcomeFailureRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := r.Repository.(operations.AtomicMutationRepository)
	if !ok {
		return nil, operations.ErrAtomicMutationsUnsupported
	}
	return atomic.UpdateWithEvent(op, event)
}
