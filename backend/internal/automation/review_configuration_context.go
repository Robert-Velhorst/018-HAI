package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const reviewConfigurationInspectionTimeout = 30 * time.Second

type ContextualConfigurationRepository interface {
	FindByIDContext(context.Context, uuid.UUID) (*models.Automation, error)
}

func (r *GormUserRepository) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	if ctx == nil {
		return nil, ErrAutomationRepositoryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.Statement == nil || r.DB.Statement.ConnPool == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" || r.DB.DryRun {
		return nil, ErrAutomationRepositoryContextUnavailable
	}
	if _, borrowed := r.DB.Statement.ConnPool.(gorm.TxCommitter); borrowed {
		return nil, ErrAutomationRepositoryContextUnavailable
	}
	return &GormUserRepository{DB: r.DB.Session(&gorm.Session{NewDB: true, Context: ctx})}, nil
}

func (r *GormUserRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	if ctx == nil {
		return nil, ErrReviewConfigurationContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, reviewConfigurationInspectionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := r.WithAutomationRepositoryContext(ctx)
	if err != nil {
		return nil, errors.Join(ErrReviewConfigurationContextUnavailable, err)
	}
	item, err := repo.FindByID(id)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *service) InspectReviewConfigurationContext(ctx context.Context, id uuid.UUID) (*ReviewConfigurationSnapshot, error) {
	if ctx == nil {
		return nil, ErrReviewConfigurationContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, reviewConfigurationInspectionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(ContextualConfigurationRepository)
	if !ok {
		return nil, ErrReviewConfigurationContextUnavailable
	}
	stored, err := repo.FindByIDContext(ctx, id)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	snapshot, err := s.reviewConfigurationSnapshot(id, stored)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *service) reviewConfigurationSnapshot(id uuid.UUID, stored *models.Automation) (*ReviewConfigurationSnapshot, error) {
	if stored == nil || id == uuid.Nil || stored.ID != id {
		return nil, fmt.Errorf("automation configuration target does not match")
	}
	configuration := *stored
	s.applyAutomationDefaults(&configuration)
	scope, _ := approvalScopeForAutomation(&configuration)
	snapshot := &ReviewConfigurationSnapshot{
		Version: reviewConfigurationSnapshotVersion, AutomationID: id, Scope: scope,
		ConfigurationDigest: automationActionDigest(&configuration, TaskLaunchRequest{}),
	}
	if err := ValidateReviewConfigurationSnapshot(snapshot, id); err != nil {
		return nil, err
	}
	return snapshot, nil
}
