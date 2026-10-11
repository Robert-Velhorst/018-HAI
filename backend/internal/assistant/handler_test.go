package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCommandHandlerUsesVerifiedOwnerForSelectedPursuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := &fakePursuitCommandRouter{}
	handler := NewHandler(NewService(&fakeTaskEngine{}, nil, router))
	pursuitID := uuid.New()

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/assistant/command", handler.Command)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Plan the evidence review","pursuitId":"`+pursuitID.String()+`"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("command status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if router.lastOwner != "alice" {
		t.Fatalf("selected pursuit owner = %q, want alice", router.lastOwner)
	}
}

func TestCommandHandlerRejectsUnauthenticatedCycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(&fakeTaskEngine{}, &fakeAgentCycleRunner{}))
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Next() })
	engine.POST("/assistant/command", handler.Command)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Run maintenance","runCycle":true}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("cycle status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestAssistantRoutesRequireVerifiedOwnerForEveryCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(&fakeTaskEngine{}, nil))

	unauthenticated := gin.New()
	unauthenticatedRoutes := unauthenticated.Group("/assistant")
	unauthenticatedRoutes.Use(RequireAuthenticatedOwner())
	unauthenticatedRoutes.POST("/command", handler.Command)
	unauthenticatedRecorder := httptest.NewRecorder()
	unauthenticatedRequest := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Plan a safe next action"}`))
	unauthenticatedRequest.Header.Set("Content-Type", "application/json")
	unauthenticated.ServeHTTP(unauthenticatedRecorder, unauthenticatedRequest)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated command status = %d, want %d: %s", unauthenticatedRecorder.Code, http.StatusUnauthorized, unauthenticatedRecorder.Body.String())
	}

	directRecorder := httptest.NewRecorder()
	directContext, _ := gin.CreateTestContext(directRecorder)
	directContext.Request = httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Plan a safe next action"}`))
	directContext.Request.Header.Set("Content-Type", "application/json")
	handler.Command(directContext)
	if directRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("direct unauthenticated command status = %d, want %d: %s", directRecorder.Code, http.StatusUnauthorized, directRecorder.Body.String())
	}

	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	authenticatedRoutes := authenticated.Group("/assistant")
	authenticatedRoutes.Use(RequireAuthenticatedOwner())
	authenticatedRoutes.POST("/command", handler.Command)
	authenticatedRecorder := httptest.NewRecorder()
	authenticatedRequest := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Plan a safe next action"}`))
	authenticatedRequest.Header.Set("Content-Type", "application/json")
	authenticated.ServeHTTP(authenticatedRecorder, authenticatedRequest)
	if authenticatedRecorder.Code != http.StatusOK {
		t.Fatalf("authenticated command status = %d, want %d: %s", authenticatedRecorder.Code, http.StatusOK, authenticatedRecorder.Body.String())
	}
}

func TestCommandHandlerAllowsAuthenticatedPersonalCycleForOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cycle := &fakeAgentCycleRunner{}
	handler := NewHandler(NewService(&fakeTaskEngine{}, cycle))
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "operator")
		c.Next()
	})
	engine.POST("/assistant/command", handler.Command)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Refresh my operating brief","runCycle":true}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("personal cycle status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if cycle.lastRequest.OwnerIdentity != "alice" {
		t.Fatalf("personal cycle owner = %q, want alice", cycle.lastRequest.OwnerIdentity)
	}
}

func TestCommandHandlerStopsCanceledRequestWithoutStartingAssistantWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tasks := &contextAwareTaskEngine{}
	cycle := &contextAwareAgentCycleRunner{}
	handler := NewHandler(NewService(tasks, cycle))
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/assistant/command", handler.Command)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Refresh my operating brief","runCycle":true}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Body.Len() != 0 {
		t.Fatalf("canceled command wrote a response body: %s", recorder.Body.String())
	}
	if tasks.planCalls != 0 || tasks.runCalls != 0 || cycle.calls != 0 {
		t.Fatalf("canceled HTTP command started work: plan=%d run=%d cycle=%d", tasks.planCalls, tasks.runCalls, cycle.calls)
	}
}
