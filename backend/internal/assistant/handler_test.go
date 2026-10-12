package assistant

import (
	"errors"
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

func TestCommandHandlerDoesNotExposeUnexpectedTaskEngineErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(&fakeTaskEngine{planErr: errors.New(`postgres password=not-for-http at C:\\private`)}, nil))
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/assistant/command", handler.Command)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Plan a safe next action"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	for _, forbidden := range []string{"password", "not-for-http", "C:\\\\private", "result"} {
		if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "assistant command could not be completed") {
		t.Fatalf("response lacks stable error: %s", recorder.Body.String())
	}
}
