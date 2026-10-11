package agentcycle

import (
	"automation-hub-backend/internal/identity"
	"errors"
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

func TestHandlerDoesNotExposePersonalCycleDependencyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := []string{}
	handler := NewHandler(NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls, err: errors.New(`postgres password=not-for-http at C:\\private`)},
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
	for _, forbidden := range []string{"password", "not-for-http", "C:\\\\private"} {
		if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "refresh personal pursuit operating brief could not be completed") {
		t.Fatalf("response lacks stable phase error: %s", recorder.Body.String())
	}
}
