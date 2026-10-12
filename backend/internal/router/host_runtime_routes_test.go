package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/hostruntime"

	"github.com/gin-gonic/gin"
)

func TestHostRuntimeRoutesDoNotRequireBrowserIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousAPIKey := config.AppConfig.BackendAPIKey
	previousJWTSecret := config.AppConfig.JWTSecret
	t.Cleanup(func() {
		config.AppConfig.BackendAPIKey = previousAPIKey
		config.AppConfig.JWTSecret = previousJWTSecret
	})
	config.AppConfig.BackendAPIKey = "backend-shared-key"
	config.AppConfig.JWTSecret = "idp-secret-that-must-not-parse-bridge-token"
	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(backendAPIKeyMiddleware())
	initializeHostRuntimeRoutes(api, hostruntime.NewHandler(
		hostruntime.NewService(nil),
		hostruntime.Config{Enabled: true, Token: strings.Repeat("a", 32)},
	))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/host-runtime/leases", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	request.Header.Set(backendAPIKeyHeader, "backend-shared-key")
	engine.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusUnauthorized {
		t.Fatalf("bridge route unexpectedly required a browser identity: %s", recorder.Body.String())
	}
}
