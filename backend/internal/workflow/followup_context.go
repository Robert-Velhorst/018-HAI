package workflow

import (
	"context"
	"errors"
	"strings"
)

var ErrFollowUpContextUnavailable = errors.New("context-aware transactional follow-up is unavailable")

// Separate from Service: legacy adapters must not silently gain a contextual
// capability through an embedded interface with an unimplemented method.
type ContextualFollowUpService interface {
	RunDueOpenLoopsForOwnerContext(context.Context, string, RunDueRequest) (*OpenLoopRunSummary, error)
}

// Trusted background capability; never substitute this for authenticated,
// owner-scoped HTTP or Temporal operations.
type ContextualFollowUpBatchService interface {
	RunDueOpenLoopsContext(context.Context, RunDueRequest) (*OpenLoopRunSummary, error)
}

type contextualFollowUpRepository interface {
	withFollowUpContext(context.Context) (Repository, error)
}

func (r *GormRepository) withFollowUpContext(ctx context.Context) (Repository, error) {
	if ctx == nil {
		return nil, ErrFollowUpContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := followUpRootDB(r); err != nil {
		return nil, err
	}
	// A detached GORM session scopes reads, claims, and the existing transaction
	// together without mutating another worker's shared repository.
	return &GormRepository{DB: r.DB.WithContext(ctx)}, nil
}

func (s *service) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	if ctx == nil {
		return nil, ErrFollowUpContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("an authenticated owner is required for contextual follow-up")
	}
	return s.runContextualFollowUp(ctx, owner, request)
}

func (s *service) RunDueOpenLoopsContext(ctx context.Context, request RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.runContextualFollowUp(ctx, "", request)
}

func (s *service) runContextualFollowUp(ctx context.Context, owner string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	if ctx == nil {
		return nil, ErrFollowUpContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrFollowUpContextUnavailable
	}
	repository, ok := s.repo.(contextualFollowUpRepository)
	if !ok {
		return nil, ErrFollowUpContextUnavailable
	}
	scoped, err := repository.withFollowUpContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := scoped.(transactionalFollowUpRepository); !ok {
		return nil, ErrFollowUpContextUnavailable
	}
	// Do not copy service: it owns a sync.Map for active task executions. This
	// view uses only follow-up dependencies and the same durable transaction.
	engine := &service{repo: scoped, lifeOntologyProjector: s.lifeOntologyProjector}
	return engine.runDueOpenLoopsForOwnerContext(ctx, owner, request)
}
