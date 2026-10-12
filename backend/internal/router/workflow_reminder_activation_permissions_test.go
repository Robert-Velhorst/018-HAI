package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/workflow"

	"github.com/gin-gonic/gin"
)

func TestWorkflowReminderActivationPermissionsSeparatePreparationDecisionAndHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := workflow.NewHandler(nil)

	unauthenticated := gin.New()
	initializeWorkflowRoutes(unauthenticated.Group("/api/v1"), handler)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/workflow/reminder-activation-requests", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-proposals/not-a-uuid/activation-requests", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/decisions", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/delivery-authorizations", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/workflow/reminder-deliveries", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-deliveries/run-due", nil),
	} {
		recorder := httptest.NewRecorder()
		unauthenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s status = %d, want 401", request.Method, request.URL.Path, recorder.Code)
		}
	}

	authenticated := gin.New()
	authenticated.Use(testIdentityMiddleware())
	initializeWorkflowRoutes(authenticated.Group("/api/v1"), handler)

	for _, role := range []string{"viewer", "unknown"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-proposals/not-a-uuid/activation-requests", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s preparation status = %d, want 403", role, recorder.Code)
		}
	}
	for _, role := range []string{"operator", "owner"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-proposals/not-a-uuid/activation-requests", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s preparation status = %d, want handler validation 400", role, recorder.Code)
		}
	}

	for _, role := range []string{"viewer", "operator", "unknown"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/decisions", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s decision status = %d, want 403", role, recorder.Code)
		}
	}
	ownerDecision := httptest.NewRecorder()
	ownerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/decisions", nil)
	ownerRequest.Header.Set("X-Test-Verified-Role", "owner")
	authenticated.ServeHTTP(ownerDecision, ownerRequest)
	if ownerDecision.Code != http.StatusBadRequest {
		t.Errorf("owner decision status = %d, want handler validation 400", ownerDecision.Code)
	}
	for _, role := range []string{"viewer", "operator", "unknown"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/delivery-authorizations", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s delivery authorization status = %d, want 403", role, recorder.Code)
		}
	}
	ownerDelivery := httptest.NewRecorder()
	ownerDeliveryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-activation-requests/not-a-uuid/delivery-authorizations", nil)
	ownerDeliveryRequest.Header.Set("X-Test-Verified-Role", "owner")
	authenticated.ServeHTTP(ownerDelivery, ownerDeliveryRequest)
	if ownerDelivery.Code != http.StatusBadRequest {
		t.Errorf("owner delivery authorization status = %d, want handler validation 400", ownerDelivery.Code)
	}

	for _, role := range []string{"viewer", "operator", "owner"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/workflow/reminder-activation-requests", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("%s history status = %d, want capability boundary 503", role, recorder.Code)
		}
	}
	for _, role := range []string{"viewer", "operator", "owner"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/workflow/reminder-deliveries", nil)
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("%s delivery history status = %d, want capability boundary 503", role, recorder.Code)
		}
	}
	for _, role := range []string{"viewer", "unknown"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-deliveries/run-due", strings.NewReader("{}"))
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s delivery worker status = %d, want 403", role, recorder.Code)
		}
	}
	for _, role := range []string{"operator", "owner"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-deliveries/run-due", strings.NewReader("{}"))
		request.Header.Set("X-Test-Verified-Role", role)
		authenticated.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("%s delivery worker status = %d, want capability boundary 503", role, recorder.Code)
		}
		for _, body := range []string{"", "null", "{} {}", "{\"limit\":101}"} {
			invalid := httptest.NewRequest(http.MethodPost, "/api/v1/workflow/reminder-deliveries/run-due", strings.NewReader(body))
			invalid.Header.Set("X-Test-Verified-Role", role)
			response := httptest.NewRecorder()
			authenticated.ServeHTTP(response, invalid)
			if response.Code != http.StatusBadRequest {
				t.Errorf("%s invalid reminder request status = %d, want 400", role, response.Code)
			}
		}
	}
}
