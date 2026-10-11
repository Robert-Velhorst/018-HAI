package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

type alteredReviewAcknowledgementRepository struct {
	*MemoryTaskStateRepository
	alter func(*PersistedReviewResolution)
}

func (r *alteredReviewAcknowledgementRepository) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return r, ctx.Err()
}

func (r *alteredReviewAcknowledgementRepository) ResolveReviewItem(owner, id string, decision ReviewResolution) (*PersistedReviewResolution, error) {
	result, err := r.MemoryTaskStateRepository.ResolveReviewItem(owner, id, decision)
	if err == nil && result != nil {
		r.alter(result)
	}
	return result, err
}

func TestReviewAcknowledgementMustMatchRequestedDecision(t *testing.T) {
	cases := map[string]func(*PersistedReviewResolution){
		"item":          func(r *PersistedReviewResolution) { r.Item.ID = "another-item" },
		"owner":         func(r *PersistedReviewResolution) { r.Item.Request.OwnerIdentity = "bob" },
		"intent":        func(r *PersistedReviewResolution) { r.Item.Request.Request = "another action" },
		"task":          func(r *PersistedReviewResolution) { r.Decision.TaskPlanID = "another-plan" },
		"decision_item": func(r *PersistedReviewResolution) { r.Decision.ReviewItemID = "another-item" },
		"resolver":      func(r *PersistedReviewResolution) { r.Decision.ResolvedBy = "bob" },
		"decision":      func(r *PersistedReviewResolution) { r.Decision.Decision = "unexpected" },
		"status":        func(r *PersistedReviewResolution) { r.Item.Status = "unexpected" },
		"source":        func(r *PersistedReviewResolution) { r.Decision.ApprovalSource = "untrusted" },
		"source_id":     func(r *PersistedReviewResolution) { r.Decision.ApprovalSourceID = "task-review:another-item" },
		"digest":        func(r *PersistedReviewResolution) { r.Decision.RequestDigest = "unbound" },
		"revision":      func(r *PersistedReviewResolution) { r.Decision.ReviewRevision = 0 },
		"time":          func(r *PersistedReviewResolution) { r.Decision.ResolvedAt = time.Time{} },
		"note":          func(r *PersistedReviewResolution) { r.Decision.ResolutionNote = "another decision note" },
		"decision_id":   func(r *PersistedReviewResolution) { r.Decision.ID = "" },
		"item_task":     func(r *PersistedReviewResolution) { r.Item.TaskID = "another-plan" },
		"created_at":    func(r *PersistedReviewResolution) { r.Item.CreatedAt = time.Time{} },
		"item_decision": func(r *PersistedReviewResolution) { r.Item.Decision = "unexpected" },
		"item_note":     func(r *PersistedReviewResolution) { r.Item.ResolutionNote = "another note" },
		"item_time":     func(r *PersistedReviewResolution) { r.Item.ResolvedAt = nil },
		"stale_time": func(r *PersistedReviewResolution) {
			at := r.Decision.ResolvedAt.Add(-time.Second)
			r.Decision.ResolvedAt = at
			r.Item.ResolvedAt = &at
		},
	}
	for decisionName, approved := range map[string]bool{"approved": true, "rejected": false} {
		t.Run(decisionName, func(t *testing.T) {
			for name, alter := range cases {
				t.Run(name, func(t *testing.T) {
					repo := &alteredReviewAcknowledgementRepository{MemoryTaskStateRepository: NewMemoryTaskStateRepository(), alter: alter}
					executor := &cancellingTaskExecutor{ctx: context.Background(), cancel: func() {}, tool: completedToolResult()}
					svc := newDurableTaskTestService(t, repo, executor)
					plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
					if err != nil || plan == nil || plan.ReviewQueueItem == nil {
						t.Fatalf("prepare review: %v", err)
					}
					id := plan.ReviewQueueItem.ID
					result, err := svc.(ContextualReviewService).ResolveReviewItemForOwnerContext(context.Background(), "alice", id, ApprovalDecision{Approved: approved, Note: "Operator decision"})
					if !errors.Is(err, ErrTaskReviewBindingMismatch) || !errors.Is(err, ErrTaskOperationNeedsReview) || result != nil || executor.calls != 0 {
						t.Fatalf("unbound acknowledgement accepted: result=%v error=%v calls=%d", result != nil, err, executor.calls)
					}
					stored, err := repo.MemoryTaskStateRepository.FindReviewItem("alice", id)
					if err != nil || stored.Status != decisionName || stored.Request.OwnerIdentity != "alice" {
						t.Fatal("authoritative decision was modified during acknowledgement validation")
					}
				})
			}
		})
	}
}
