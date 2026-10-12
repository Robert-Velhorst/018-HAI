package router

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/assistant"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/task"

	"github.com/gin-gonic/gin"
)

func TestConsequentialTaskExecutionAndReviewRequireOwnerApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	api := engine.Group("/api/v1")

	tasks := &interactiveTaskService{}
	initializeAssistantRoutes(api, assistant.NewHandler(assistant.NewService(tasks, nil)))
	initializeTaskRoutes(api, task.NewHandler(tasks))

	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
		role   string
		want   int
	}{
		{name: "viewer cannot run assistant command", method: http.MethodPost, path: "/api/v1/assistant/command", body: `{"message":"Run the safe task","executeAllowed":true}`, role: "viewer", want: http.StatusForbidden},
		{name: "operator cannot run assistant command", method: http.MethodPost, path: "/api/v1/assistant/command", body: `{"message":"Run the safe task","executeAllowed":true}`, role: "operator", want: http.StatusForbidden},
		{name: "owner can run assistant command", method: http.MethodPost, path: "/api/v1/assistant/command", body: `{"message":"Run the safe task","executeAllowed":true}`, role: "owner", want: http.StatusOK},
		{name: "viewer cannot execute task", method: http.MethodPost, path: "/api/v1/task/run", body: `{"request":"Run the safe task"}`, role: "viewer", want: http.StatusForbidden},
		{name: "operator cannot execute task", method: http.MethodPost, path: "/api/v1/task/run", body: `{"request":"Run the safe task"}`, role: "operator", want: http.StatusForbidden},
		{name: "owner can execute task", method: http.MethodPost, path: "/api/v1/task/run", body: `{"request":"Run the safe task"}`, role: "owner", want: http.StatusOK},
		{name: "operator cannot execute success alias", method: http.MethodPost, path: "/api/v1/task/success", body: `{"request":"Run the safe task"}`, role: "operator", want: http.StatusForbidden},
		{name: "owner can execute success alias", method: http.MethodPost, path: "/api/v1/task/success", body: `{"request":"Run the safe task"}`, role: "owner", want: http.StatusOK},
		{name: "viewer cannot resolve review", method: http.MethodPost, path: "/api/v1/task/review-queue/item/resolve", body: `{"approved":true}`, role: "viewer", want: http.StatusForbidden},
		{name: "operator cannot approve review", method: http.MethodPost, path: "/api/v1/task/review-queue/item/resolve", body: `{"approved":true}`, role: "operator", want: http.StatusForbidden},
		{name: "operator cannot deny review", method: http.MethodPost, path: "/api/v1/task/review-queue/item/resolve", body: `{"approved":false}`, role: "operator", want: http.StatusForbidden},
		{name: "owner can approve review", method: http.MethodPost, path: "/api/v1/task/review-queue/item/resolve", body: `{"approved":true}`, role: "owner", want: http.StatusOK},
		{name: "owner can deny review", method: http.MethodPost, path: "/api/v1/task/review-queue/item/resolve", body: `{"approved":false}`, role: "owner", want: http.StatusOK},
		{name: "viewer cannot reconcile reviews", method: http.MethodPost, path: "/api/v1/task/review-queue/reconcile", body: `{}`, role: "viewer", want: http.StatusForbidden},
		{name: "operator cannot reconcile reviews", method: http.MethodPost, path: "/api/v1/task/review-queue/reconcile", body: `{}`, role: "operator", want: http.StatusForbidden},
		{name: "owner can preview review reconciliation", method: http.MethodPost, path: "/api/v1/task/review-queue/reconcile", body: `{}`, role: "owner", want: http.StatusOK},
		{name: "viewer can read review queue", method: http.MethodGet, path: "/api/v1/task/review-queue", role: "viewer", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-Verified-Role", test.role)
			engine.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}

	if tasks.runCalls != 3 {
		t.Fatalf("executions = %d, want exactly the three owner-authorized calls", tasks.runCalls)
	}
	if tasks.resolveCalls != 2 {
		t.Fatalf("review resolutions = %d, want the two owner decisions only", tasks.resolveCalls)
	}
}

func TestInteractivePlanningRoutesRequireSignedWritePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	api := engine.Group("/api/v1")
	tasks := &interactiveTaskService{}
	initializeTaskRoutes(api, task.NewHandler(tasks))

	for _, test := range []struct {
		role string
		want int
	}{
		{role: "viewer", want: http.StatusForbidden},
		{role: "operator", want: http.StatusOK},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/task/plan", bytes.NewBufferString(`{"request":"Plan a safe task"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Test-Verified-Role", test.role)
		engine.ServeHTTP(recorder, request)
		if recorder.Code != test.want {
			t.Fatalf("role %s status = %d, want %d: %s", test.role, recorder.Code, test.want, recorder.Body.String())
		}
	}
	if tasks.planCalls != 1 {
		t.Fatalf("plans = %d, want operator call only", tasks.planCalls)
	}
}

func testIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, c.GetHeader("X-Test-Verified-Role"))
		c.Next()
	}
}

type interactiveTaskService struct {
	planCalls    int
	runCalls     int
	resolveCalls int
}

func (s *interactiveTaskService) ReconcileApprovedReviewsForOwner(string, task.ApprovedReviewReconciliationRequest) (*task.ApprovedReviewReconciliationResult, error) {
	return &task.ApprovedReviewReconciliationResult{DryRun: true, Items: []task.ApprovedReviewReconciliationItem{}}, nil
}

func (s *interactiveTaskService) Plan(request task.IntakeRequest) (*task.CompletionPlan, error) {
	s.planCalls++
	return &task.CompletionPlan{Request: request.Request}, nil
}

func (s *interactiveTaskService) Run(request task.IntakeRequest) (*task.CompletionPlan, error) {
	s.runCalls++
	return &task.CompletionPlan{Request: request.Request}, nil
}

func (s *interactiveTaskService) Logs() []task.CompletionPlan { return nil }

func (s *interactiveTaskService) ReviewQueue() []task.ReviewQueueItem { return nil }

func (s *interactiveTaskService) ResolveReviewItem(string, task.ApprovalDecision) (*task.ReviewResolutionResult, error) {
	s.resolveCalls++
	return &task.ReviewResolutionResult{}, nil
}

func (s *interactiveTaskService) LogsForOwner(string) []task.CompletionPlan { return nil }

func (s *interactiveTaskService) ReviewQueueForOwner(string) []task.ReviewQueueItem { return nil }

func (s *interactiveTaskService) ResolveReviewItemForOwner(_ string, _ string, _ task.ApprovalDecision) (*task.ReviewResolutionResult, error) {
	s.resolveCalls++
	return &task.ReviewResolutionResult{}, nil
}

func (s *interactiveTaskService) ResolveReviewItemForOwnerContext(ctx context.Context, owner, id string, decision task.ApprovalDecision) (*task.ReviewResolutionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.ResolveReviewItemForOwner(owner, id, decision)
}
