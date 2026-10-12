package workflow

import (
	"context"
	"errors"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
)

var (
	ErrClaimRecoveryContextUnavailable = errors.New("context-aware atomic claim recovery is unavailable")
	ErrClaimRecoveryOutcomeUnconfirmed = errors.New("claim recovery outcome is unconfirmed")
)

type ContextualClaimRecoveryService interface {
	RecoverStaleClaimsForOwnerContext(context.Context, string, RunDueRequest) (*ClaimRecoverySummary, error)
}

type ContextualClaimRecoveryBatchService interface {
	RecoverStaleClaimsContext(context.Context, RunDueRequest) (*ClaimRecoverySummary, error)
}

type contextualClaimRecoveryRepository interface {
	withClaimRecoveryContext(context.Context) (Repository, error)
}

type atomicClaimRecoveryRepository interface {
	RecoverExpiredWorkflowClaimAtomic(models.WorkflowItem, time.Time) (*models.WorkflowItem, bool, error)
	RecoverExpiredOpenLoopClaimAtomic(string, models.WorkflowOpenLoop, time.Time) (*models.WorkflowOpenLoop, bool, error)
}

func (r *GormRepository) withClaimRecoveryContext(ctx context.Context) (Repository, error) {
	if ctx == nil {
		return nil, ErrClaimRecoveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := claimRecoveryRootDB(r); err != nil {
		return nil, errors.Join(ErrClaimRecoveryContextUnavailable, err)
	}
	return &GormRepository{DB: r.DB.WithContext(ctx)}, nil
}

func (s *service) RecoverStaleClaimsForOwnerContext(ctx context.Context, owner string, request RunDueRequest) (*ClaimRecoverySummary, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("an authenticated recovery owner is required")
	}
	return s.runContextualClaimRecovery(ctx, owner, request)
}

func (s *service) RecoverStaleClaimsContext(ctx context.Context, request RunDueRequest) (*ClaimRecoverySummary, error) {
	return s.runContextualClaimRecovery(ctx, "", request)
}

func (s *service) runContextualClaimRecovery(ctx context.Context, owner string, request RunDueRequest) (*ClaimRecoverySummary, error) {
	if ctx == nil || s == nil {
		return nil, ErrClaimRecoveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repository, ok := s.repo.(contextualClaimRecoveryRepository)
	if !ok {
		return nil, ErrClaimRecoveryContextUnavailable
	}
	scoped, err := repository.withClaimRecoveryContext(ctx)
	if err != nil {
		return nil, err
	}
	atomic, ok := scoped.(atomicClaimRecoveryRepository)
	if !ok {
		return nil, ErrClaimRecoveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A narrow view avoids copying the active-task sync.Map or changing root context.
	view := &service{repo: scoped, lifeOntologyProjector: s.lifeOntologyProjector}
	now := time.Now().UTC()
	items, err := scoped.FindExpiredWorkflowClaimsForOwner(owner, now, normalizeRunLimit(request.Limit))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loops, err := scoped.FindExpiredOpenLoopClaimsForOwner(owner, now, normalizeRunLimit(request.Limit))
	if err != nil {
		return nil, err
	}
	summary := &ClaimRecoverySummary{Results: []ClaimRecoveryResult{}}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.Checked++
		recovered, changed, recoverErr := atomic.RecoverExpiredWorkflowClaimAtomic(item, now)
		if recoverErr != nil {
			return summary, errors.Join(ErrClaimRecoveryOutcomeUnconfirmed, recoverErr)
		}
		if changed && recovered == nil {
			return summary, ErrClaimRecoveryOutcomeUnconfirmed
		}
		if !changed {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{WorkflowID: item.ID, Type: "workflow", Status: "skipped", Message: "workflow claim was renewed or recovered by another worker"})
			continue
		}
		summary.WorkflowsBlocked++
		summary.Results = append(summary.Results, ClaimRecoveryResult{WorkflowID: recovered.ID, Type: "workflow", Status: "blocked", Message: workflowRecoveryMessage})
		view.projectWorkflowTransitionContext(ctx, recovered.ID, StateInProgress, StateBlocked, "worker_lease_expired")
	}
	for _, loop := range loops {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.Checked++
		recovered, changed, recoverErr := atomic.RecoverExpiredOpenLoopClaimAtomic(owner, loop, now)
		if recoverErr != nil {
			return summary, errors.Join(ErrClaimRecoveryOutcomeUnconfirmed, recoverErr)
		}
		if changed && recovered == nil {
			return summary, ErrClaimRecoveryOutcomeUnconfirmed
		}
		if !changed {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Type: "open_loop", Status: "skipped", Message: "open-loop claim was recovered by another worker"})
			continue
		}
		summary.OpenLoopsReopened++
		summary.Results = append(summary.Results, ClaimRecoveryResult{WorkflowID: recovered.WorkflowID, OpenLoopID: recovered.ID, Type: "open_loop", Status: "reopened", Message: openLoopRecoveryMessage})
	}
	return summary, ctx.Err()
}
