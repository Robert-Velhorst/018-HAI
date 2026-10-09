package workflow

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrTaskExecutionContextUnavailable = errors.New("workflow task execution requires cancellation-aware execution")

type ContextualTaskRunner interface {
	RunWorkflowTaskContext(context.Context, TaskRunRequest) (*TaskRunResult, error)
}

type ContextualWorkflowExecutionService interface {
	RunDueForOwnerContext(context.Context, string, RunDueRequest) (*WorkflowRunSummary, error)
	RunOneForOwnerContext(context.Context, string, uuid.UUID) (*WorkflowRunResult, error)
}

type ContextualWorkflowExecutionBatchService interface {
	RunDueContext(context.Context, RunDueRequest) (*WorkflowRunSummary, error)
}

func workflowExecutionContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *service) checkTaskExecutionContext(ctx context.Context) error {
	if ctx == nil {
		return ErrTaskExecutionContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.taskRunner.(ContextualTaskRunner); !ok {
		return ErrTaskExecutionContextUnavailable
	}
	return nil
}

func (s *service) RunDueForOwnerContext(ctx context.Context, owner string, request RunDueRequest) (*WorkflowRunSummary, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, ErrTaskExecutionContextUnavailable
	}
	if err := s.checkTaskExecutionContext(ctx); err != nil {
		return nil, err
	}
	return s.runDueForOwnerContext(ctx, owner, request)
}

// The batch entry point is exclusively for trusted background scheduling.
func (s *service) RunDueContext(ctx context.Context, request RunDueRequest) (*WorkflowRunSummary, error) {
	if err := s.checkTaskExecutionContext(ctx); err != nil {
		return nil, err
	}
	return s.runDueForOwnerContext(ctx, "", request)
}

func (s *service) RunOneForOwnerContext(ctx context.Context, owner string, id uuid.UUID) (*WorkflowRunResult, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, ErrTaskExecutionContextUnavailable
	}
	if err := s.checkTaskExecutionContext(ctx); err != nil {
		return nil, err
	}
	return s.runOneForOwnerContext(ctx, owner, id)
}
