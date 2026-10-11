package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

type approvalReadProbe struct {
	*MemoryTaskStateRepository
	events                   []reviewStorageEvent
	afterItem, afterApproval context.CancelFunc
	nilItem, nilApproval     bool
}

type approvalReadView struct {
	TaskStateRepository
	root *approvalReadProbe
	ctx  context.Context
}

func (r *approvalReadProbe) FindReviewItem(owner, id string) (*ReviewQueueItem, error) {
	return (&approvalReadView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: context.Background()}).FindReviewItem(owner, id)
}

func (r *approvalReadProbe) FindApprovedReviewDecision(owner, id string) (*ReviewDecisionRecord, error) {
	return (&approvalReadView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: context.Background()}).FindApprovedReviewDecision(owner, id)
}

func (r *approvalReadProbe) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return &approvalReadView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: ctx}, ctx.Err()
}

func (v *approvalReadView) FindReviewItem(owner, id string) (*ReviewQueueItem, error) {
	v.root.events = append(v.root.events, reviewStorageEvent{kind: "item", ctx: v.ctx})
	if v.root.nilItem {
		return nil, nil
	}
	item, err := v.TaskStateRepository.FindReviewItem(owner, id)
	if v.root.afterItem != nil {
		v.root.afterItem()
	}
	return item, err
}

func (v *approvalReadView) FindApprovedReviewDecision(owner, id string) (*ReviewDecisionRecord, error) {
	v.root.events = append(v.root.events, reviewStorageEvent{kind: "approval", ctx: v.ctx})
	if v.root.nilApproval {
		return nil, nil
	}
	decision, err := v.TaskStateRepository.FindApprovedReviewDecision(owner, id)
	if v.root.afterApproval != nil {
		v.root.afterApproval()
	}
	return decision, err
}

func TestExecutionApprovalReadbackOwnsCallerContext(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after_item", "cancel_after_approval", "missing_capability", "nil_item", "nil_approval"} {
		t.Run(boundary, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Fatal("missing approval acknowledgement crashed execution gate")
				}
			}()
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "approval-read"))
			defer cancel()
			repo := &approvalReadProbe{MemoryTaskStateRepository: NewMemoryTaskStateRepository()}
			executor := &cancellingTaskExecutor{ctx: ctx, cancel: func() {}, tool: completedToolResult()}
			svc := newDurableTaskTestService(t, repo, executor)
			plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
			if err != nil || plan == nil || plan.ReviewQueueItem == nil {
				t.Fatalf("prepare review: %v", err)
			}
			id := plan.ReviewQueueItem.ID
			stored, err := repo.MemoryTaskStateRepository.ResolveReviewItem("alice", id, ReviewResolution{Decision: "approved", ResolvedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			request := stored.Item.Request
			request.ExecutionContext = ctx
			request.reviewItemID = id
			request.ApprovalSourceID = stored.Decision.ApprovalSourceID
			s := svc.(*service)
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after_item":
				repo.afterItem = cancel
			case "cancel_after_approval":
				repo.afterApproval = cancel
			case "missing_capability":
				s.stateRepository = &legacyTaskStorage{repo.MemoryTaskStateRepository}
			case "nil_item":
				repo.nilItem = true
			case "nil_approval":
				repo.nilApproval = true
			}
			decision, err := s.verifiedApprovalDecisionForExecution(plan, request)
			if boundary == "valid" {
				if err != nil || decision == nil || len(repo.events) != 2 {
					t.Fatalf("valid approval did not use owned reads: %v events=%d", err, len(repo.events))
				}
			} else {
				if decision != nil || err == nil {
					t.Fatal("failed approval readback granted execution authority")
				}
				if boundary == "missing_capability" && !errors.Is(err, ErrTaskStorageContextUnavailable) {
					t.Fatalf("missing contextual storage accepted: %v", err)
				}
				if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("readback lost cancellation: %v", err)
				}
			}
			if boundary == "cancel_after_item" && len(repo.events) != 1 {
				t.Fatal("approval read started after cancelled item read")
			}
			for _, event := range repo.events {
				if event.ctx.Value(taskStorageContextKey{}) != "approval-read" {
					t.Fatal("approval read lost caller context")
				}
				if _, ok := event.ctx.Deadline(); !ok {
					t.Fatal("approval read has no storage deadline")
				}
			}
		})
	}
}
