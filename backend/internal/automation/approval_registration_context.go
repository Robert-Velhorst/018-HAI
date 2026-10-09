package automation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const approvalRegistrationTimeout = 30 * time.Second

var (
	ErrAutomationRepositoryContextUnavailable = errors.New("owned automation repository context is unavailable")
	ErrApprovalRegistrationContextUnavailable = errors.New("owned approval registration is unavailable")
	ErrApprovalRegistrationUnconfirmed        = errors.New("approval registration acknowledgement requires reconciliation")
)

type ContextualAutomationRepository interface {
	WithAutomationRepositoryContext(context.Context) (Repository, error)
}

type ContextualApprovalDecisionRecorder interface {
	RecordApprovalDecisionContext(context.Context, uuid.UUID, TaskApprovalDecisionRequest) error
}

func (s *service) RecordApprovalDecisionContext(ctx context.Context, id uuid.UUID, request TaskApprovalDecisionRequest) error {
	if ctx == nil {
		return ErrApprovalRegistrationContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, approvalRegistrationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	factory, ok := s.repo.(ContextualAutomationRepository)
	if !ok {
		return ErrApprovalRegistrationContextUnavailable
	}
	repo, err := factory.WithAutomationRepositoryContext(ctx)
	if err != nil || repo == nil {
		return errors.Join(ErrApprovalRegistrationContextUnavailable, err)
	}
	return s.recordApprovalDecisionWithRepository(ctx, repo, id, request)
}
