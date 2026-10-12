package task

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestResolveReviewItemExplainsUncertainOutcomeReconciliation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capturingTaskService{resolveErr: ErrTaskOutcomeReconciliationRequired}
	handler := NewHandler(service)
	request := httptest.NewRequest(http.MethodPost, "/task/review-queue/item/resolve", strings.NewReader(`{"approved":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: "item"}}
	context.Set(identity.ContextSubjectKey, "alice")
	handler.ResolveReviewItem(context)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "outcome_reconciliation_required") || !strings.Contains(response.Body.String(), "workflow recovery") || service.resolveOwner != "alice" {
		t.Fatalf("missing owner-scoped recovery contract: status=%d body=%s owner=%q", response.Code, response.Body.String(), service.resolveOwner)
	}
}

func TestMissingPlanReviewReturnsReconciliationWithoutMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := NewMemoryTaskStateRepository()
	executor := &fakeToolExecutor{result: completedToolResult()}
	engine := &service{stateRepository: repo, toolExecutor: executor}
	item := taskStateTestReviewItem("alice", "missing-plan", time.Now().UTC())
	queued, err := repo.CreateReviewItem("alice", item)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id      string
		status  int
		message string
	}{
		{queued.ID, http.StatusConflict, "outcome_reconciliation_required"},
		{"absent-review", http.StatusNotFound, "review item not found"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/task/review-queue/"+test.id+"/resolve", strings.NewReader(`{"approved":true}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = request
		context.Params = gin.Params{{Key: "id", Value: test.id}}
		context.Set(identity.ContextSubjectKey, "alice")
		NewHandler(engine).ResolveReviewItem(context)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) || executor.calls != 0 {
			t.Fatalf("wrong recovery contract: status=%d body=%s effects=%d", response.Code, response.Body.String(), executor.calls)
		}
	}
	stored, err := repo.FindReviewItem("alice", queued.ID)
	if err != nil || stored.Status != "open" || stored.Decision != "" {
		t.Fatalf("missing evidence mutated authority: err=%v item=%#v", err, stored)
	}
	if _, err := engine.ResolveReviewItemForOwner("alice", queued.ID, ApprovalDecision{Approved: false}); err != nil {
		t.Fatalf("missing plan blocked safe rejection: %v", err)
	}
}
