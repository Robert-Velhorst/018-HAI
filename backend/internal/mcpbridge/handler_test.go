package mcpbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHandlerRequiresDedicatedBridgeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "raw workflow description must never cross the MCP boundary"
	provider := &dashboardProvider{dashboard: &workflow.WorkflowDashboard{
		Counts: map[string]int64{"ready": 1},
		ReadyItems: []models.WorkflowItem{{
			ID:           uuid.New(),
			Title:        "Review a local task",
			Description:  secret,
			CurrentState: workflow.StateReady,
		}},
	}}
	token := "12345678901234567890123456789012"
	handler := NewHandler(NewService(Config{
		Enabled: true,
		Token:   token,
		OwnerID: "robert@example.test",
	}, provider))

	for name, configure := range map[string]func(*http.Request){
		"missing token": func(*http.Request) {},
		"wrong bridge token": func(request *http.Request) {
			request.Header.Set(bridgeTokenHeader, "wrong-token")
		},
		"browser bearer token": func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp-agent/actionable", nil)
			configure(request)
			context.Request = request

			handler.Actionable(context)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
			}
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("unauthorized response leaked raw workflow content: %s", response.Body.String())
			}
		})
	}
}

func TestHandlerReturnsOnlyBoundedReadOnlyActionableSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "highly sensitive raw intake body"
	provider := &dashboardProvider{dashboard: &workflow.WorkflowDashboard{
		ReadyItems: []models.WorkflowItem{{
			ID:            uuid.New(),
			Title:         "Prepare local draft",
			Description:   secret,
			CurrentState:  workflow.StateReady,
			PriorityScore: 50,
			NextAction:    "review the draft",
		}},
	}}
	token := "12345678901234567890123456789012"
	handler := NewHandler(NewService(Config{
		Enabled: true,
		Token:   token,
		OwnerID: "robert@example.test",
	}, provider))

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp-agent/actionable?limit=1", nil)
	request.Header.Set(bridgeTokenHeader, token)
	context.Request = request

	handler.Actionable(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, secret) || strings.Contains(body, "Description") {
		t.Fatalf("read-only MCP response leaked raw workflow content: %s", body)
	}
	if !strings.Contains(body, "Prepare local draft") || !strings.Contains(body, "review the draft") {
		t.Fatalf("read-only MCP response omitted bounded actionable fields: %s", body)
	}
}

func TestHandlerRejectsUnboundedActionableLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "12345678901234567890123456789012"
	handler := NewHandler(NewService(Config{
		Enabled: true,
		Token:   token,
		OwnerID: "robert@example.test",
	}, &dashboardProvider{dashboard: &workflow.WorkflowDashboard{}}))

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp-agent/actionable?limit=9", nil)
	request.Header.Set(bridgeTokenHeader, token)
	context.Request = request

	handler.Actionable(context)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}
