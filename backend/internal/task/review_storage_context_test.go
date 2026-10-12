package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
)

type reviewStorageEvent struct {
	kind string
	id   string
	ctx  context.Context
	err  error
}

type reviewStorageProbe struct {
	*MemoryTaskStateRepository
	events        []reviewStorageEvent
	afterDecision context.CancelFunc
	outcomeError  error
}

type reviewStorageView struct {
	TaskStateRepository
	root *reviewStorageProbe
	ctx  context.Context
}

func (r *reviewStorageProbe) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return &reviewStorageView{TaskStateRepository: r.MemoryTaskStateRepository, root: r, ctx: ctx}, ctx.Err()
}

func (v *reviewStorageView) event(kind, id string) error {
	err := v.ctx.Err()
	v.root.events = append(v.root.events, reviewStorageEvent{kind: kind, id: id, ctx: v.ctx, err: err})
	return err
}

func (v *reviewStorageView) FindReviewItem(owner, id string) (*ReviewQueueItem, error) {
	if err := v.event("lookup", id); err != nil {
		return nil, err
	}
	return v.TaskStateRepository.FindReviewItem(owner, id)
}

func (v *reviewStorageView) ResolveReviewItem(owner, id string, decision ReviewResolution) (*PersistedReviewResolution, error) {
	if err := v.event("decision", id); err != nil {
		return nil, err
	}
	result, err := v.TaskStateRepository.ResolveReviewItem(owner, id, decision)
	if err == nil && v.root.afterDecision != nil {
		v.root.afterDecision()
	}
	return result, err
}

func (v *reviewStorageView) MarkReviewOutcome(owner, id string, outcome ReviewOutcome) (*ReviewQueueItem, error) {
	if err := v.event("outcome", id); err != nil {
		return nil, err
	}
	if v.root.outcomeError != nil {
		return nil, v.root.outcomeError
	}
	return v.TaskStateRepository.MarkReviewOutcome(owner, id, outcome)
}

func TestOwnedReviewStorageScopesDecisionAndCancellationOutcome(t *testing.T) {
	for _, boundary := range []string{"rejected", "cancelled_runtime", "rejected_cancel", "outcome_failure"} {
		t.Run(boundary, func(t *testing.T) {
			approve := boundary == "cancelled_runtime" || boundary == "outcome_failure"
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "review-lifetime"))
			defer cancel()
			repo := &reviewStorageProbe{MemoryTaskStateRepository: NewMemoryTaskStateRepository()}
			executor := &cancellingTaskExecutor{ctx: ctx, cancel: cancel, tool: completedToolResult()}
			svc := newDurableTaskTestService(t, repo, executor)
			plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
			if err != nil || plan == nil || plan.ReviewQueueItem == nil {
				t.Fatalf("prepare approval: %v", err)
			}
			id := plan.ReviewQueueItem.ID
			repo.events = nil
			if boundary == "rejected_cancel" {
				repo.afterDecision = cancel
			}
			if boundary == "outcome_failure" {
				executor.cancel = func() {}
				repo.outcomeError = errors.New("controlled review outcome failure")
			}
			result, err := svc.(ContextualReviewService).ResolveReviewItemForOwnerContext(ctx, "alice", id, ApprovalDecision{Approved: approve})
			if (boundary == "cancelled_runtime" || boundary == "rejected_cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost runtime cancellation: %v", err)
			}
			if boundary == "rejected" && (err != nil || result == nil) {
				t.Fatalf("rejection failed: %v", err)
			}
			if boundary == "outcome_failure" {
				if !errors.Is(err, repo.outcomeError) || !errors.Is(err, ErrTaskOperationNeedsReview) || result == nil || result.Plan == nil || result.Plan.ExecutionResult.ToolExecution == nil {
					t.Fatalf("outcome failure erased result or claimed acknowledgement: %#v %v", result, err)
				}
				stored, readErr := repo.MemoryTaskStateRepository.FindReviewItem("alice", id)
				if readErr != nil || stored.Status != "approved" {
					t.Fatal("failed outcome write was represented as acknowledged")
				}
			}
			if boundary == "rejected_cancel" && (result == nil || result.Item.Status != "rejected" || result.LearningOutcomeID != "" || executor.calls != 0) {
				t.Fatal("cancellation erased rejected decision or started another stage")
			}
			found := map[string]bool{}
			for _, event := range repo.events {
				if event.id != id {
					continue
				}
				found[event.kind] = true
				if event.ctx.Value(taskStorageContextKey{}) != "review-lifetime" || event.err != nil {
					t.Fatalf("review storage dropped lifetime or used dead context: %#v", event)
				}
				if _, ok := event.ctx.Deadline(); !ok {
					t.Fatal("review storage lacks deadline")
				}
			}
			if !found["lookup"] || !found["decision"] || (approve && !found["outcome"]) {
				t.Fatalf("review storage was not scoped: %#v", found)
			}
		})
	}
}

func TestOwnedReviewRefusesLegacyStorageBeforeDecision(t *testing.T) {
	ctx := context.Background()
	memory := NewMemoryTaskStateRepository()
	executor := &cancellingTaskExecutor{ctx: ctx, cancel: func() {}, tool: completedToolResult()}
	svc := newDurableTaskTestService(t, memory, executor)
	plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script", ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
	if err != nil || plan == nil || plan.ReviewQueueItem == nil {
		t.Fatalf("prepare approval: %v", err)
	}
	svc.(*service).stateRepository = &legacyTaskStorage{memory}
	id := plan.ReviewQueueItem.ID
	_, err = svc.(ContextualReviewService).ResolveReviewItemForOwnerContext(ctx, "alice", id, ApprovalDecision{Approved: true})
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/task/review/"+id, strings.NewReader(`{"approved":true}`))
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set(identity.ContextSubjectKey, "alice")
	(&Handler{service: svc}).ResolveReviewItem(c)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing owned storage was not reported as unavailable: %d %s", response.Code, response.Body.String())
	}
	decisions, readErr := memory.ListReviewDecisions("alice", id, 10)
	if !errors.Is(err, ErrTaskStorageContextUnavailable) || readErr != nil || len(decisions) != 0 || executor.calls != 0 {
		t.Fatalf("legacy storage created authority or execution: %v decisions=%d calls=%d", err, len(decisions), executor.calls)
	}
}
