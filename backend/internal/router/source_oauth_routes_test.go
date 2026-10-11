package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/source"

	"github.com/gin-gonic/gin"
)

func TestGoogleOAuthStartRequiresWritePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	initializeSourceRoutes(api, source.NewHandler(source.Service(nil)))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/sources/oauth/google/start?sourceId=00000000-0000-0000-0000-000000000001", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("anonymous OAuth start status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestGoogleOAuthCallbackCanReachSignedStateValidationWithoutSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	initializeSourceRoutes(api, source.NewHandler(source.Service(nil)))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/sources/oauth/google/callback?code=unused&state=invalid", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("sessionless callback with invalid state status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
