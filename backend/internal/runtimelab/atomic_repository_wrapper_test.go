package runtimelab

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func (r *labOutcomeFailureRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := r.Repository.(operations.AtomicCreationRepository)
	if !ok {
		return nil, operations.ErrAtomicCreationUnsupported
	}
	return atomic.CreateWithEvent(op, event)
}

func (r *labOutcomeFailureRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	atomic, ok := r.Repository.(operations.AtomicMutationRepository)
	if !ok {
		return nil, operations.ErrAtomicMutationsUnsupported
	}
	return atomic.UpdateWithEvent(op, event)
}
