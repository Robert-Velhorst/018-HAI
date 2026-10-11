package automation

import (
	"context"
	"errors"
	"fmt"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

var ErrApprovalRequirementContextUnavailable = errors.New("owned approval requirement inspection is unavailable")

type ContextualActionApprovalRequirementInspector interface {
	ActionApprovalRequiredContext(context.Context, uuid.UUID) (bool, error)
}

func (s *service) ActionApprovalRequiredContext(ctx context.Context, id uuid.UUID) (bool, error) {
	if ctx == nil {
		return false, ErrApprovalRequirementContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, reviewConfigurationInspectionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	repo, ok := s.repo.(ContextualConfigurationRepository)
	if !ok {
		return false, ErrApprovalRequirementContextUnavailable
	}
	stored, err := repo.FindByIDContext(ctx, id)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return false, err
	}
	required, err := s.actionApprovalRequiredForConfiguration(id, stored)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return false, err
	}
	return required, nil
}

func (s *service) actionApprovalRequiredForConfiguration(id uuid.UUID, stored *models.Automation) (bool, error) {
	if stored == nil || id == uuid.Nil || stored.ID != id {
		return false, fmt.Errorf("automation approval configuration target does not match")
	}
	configuration := *stored
	s.applyAutomationDefaults(&configuration)
	scope, required := approvalScopeForAutomation(&configuration)
	if scope == "" {
		return false, fmt.Errorf("automation action does not have a supported approval scope")
	}
	return required, nil
}
