package workflow

import (
	"context"
	"errors"
	"strings"
)

var ErrReminderDeliveryContextUnavailable = errors.New("context-aware atomic reminder delivery is unavailable")

// Owner-scoped HTTP capability, distinct from trusted system-wide scheduling.
type ContextualReminderDeliveryService interface {
	RunDueReminderDeliveriesForOwnerContext(context.Context, string, RunDueRequest) (*ReminderDeliveryRunSummary, error)
}

type ContextualReminderDeliveryBatchService interface {
	RunDueReminderDeliveriesContext(context.Context, RunDueRequest) (*ReminderDeliveryRunSummary, error)
}

type contextualReminderDeliveryRepository interface {
	withReminderDeliveryContext(context.Context) (Repository, error)
}

func (r *GormRepository) withReminderDeliveryContext(ctx context.Context) (Repository, error) {
	if ctx == nil {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := reminderDeliveryRootDB(r); err != nil {
		return nil, errors.Join(ErrReminderDeliveryContextUnavailable, err)
	}
	return &GormRepository{DB: r.DB.WithContext(ctx)}, nil
}

func (s *service) RunDueReminderDeliveriesForOwnerContext(ctx context.Context, owner string, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("authenticated reminder owner is required")
	}
	return s.runContextualReminderDelivery(ctx, owner, request)
}

func (s *service) RunDueReminderDeliveriesContext(ctx context.Context, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	return s.runContextualReminderDelivery(ctx, "", request)
}

func (s *service) runContextualReminderDelivery(ctx context.Context, owner string, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	if ctx == nil || s == nil {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repository, ok := s.repo.(contextualReminderDeliveryRepository)
	if !ok {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	scoped, err := repository.withReminderDeliveryContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := scoped.(transactionalReminderDeliveryRepository); !ok {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if _, ok := scoped.(reminderDeliveryExecutionRepository); !ok {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	// This view carries only reminder dependencies, never copying the service's
	// active-task sync.Map or mutating a shared repository's context.
	engine := &service{repo: scoped, reminderDeliverySink: s.reminderDeliverySink}
	return engine.runDueReminderDeliveriesContext(ctx, owner, request)
}
