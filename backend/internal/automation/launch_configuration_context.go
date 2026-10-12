package automation

import (
	"context"
	"errors"
	"fmt"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

var ErrLaunchConfigurationContextUnavailable = errors.New("owned launch configuration inspection is unavailable")

func (s *service) launchConfiguration(ctx context.Context, id uuid.UUID, owned bool) (*models.Automation, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("automation repository is unavailable")
	}
	var item *models.Automation
	var err error
	if owned {
		if ctx == nil {
			return nil, ErrLaunchConfigurationContextUnavailable
		}
		ctx, cancel := context.WithTimeout(ctx, reviewConfigurationInspectionTimeout)
		defer cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reader, ok := s.repo.(ContextualConfigurationRepository)
		if !ok {
			return nil, ErrLaunchConfigurationContextUnavailable
		}
		item, err = reader.FindByIDContext(ctx, id)
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, errors.Join(err, contextErr)
		}
		if errors.Is(err, ErrReviewConfigurationContextUnavailable) || errors.Is(err, ErrAutomationRepositoryContextUnavailable) {
			return nil, errors.Join(ErrLaunchConfigurationContextUnavailable, err)
		}
	} else {
		item, err = s.repo.FindByID(id)
	}
	if err != nil {
		return nil, err
	}
	if item == nil || id == uuid.Nil || item.ID != id {
		return nil, fmt.Errorf("automation launch configuration target does not match")
	}
	return item, nil
}
