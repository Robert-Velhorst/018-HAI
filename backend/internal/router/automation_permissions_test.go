package router

import (
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAutomationRoutesApplySignedRolePermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstID := uuid.New()
	secondID := uuid.New()

	newEngine := func(role string) *gin.Engine {
		engine := gin.New()
		v1 := engine.Group("/api/v1")
		v1.Use(func(c *gin.Context) {
			c.Set(identity.ContextSubjectKey, "robert")
			c.Set(identity.ContextRoleKey, role)
			c.Next()
		})
		if err := initializeAutomationsRoutes(v1, automation.NewHandler(automationRouteServiceStub{})); err != nil {
			t.Fatalf("initialize automation routes: %v", err)
		}
		return engine
	}

	operatorConfig := httptest.NewRecorder()
	newEngine("operator").ServeHTTP(operatorConfig, httptest.NewRequest(http.MethodPatch, "/api/v1/automation/swap/"+firstID.String()+"/"+secondID.String(), nil))
	if operatorConfig.Code != http.StatusForbidden {
		t.Fatalf("operator reorder status = %d, want %d: %s", operatorConfig.Code, http.StatusForbidden, operatorConfig.Body.String())
	}

	operatorLaunch := httptest.NewRecorder()
	newEngine("operator").ServeHTTP(operatorLaunch, httptest.NewRequest(http.MethodPost, "/api/v1/automation/"+firstID.String()+"/launch", nil))
	if operatorLaunch.Code != http.StatusOK {
		t.Fatalf("operator launch status = %d, want %d: %s", operatorLaunch.Code, http.StatusOK, operatorLaunch.Body.String())
	}

	operatorStop := httptest.NewRecorder()
	newEngine("operator").ServeHTTP(operatorStop, httptest.NewRequest(http.MethodPost, "/api/v1/automation/"+firstID.String()+"/stop-runtime", nil))
	if operatorStop.Code != http.StatusOK {
		t.Fatalf("operator user-scoped stop status = %d, want %d: %s", operatorStop.Code, http.StatusOK, operatorStop.Body.String())
	}

	viewerLaunch := httptest.NewRecorder()
	newEngine("viewer").ServeHTTP(viewerLaunch, httptest.NewRequest(http.MethodPost, "/api/v1/automation/"+firstID.String()+"/launch", nil))
	if viewerLaunch.Code != http.StatusForbidden {
		t.Fatalf("viewer launch status = %d, want %d: %s", viewerLaunch.Code, http.StatusForbidden, viewerLaunch.Body.String())
	}

	ownerConfig := httptest.NewRecorder()
	newEngine("owner").ServeHTTP(ownerConfig, httptest.NewRequest(http.MethodPatch, "/api/v1/automation/swap/"+firstID.String()+"/"+secondID.String(), nil))
	if ownerConfig.Code != http.StatusOK {
		t.Fatalf("owner reorder status = %d, want %d: %s", ownerConfig.Code, http.StatusOK, ownerConfig.Body.String())
	}
}

func TestAutomationRuntimeRoutesForwardVerifiedOwnerToLedger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	service := &ownerCapturingAutomationService{}
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "robert")
		c.Set(identity.ContextRoleKey, "owner")
		c.Next()
	})
	if err := initializeAutomationsRoutes(v1, automation.NewHandler(service)); err != nil {
		t.Fatalf("initialize automation routes: %v", err)
	}

	launch := httptest.NewRecorder()
	engine.ServeHTTP(launch, httptest.NewRequest(http.MethodPost, "/api/v1/automation/"+id.String()+"/launch", nil))
	if launch.Code != http.StatusOK || service.launchRequest.OwnerIdentity != "robert" {
		t.Fatalf("launch owner propagation = status %d request %#v", launch.Code, service.launchRequest)
	}

	stop := httptest.NewRecorder()
	stopRequest := httptest.NewRequest(http.MethodPost, "/api/v1/automation/"+id.String()+"/stop-runtime", nil)
	engine.ServeHTTP(stop, stopRequest)
	if stop.Code != http.StatusOK || service.stopOwnerIdentity != "robert" || service.stopContext != stopRequest.Context() {
		t.Fatalf("stop owner propagation = status %d owner %q", stop.Code, service.stopOwnerIdentity)
	}

	diagnostics := httptest.NewRecorder()
	engine.ServeHTTP(diagnostics, httptest.NewRequest(http.MethodGet, "/api/v1/automation/"+id.String()+"/diagnostics", nil))
	if diagnostics.Code != http.StatusOK || service.diagnosticsOwner != "robert" {
		t.Fatalf("diagnostics owner propagation = status %d owner %q: %s", diagnostics.Code, service.diagnosticsOwner, diagnostics.Body.String())
	}
}

type automationRouteServiceStub struct{}

type ownerCapturingAutomationService struct {
	automationRouteServiceStub
	launchRequest     automation.TaskLaunchRequest
	stopOwnerIdentity string
	stopContext       context.Context
	diagnosticsOwner  string
}

func (s *ownerCapturingAutomationService) LaunchTask(id uuid.UUID, request automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	s.launchRequest = request
	return &automation.LaunchResult{AutomationID: id, Status: "completed"}, nil
}

func (s *ownerCapturingAutomationService) StopRuntimeTaskForOwner(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	s.stopOwnerIdentity = ownerIdentity
	return &agentruntime.StopResult{RuntimeID: "openclaw", TaskID: id.String(), Status: "stopped", EvidenceURI: "automation-launch://" + uuid.NewString()}, nil
}

func (s *ownerCapturingAutomationService) StopRuntimeTaskForOwnerContext(ctx context.Context, id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	s.stopContext = ctx
	return s.StopRuntimeTaskForOwner(id, ownerIdentity)
}

func (s *ownerCapturingAutomationService) DiagnosticsForOwner(id uuid.UUID, ownerIdentity string) (*automation.DiagnosticResult, error) {
	s.diagnosticsOwner = ownerIdentity
	return &automation.DiagnosticResult{AutomationID: id}, nil
}

func (automationRouteServiceStub) FindByID(uuid.UUID) (*models.Automation, error) {
	return &models.Automation{}, nil
}
func (automationRouteServiceStub) Create(item *models.Automation) (*models.Automation, error) {
	return item, nil
}
func (automationRouteServiceStub) Update(item *models.Automation) (*models.Automation, error) {
	return item, nil
}
func (automationRouteServiceStub) Delete(uuid.UUID) error { return nil }
func (automationRouteServiceStub) FindAll() ([]*models.Automation, error) {
	return []*models.Automation{}, nil
}
func (automationRouteServiceStub) SwapOrder(uuid.UUID, uuid.UUID) error { return nil }
func (automationRouteServiceStub) RunHealthCheck(id uuid.UUID) (*automation.HealthResult, error) {
	return &automation.HealthResult{AutomationID: id, Status: "healthy"}, nil
}
func (automationRouteServiceStub) HealthSummary() (*automation.HealthSummary, error) {
	return &automation.HealthSummary{}, nil
}
func (automationRouteServiceStub) Launch(id uuid.UUID) (*automation.LaunchResult, error) {
	return &automation.LaunchResult{AutomationID: id, Status: "completed"}, nil
}
func (automationRouteServiceStub) LaunchTask(id uuid.UUID, _ automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	return &automation.LaunchResult{AutomationID: id, Status: "completed"}, nil
}
func (automationRouteServiceStub) PrepareWorkflowApprovalBinding(uuid.UUID, automation.TaskLaunchRequest) (string, error) {
	return "", nil
}
func (automationRouteServiceStub) StopRuntimeTask(id uuid.UUID) (*agentruntime.StopResult, error) {
	return &agentruntime.StopResult{TaskID: id.String(), Status: "stopped"}, nil
}
func (automationRouteServiceStub) StopRuntimeTaskForOwner(id uuid.UUID, _ string) (*agentruntime.StopResult, error) {
	return &agentruntime.StopResult{TaskID: id.String(), Status: "stopped"}, nil
}
func (automationRouteServiceStub) StopRuntimeTaskForOwnerContext(_ context.Context, id uuid.UUID, _ string) (*agentruntime.StopResult, error) {
	return &agentruntime.StopResult{RuntimeID: "openclaw", TaskID: id.String(), Status: "stopped", EvidenceURI: "automation-launch://" + uuid.NewString()}, nil
}
func (automationRouteServiceStub) Diagnostics(id uuid.UUID) (*automation.DiagnosticResult, error) {
	return &automation.DiagnosticResult{AutomationID: id}, nil
}
func (automationRouteServiceStub) DiagnosticsForOwner(id uuid.UUID, _ string) (*automation.DiagnosticResult, error) {
	return &automation.DiagnosticResult{AutomationID: id}, nil
}

var _ automation.Service = automationRouteServiceStub{}
