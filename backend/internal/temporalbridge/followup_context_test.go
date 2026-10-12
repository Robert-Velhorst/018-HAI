package temporalbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/workflow"
	"go.temporal.io/sdk/client"
)

type contextualActivityWorkflowService struct {
	workflow.Service
	legacyCalls, contextCalls int
	gotContext                context.Context
	gotOwner                  string
	gotLimit                  int
	run                       func(context.Context) (*workflow.OpenLoopRunSummary, error)
}

func (w *contextualActivityWorkflowService) RunDueOpenLoopsForOwner(string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.legacyCalls++
	return &workflow.OpenLoopRunSummary{}, nil
}

func (w *contextualActivityWorkflowService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.contextCalls++
	w.gotContext, w.gotOwner, w.gotLimit = ctx, owner, request.Limit
	return w.run(ctx)
}

func TestActivityFollowUpReceivesCancellableExecutionContext(t *testing.T) {
	repo, id := activityBoundaryRow(t, "scheduled")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	work := &contextualActivityWorkflowService{run: func(received context.Context) (*workflow.OpenLoopRunSummary, error) {
		cancel()
		return nil, received.Err()
	}}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	_, err := a.Run(ctx, FollowUpInput{RunID: id.String(), Limit: 7})
	if err == nil || work.legacyCalls != 0 || work.contextCalls != 1 || work.gotContext != ctx ||
		work.gotOwner != "robert@example.test" || work.gotLimit != 7 || !errors.Is(work.gotContext.Err(), context.Canceled) {
		t.Fatalf("follow-up discarded execution context: err=%v legacy=%d contextual=%d owner=%q limit=%d", err, work.legacyCalls, work.contextCalls, work.gotOwner, work.gotLimit)
	}
	if repo.lastStatus() != "failed" {
		t.Fatal("canceled proposal outcome was not retained for review")
	}
}

type legacyOnlyActivityWorkflowService struct {
	workflow.Service
	calls int
}

func (w *legacyOnlyActivityWorkflowService) RunDueOpenLoopsForOwner(string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.calls++
	return &workflow.OpenLoopRunSummary{}, nil
}

func TestActivityFollowUpRejectsContextlessAdapterBeforeClaim(t *testing.T) {
	repo, id := activityBoundaryRow(t, "scheduled")
	work := &legacyOnlyActivityWorkflowService{}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
	if err == nil || work.calls != 0 || repo.lastStatus() != "scheduled" {
		t.Fatalf("contextless adapter executed or claimed: err=%v calls=%d status=%s", err, work.calls, repo.lastStatus())
	}
}

func TestFollowUpStartupRejectsContextlessAdapterBeforeDial(t *testing.T) {
	work := &legacyOnlyActivityWorkflowService{}
	s := NewService(newMemoryTemporalRepository(), work, true, "127.0.0.1:7233", "default", "hai-test")
	dials := 0
	s.dial = func(context.Context, client.Options) (client.Client, error) {
		dials++
		return nil, errors.New("offline probe must not dial")
	}
	s.StartWorkerContext(context.Background())
	status := s.Status()
	if dials != 0 || status.WorkerStarted || status.WorkerStarting || status.WorkerError != "context-aware governed follow-up adapter is required" {
		t.Fatalf("unsupported follow-up worker attempted startup: dials=%d status=%+v", dials, status)
	}
}
