package task

import (
	"context"
	"errors"
	"testing"
)

type planningStorageProbe struct {
	*MemoryTaskStateRepository
	events      []reviewStorageEvent
	appendError error
	createError error
	nilReview   bool
	afterAppend context.CancelFunc
	afterCreate context.CancelFunc
}

type planningStorageView struct {
	TaskStateRepository
	root *planningStorageProbe
	ctx  context.Context
}

func (r *planningStorageProbe) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return &planningStorageView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: ctx}, ctx.Err()
}

func (r *planningStorageProbe) AppendCompletionPlan(owner string, plan CompletionPlan) error {
	return (&planningStorageView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: context.Background()}).AppendCompletionPlan(owner, plan)
}

func (r *planningStorageProbe) CreateReviewItem(owner string, item ReviewQueueItem) (*ReviewQueueItem, error) {
	return (&planningStorageView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: context.Background()}).CreateReviewItem(owner, item)
}

func (v *planningStorageView) AppendCompletionPlan(owner string, plan CompletionPlan) error {
	v.root.events = append(v.root.events, reviewStorageEvent{kind: "append", ctx: v.ctx, err: v.ctx.Err()})
	if v.root.appendError != nil {
		return v.root.appendError
	}
	err := v.TaskStateRepository.AppendCompletionPlan(owner, plan)
	if err == nil && v.root.afterAppend != nil {
		v.root.afterAppend()
	}
	return err
}

func (v *planningStorageView) CreateReviewItem(owner string, item ReviewQueueItem) (*ReviewQueueItem, error) {
	v.root.events = append(v.root.events, reviewStorageEvent{kind: "create", ctx: v.ctx, err: v.ctx.Err()})
	if v.root.createError != nil {
		return nil, v.root.createError
	}
	if v.root.nilReview {
		return nil, nil
	}
	stored, err := v.TaskStateRepository.CreateReviewItem(owner, item)
	if err == nil && v.root.afterCreate != nil {
		v.root.afterCreate()
	}
	return stored, err
}

func TestPlanningStorageOwnsContextAndRetainsBuiltPlan(t *testing.T) {
	for _, boundary := range []string{"plan", "append_failure", "append_cancel", "blocked_run", "create_failure", "create_nil", "create_cancel"} {
		t.Run(boundary, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Fatal("missing planning storage acknowledgement crashed service")
				}
			}()
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "planning-storage"))
			defer cancel()
			repo := &planningStorageProbe{MemoryTaskStateRepository: NewMemoryTaskStateRepository()}
			executor := &cancellingTaskExecutor{ctx: ctx, cancel: func() {}, tool: completedToolResult()}
			svc := newDurableTaskTestService(t, repo, executor)
			request := IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true}
			storageError := errors.New("controlled planning storage failure")
			switch boundary {
			case "append_failure":
				repo.appendError = storageError
			case "append_cancel":
				repo.afterAppend = cancel
			case "create_failure":
				repo.createError = storageError
			case "create_nil":
				repo.nilReview = true
			case "create_cancel":
				repo.afterCreate = cancel
			}
			var plan *CompletionPlan
			var err error
			if boundary == "blocked_run" || boundary == "create_failure" || boundary == "create_nil" || boundary == "create_cancel" {
				plan, err = svc.Run(request)
			} else {
				plan, err = svc.Plan(request)
			}
			if plan == nil || executor.calls != 0 {
				t.Fatalf("planning evidence erased or tool dispatched: error=%v calls=%d", err, executor.calls)
			}
			if boundary == "plan" || boundary == "blocked_run" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("failed or cancelled planning acknowledged as success")
			}
			if (boundary == "append_failure" || boundary == "create_failure") && !errors.Is(err, storageError) {
				t.Fatalf("storage error identity lost: %v", err)
			}
			if boundary == "append_cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			if boundary == "create_cancel" {
				if !errors.Is(err, context.Canceled) || plan.ReviewQueueItem == nil {
					t.Fatal("received review item erased or cancellation lost")
				}
				stored, readErr := repo.MemoryTaskStateRepository.FindReviewItem("alice", plan.ReviewQueueItem.ID)
				if readErr != nil || stored.Status != "open" || stored.TaskID != plan.ID {
					t.Fatal("acknowledged review item was overwritten during cancellation")
				}
			}
			if boundary == "create_nil" && !errors.Is(err, ErrTaskReviewBindingMismatch) {
				t.Fatalf("missing acknowledgement accepted: %v", err)
			}
			if len(repo.events) == 0 {
				t.Fatal("storage not exercised")
			}
			for _, event := range repo.events {
				if event.ctx.Value(taskStorageContextKey{}) != "planning-storage" || event.err != nil {
					t.Fatal("planning storage lost live caller scope")
				}
				if _, ok := event.ctx.Deadline(); !ok {
					t.Fatal("planning storage has no deadline")
				}
			}
		})
	}
}
