package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

type alteredReviewOutcomeRepository struct {
	*MemoryTaskStateRepository
	alter   func(*ReviewQueueItem)
	drop    bool
	outcome ReviewOutcome
}

func (r *alteredReviewOutcomeRepository) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return r, ctx.Err()
}

func (r *alteredReviewOutcomeRepository) MarkReviewOutcome(owner, id string, outcome ReviewOutcome) (*ReviewQueueItem, error) {
	r.outcome = outcome
	item, err := r.MemoryTaskStateRepository.MarkReviewOutcome(owner, id, outcome)
	if err == nil && r.drop {
		return nil, nil
	}
	if err == nil && item != nil {
		r.alter(item)
	}
	return item, err
}

func TestReviewOutcomeAcknowledgementRetainsReceivedExecution(t *testing.T) {
	cases := map[string]func(*ReviewQueueItem){
		"item":     func(r *ReviewQueueItem) { r.ID = "another-item" },
		"owner":    func(r *ReviewQueueItem) { r.Request.OwnerIdentity = "bob" },
		"intent":   func(r *ReviewQueueItem) { r.Request.Request = "another action" },
		"task":     func(r *ReviewQueueItem) { r.TaskID = "another-plan" },
		"status":   func(r *ReviewQueueItem) { r.Status = "rejected" },
		"reason":   func(r *ReviewQueueItem) { r.Reason = "another outcome" },
		"created":  func(r *ReviewQueueItem) { r.CreatedAt = time.Time{} },
		"decision": func(r *ReviewQueueItem) { r.Decision = "rejected" },
		"note":     func(r *ReviewQueueItem) { r.ResolutionNote = "another decision" },
		"time":     func(r *ReviewQueueItem) { at := time.Time{}; r.ResolvedAt = &at },
		"missing":  func(r *ReviewQueueItem) {},
	}
	for _, interrupted := range []bool{false, true} {
		name := "normal"
		if interrupted {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			for field, alter := range cases {
				t.Run(field, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					executorCancel := func() {}
					if interrupted {
						executorCancel = cancel
					}
					repo := &alteredReviewOutcomeRepository{MemoryTaskStateRepository: NewMemoryTaskStateRepository(), alter: alter, drop: field == "missing"}
					executor := &cancellingTaskExecutor{ctx: ctx, cancel: executorCancel, tool: completedToolResult()}
					svc := newDurableTaskTestService(t, repo, executor)
					plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
					if err != nil || plan == nil || plan.ReviewQueueItem == nil {
						t.Fatalf("prepare approval: %v", err)
					}
					id := plan.ReviewQueueItem.ID
					result, err := svc.(ContextualReviewService).ResolveReviewItemForOwnerContext(ctx, "alice", id, ApprovalDecision{Approved: true})
					if !errors.Is(err, ErrTaskReviewBindingMismatch) || !errors.Is(err, ErrTaskOperationNeedsReview) || result == nil || result.Plan == nil || result.Plan.ExecutionResult == nil || result.Plan.ExecutionResult.ToolExecution == nil || executor.calls != 1 {
						t.Fatalf("unbound outcome accepted or evidence lost: result=%v error=%v calls=%d", result != nil, err, executor.calls)
					}
					if result.Item.ID != id || result.Item.Status != "approved" || result.Item.Request.OwnerIdentity != "alice" {
						t.Fatal("substituted outcome overwrote acknowledged approval")
					}
					if result.Plan.ExecutionResult.ToolExecution.LaunchEventID != executor.tool.LaunchEventID {
						t.Fatal("original execution receipt was replaced")
					}
					stored, readErr := repo.MemoryTaskStateRepository.FindReviewItem("alice", id)
					if readErr != nil || stored.Status != repo.outcome.Status || stored.TaskID != repo.outcome.TaskPlanID {
						t.Fatal("authoritative outcome changed while refusing its faulty acknowledgement")
					}
					if interrupted && !errors.Is(err, context.Canceled) {
						t.Fatal("outcome mismatch erased caller cancellation")
					}
				})
			}
		})
	}
}
