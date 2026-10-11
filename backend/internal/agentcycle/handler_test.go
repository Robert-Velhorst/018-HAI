package agentcycle

import (
	"automation-hub-backend/internal/identity"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerUsesAuthenticatedOwnerForPersonalCycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := []string{}
	handler := NewHandler(NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/agent-cycle/run", handler.Run)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agent-cycle/run", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("run status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := string(recorder.Body.Bytes()); !strings.Contains(got, `"executionScope":"owner_scoped"`) {
		t.Fatalf("handler did not use owner-scoped execution: %s", got)
	}
}

func TestHandlerRejectsUnauthenticatedGlobalCycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := []string{}
	handler := NewHandler(NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	))
	router := gin.New()
	router.POST("/agent-cycle/run", handler.Run)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agent-cycle/run", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("unauthenticated route started work: %v", calls)
	}
}

func TestHandlerRejectsMalformedChunkedRequestBeforeStartingCycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := []string{}
	handler := NewHandler(NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/agent-cycle/run", handler.Run)

	request := httptest.NewRequest(http.MethodPost, "/agent-cycle/run", strings.NewReader(`{"limit":`))
	request.Header.Set("Content-Type", "application/json")
	request.ContentLength = -1
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed cycle status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("malformed cycle request started work: %v", calls)
	}
}

func TestHandlerDoesNotStartCycleForCancelledRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := []string{}
	handler := NewHandler(NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/agent-cycle/run", handler.Run)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/agent-cycle/run", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if len(calls) != 0 {
		t.Fatalf("cancelled request started work: %v", calls)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("cancelled request wrote response body: %s", recorder.Body.String())
	}
}
