package automation

import (
	"context"
	"errors"

	"automation-hub-backend/internal/models"
)

var ErrLaunchStorageContextUnavailable = errors.New("owned launch admission storage is unavailable")
var ErrLaunchIntentStorageUnconfirmed = errors.New("launch intent storage requires reconciliation")
var ErrLaunchOutcomeStorageUnconfirmed = errors.New("launch outcome storage requires reconciliation")

func (s *service) launchOutcomeRepository(ctx context.Context, owned bool) (Repository, context.Context, context.CancelFunc, error) {
	if owned && ctx != nil {
		ctx = context.WithoutCancel(ctx)
	}
	return s.launchAdmissionRepository(ctx, owned)
}

func (s *service) launchAdmissionRepository(ctx context.Context, owned bool) (Repository, context.Context, context.CancelFunc, error) {
	if !owned {
		return s.repo, context.Background(), func() {}, nil
	}
	if ctx == nil {
		return nil, nil, func() {}, ErrLaunchStorageContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, approvalRegistrationTimeout)
	if err := ctx.Err(); err != nil {
		cancel()
		return nil, nil, func() {}, err
	}
	factory, ok := s.repo.(ContextualAutomationRepository)
	if !ok {
		cancel()
		return nil, nil, func() {}, ErrLaunchStorageContextUnavailable
	}
	repo, err := factory.WithAutomationRepositoryContext(ctx)
	if err != nil || repo == nil {
		cancel()
		return nil, nil, func() {}, errors.Join(ErrLaunchStorageContextUnavailable, err)
	}
	return repo, ctx, cancel, nil
}

func unconfirmedLaunchIntent(configuration *models.Automation, intent *models.AutomationLaunchEvent) *LaunchResult {
	return &LaunchResult{AutomationID: configuration.ID, LaunchEventID: intent.ID, LaunchType: configuration.LaunchType, RuntimeType: configuration.RuntimeType, Status: "indeterminate", ExitCode: -1, Message: "intent storage acknowledgement is uncertain; no execution was dispatched; reconcile this candidate before another attempt", AuditEvents: []string{"candidate intent identity retained; persistence is not confirmed", "execution stopped before dispatch"}, LaunchedAt: intent.StartedAt}
}
