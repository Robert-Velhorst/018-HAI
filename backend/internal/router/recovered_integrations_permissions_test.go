package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentframework"
	"automation-hub-backend/internal/autogencompat"
	"automation-hub-backend/internal/braincatalog"
	"automation-hub-backend/internal/crewai"
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/gitleaks"
	"automation-hub-backend/internal/gosec"
	"automation-hub-backend/internal/grype"
	"automation-hub-backend/internal/hireai"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/miniswe"
	"automation-hub-backend/internal/mlflow"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openlit"
	"automation-hub-backend/internal/syft"
	"automation-hub-backend/internal/trivy"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type recoveredRouteDefinition struct {
	name             string
	statusPath       string
	privilegedMethod string
	privilegedPath   string
	initialize       func(*gin.RouterGroup)
}

func TestRecoveredIntegrationRoutesRequireIdentityAndOwnerAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	definitions := []recoveredRouteDefinition{
		{
			name: "agent framework", statusPath: "/api/v1/agent-framework/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/agent-framework/probe",
			initialize: func(group *gin.RouterGroup) {
				initializeAgentFrameworkRoutes(group, agentframework.NewHandler(agentframework.NewService(false, "", time.Second, nil)))
			},
		},
		{
			name: "crewai", statusPath: "/api/v1/crewai/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/crewai/probe",
			initialize: func(group *gin.RouterGroup) {
				initializeCrewAIRoutes(group, crewai.NewHandler(crewai.NewService(false, "", time.Second, nil)))
			},
		},
		{
			name: "docling", statusPath: "/api/v1/docling/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/docling/probe",
			initialize: func(group *gin.RouterGroup) {
				initializeDoclingRoutes(group, docling.NewHandler(docling.NewService(docling.Config{}, nil)))
			},
		},
		{
			name: "gitleaks", statusPath: "/api/v1/gitleaks/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/gitleaks/scan",
			initialize: func(group *gin.RouterGroup) {
				initializeGitleaksRoutes(group, gitleaks.NewHandler(gitleaks.NewService(gitleaks.Config{}, nil)))
			},
		},
		{
			name: "gosec", statusPath: "/api/v1/gosec/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/gosec/scan",
			initialize: func(group *gin.RouterGroup) {
				initializeGosecRoutes(group, gosec.NewHandler(gosec.NewService(gosec.Config{}, nil)))
			},
		},
		{
			name: "grype", statusPath: "/api/v1/grype/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/grype/scan",
			initialize: func(group *gin.RouterGroup) {
				initializeGrypeRoutes(group, grype.NewHandler(grype.NewService(grype.Config{}, nil)))
			},
		},
		{
			name: "mini swe", statusPath: "/api/v1/mini-swe/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/mini-swe/workflows/" + uuid.NewString() + "/propose-patch",
			initialize: func(group *gin.RouterGroup) {
				initializeMiniSWERoutes(group, miniswe.NewHandler(miniswe.NewService(nil, nil, miniswe.Config{}, nil)))
			},
		},
		{
			name: "mlflow", statusPath: "/api/v1/mlflow/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/mlflow/probe",
			initialize: func(group *gin.RouterGroup) {
				initializeMLflowRoutes(group, mlflow.NewHandler(mlflow.NewService(false, "", "", "", "", time.Second, nil)))
			},
		},
		{
			name: "openlit", statusPath: "/api/v1/openlit/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/openlit/export/operational-snapshot",
			initialize: func(group *gin.RouterGroup) {
				initializeOpenLITRoutes(group, openlit.NewHandler(openlit.NewService(false, "", time.Second, nil)))
			},
		},
		{
			name: "syft", statusPath: "/api/v1/syft/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/syft/inventory",
			initialize: func(group *gin.RouterGroup) {
				initializeSyftRoutes(group, syft.NewHandler(syft.NewService(syft.Config{}, nil)))
			},
		},
		{
			name: "trivy", statusPath: "/api/v1/trivy/status",
			privilegedMethod: http.MethodPost, privilegedPath: "/api/v1/trivy/scan",
			initialize: func(group *gin.RouterGroup) {
				initializeTrivyRoutes(group, trivy.NewHandler(trivy.NewService(trivy.Config{}, nil)))
			},
		},
	}

	for _, definition := range definitions {
		t.Run(definition.name, func(t *testing.T) {
			unauthenticated := gin.New()
			definition.initialize(unauthenticated.Group("/api/v1"))
			if response := performRecoveredRequest(unauthenticated, http.MethodGet, definition.statusPath, ""); response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status = %d, want %d", response.Code, http.StatusUnauthorized)
			}

			viewer := recoveredRouteEngine("viewer")
			definition.initialize(viewer.Group("/api/v1"))
			if response := performRecoveredRequest(viewer, http.MethodGet, definition.statusPath, ""); response.Code != http.StatusOK {
				t.Fatalf("viewer status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			operator := recoveredRouteEngine("operator")
			definition.initialize(operator.Group("/api/v1"))
			if response := performRecoveredRequest(operator, definition.privilegedMethod, definition.privilegedPath, ""); response.Code != http.StatusForbidden {
				t.Fatalf("operator privileged status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
			}

			owner := recoveredRouteEngine("owner")
			definition.initialize(owner.Group("/api/v1"))
			response := performRecoveredRequest(owner, definition.privilegedMethod, definition.privilegedPath, "")
			if response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
				t.Fatalf("owner did not reach fail-closed handler: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHireAIRoutesRequireAuthenticationAndReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initialize := func(engine *gin.Engine) {
		initializeHireAIRoutes(engine.Group("/api/v1"), hireai.NewHandler(hireai.NewService(hireai.Config{}, nil)))
	}

	unauthenticated := gin.New()
	initialize(unauthenticated)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/hire-ai/status", ""},
		{http.MethodPost, "/api/v1/hire-ai/jobs/search", `{}`},
	} {
		if response := performRecoveredRequest(unauthenticated, route.method, route.path, route.body); response.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s status = %d, want %d", route.method, route.path, response.Code, http.StatusUnauthorized)
		}
	}

	viewer := recoveredRouteEngine("viewer")
	initialize(viewer)
	if response := performRecoveredRequest(viewer, http.MethodGet, "/api/v1/hire-ai/status", ""); response.Code != http.StatusOK {
		t.Fatalf("viewer status route returned %d: %s", response.Code, response.Body.String())
	}
	if response := performRecoveredRequest(viewer, http.MethodPost, "/api/v1/hire-ai/jobs/search", `{}`); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("read-authorized search did not reach disabled connector handler: %d %s", response.Code, response.Body.String())
	}
}

func TestRecoveredPlanningPreviewRoutesRequireWritePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unauthenticated := gin.New()
	initializeAutoGenCompatibilityRoutes(unauthenticated.Group("/api/v1"), autogencompat.NewHandler(autogencompat.DefaultService()))
	if response := performRecoveredRequest(unauthenticated, http.MethodGet, "/api/v1/autogen-compat/status", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated AutoGen compatibility status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	definitions := []struct {
		name       string
		path       string
		initialize func(*gin.RouterGroup)
	}{
		{
			name: "agent framework proposal", path: "/api/v1/agent-framework/proposals",
			initialize: func(group *gin.RouterGroup) {
				initializeAgentFrameworkRoutes(group, agentframework.NewHandler(agentframework.NewService(false, "", time.Second, nil)))
			},
		},
		{
			name: "autogen preview", path: "/api/v1/autogen-compat/preview",
			initialize: func(group *gin.RouterGroup) {
				initializeAutoGenCompatibilityRoutes(group, autogencompat.NewHandler(autogencompat.DefaultService()))
			},
		},
		{
			name: "autogen migration plan", path: "/api/v1/autogen-compat/migration-plan",
			initialize: func(group *gin.RouterGroup) {
				initializeAutoGenCompatibilityRoutes(group, autogencompat.NewHandler(autogencompat.DefaultService()))
			},
		},
		{
			name: "crewai proposal", path: "/api/v1/crewai/proposals",
			initialize: func(group *gin.RouterGroup) {
				initializeCrewAIRoutes(group, crewai.NewHandler(crewai.NewService(false, "", time.Second, nil)))
			},
		},
	}

	for _, definition := range definitions {
		t.Run(definition.name, func(t *testing.T) {
			viewer := recoveredRouteEngine("viewer")
			definition.initialize(viewer.Group("/api/v1"))
			if response := performRecoveredRequest(viewer, http.MethodPost, definition.path, ""); response.Code != http.StatusForbidden {
				t.Fatalf("viewer status = %d, want %d", response.Code, http.StatusForbidden)
			}

			operator := recoveredRouteEngine("operator")
			definition.initialize(operator.Group("/api/v1"))
			if response := performRecoveredRequest(operator, http.MethodPost, definition.path, ""); response.Code == http.StatusForbidden {
				t.Fatalf("operator with write permission did not reach bounded preview handler")
			}
		})
	}
}

func TestRecoveredMaintenanceAndMemoryRoutesRemainAuthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LLM_PROVIDERS_JSON", "")
	t.Setenv("LLM_POLICY_JSON", "")

	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	operator := recoveredRouteEngine("operator")
	initializeLLMRoutes(operator.Group("/api/v1"), llm.NewHandler(llmService))
	if response := performRecoveredRequest(operator, http.MethodPost, "/api/v1/llm/model-maintenance/run", ""); response.Code != http.StatusForbidden {
		t.Fatalf("operator model maintenance status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if response := performRecoveredRequest(operator, http.MethodPost, "/api/v1/llm/generate", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("operator generation status = %d, want %d", response.Code, http.StatusForbidden)
	}
	owner := recoveredRouteEngine("owner")
	initializeLLMRoutes(owner.Group("/api/v1"), llm.NewHandler(llmService))
	if response := performRecoveredRequest(owner, http.MethodPost, "/api/v1/llm/model-maintenance/run", ""); response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
		t.Fatalf("owner did not reach model maintenance handler: %d %s", response.Code, response.Body.String())
	}

	viewer := recoveredRouteEngine("viewer")
	initializeBrainCatalogRoutes(viewer.Group("/api/v1"), braincatalog.NewHandler())
	if response := performRecoveredRequest(viewer, http.MethodGet, "/api/v1/brain-catalog/revalidation-history", ""); response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
		t.Fatalf("viewer could not reach read-only catalog history: %d", response.Code)
	}
	if response := performRecoveredRequest(viewer, http.MethodPost, "/api/v1/brain-catalog/revalidation/run", ""); response.Code != http.StatusForbidden {
		t.Fatalf("viewer catalog maintenance status = %d, want %d", response.Code, http.StatusForbidden)
	}

	memoryEngine := recoveredRouteEngine("viewer")
	initializeMemoryRoutes(memoryEngine.Group("/api/v1"), memory.NewHandler(memory.NewService(emptyMemoryRepository{})))
	if response := performRecoveredRequest(memoryEngine, http.MethodGet, "/api/v1/memory/health", ""); response.Code != http.StatusOK {
		t.Fatalf("memory health status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	unauthenticatedMemory := gin.New()
	initializeMemoryRoutes(unauthenticatedMemory.Group("/api/v1"), memory.NewHandler(memory.NewService(emptyMemoryRepository{})))
	if response := performRecoveredRequest(unauthenticatedMemory, http.MethodGet, "/api/v1/memory/health", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated memory health status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func recoveredRouteEngine(role string) *gin.Engine {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, role+"@example.test")
		c.Set(contextRoleKey, role)
		c.Next()
	})
	return engine
}

func performRecoveredRequest(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(response, request)
	return response
}

type emptyMemoryRepository struct{}

func (emptyMemoryRepository) Create(record *models.ContextMemory) (*models.ContextMemory, error) {
	return record, nil
}
func (emptyMemoryRepository) Update(record *models.ContextMemory) (*models.ContextMemory, error) {
	return record, nil
}
func (emptyMemoryRepository) FindByID(uuid.UUID) (*models.ContextMemory, error) { return nil, nil }
func (emptyMemoryRepository) FindAll(string, bool) ([]models.ContextMemory, error) {
	return []models.ContextMemory{}, nil
}
func (emptyMemoryRepository) FindByHash(string, string, string) (*models.ContextMemory, error) {
	return nil, nil
}
func (emptyMemoryRepository) Delete(uuid.UUID) error { return nil }
