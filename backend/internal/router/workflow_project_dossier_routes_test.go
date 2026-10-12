package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-backend/internal/workflow"

	"github.com/gin-gonic/gin"
)

type projectDossierRouteService struct {
	owner, projectKey string
	dossier           *workflow.ProjectDossier
	err               error
}

func (s *projectDossierRouteService) ProjectDossierForOwner(owner, projectKey string) (*workflow.ProjectDossier, error) {
	s.owner, s.projectKey = owner, projectKey
	return s.dossier, s.err
}

func TestWorkflowProjectDossierRouteIsRegisteredOwnerScopedAndReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dossierService := &projectDossierRouteService{dossier: &workflow.ProjectDossier{
		ProjectKey:  "Case-A",
		GeneratedAt: time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC),
		Workflows:   []workflow.ProjectDossierWorkflow{},
		Memories:    []workflow.ProjectDossierMemory{},
	}}
	handler := workflow.NewHandlerWithPursuitIntakeRouterAndProjectDossierService(nil, nil, dossierService)

	unauthenticated := gin.New()
	initializeWorkflowRoutes(unauthenticated.Group("/api/v1"), handler)
	unauthenticatedResponse := httptest.NewRecorder()
	unauthenticated.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodGet, "/api/v1/workflow/project-dossier?projectKey=Case-A", nil))
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401: %s", unauthenticatedResponse.Code, unauthenticatedResponse.Body.String())
	}
	if dossierService.owner != "" {
		t.Fatalf("unauthenticated request reached dossier reader as %q", dossierService.owner)
	}

	authenticated := gin.New()
	authenticated.Use(testIdentityMiddleware())
	initializeWorkflowRoutes(authenticated.Group("/api/v1"), handler)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workflow/project-dossier?projectKey=Case-A", nil)
	request.Header.Set("X-Test-Verified-Role", "viewer")
	authenticated.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated read status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if dossierService.owner != "alice" || dossierService.projectKey != "Case-A" {
		t.Fatalf("reader owner/project = %q/%q, want verified owner and exact query", dossierService.owner, dossierService.projectKey)
	}
	for _, route := range authenticated.Routes() {
		if route.Path == "/api/v1/workflow/project-dossier" && route.Method != http.MethodGet {
			t.Fatalf("project dossier registered mutating method %s", route.Method)
		}
	}
}

func TestWorkflowProjectDossierRouteValidatesAndFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	dossierService := &projectDossierRouteService{err: workflow.ErrProjectDossierMemoryUnavailable}
	initializeWorkflowRoutes(engine.Group("/api/v1"), workflow.NewHandlerWithPursuitIntakeRouterAndProjectDossierService(nil, nil, dossierService))

	emptyKey := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workflow/project-dossier?projectKey=%20", nil)
	request.Header.Set("X-Test-Verified-Role", "viewer")
	engine.ServeHTTP(emptyKey, request)
	if emptyKey.Code != http.StatusBadRequest {
		t.Fatalf("empty project key status = %d, want 400: %s", emptyKey.Code, emptyKey.Body.String())
	}
	if dossierService.owner != "" {
		t.Fatal("invalid project key reached dossier reader")
	}

	unavailable := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/workflow/project-dossier?projectKey=Case-A", nil)
	request.Header.Set("X-Test-Verified-Role", "viewer")
	engine.ServeHTTP(unavailable, request)
	if unavailable.Code != http.StatusServiceUnavailable || unavailable.Body.String() == "" {
		t.Fatalf("missing owner-scoped memory status/body = %d/%q, want 503 with safe error", unavailable.Code, unavailable.Body.String())
	}
}
