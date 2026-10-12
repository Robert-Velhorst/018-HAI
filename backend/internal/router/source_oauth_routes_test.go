package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/source"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type oauthRouteServiceStub struct {
	source.Service
}

func (oauthRouteServiceStub) CompleteGoogleOAuth(context.Context, string, string) (uuid.UUID, error) {
	return uuid.Nil, errors.New("invalid OAuth state")
}

func TestGoogleOAuthRoutesRequireAuthorityToStartAndRejectInvalidCallbackState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	initializeSourceRoutes(api, source.NewHandler(oauthRouteServiceStub{}))

	startResponse := httptest.NewRecorder()
	engine.ServeHTTP(startResponse, httptest.NewRequest(http.MethodGet, "/api/v1/sources/oauth/google/start?sourceId=00000000-0000-0000-0000-000000000001", nil))
	if startResponse.Code != http.StatusForbidden {
		t.Fatalf("anonymous OAuth start status = %d, want %d: %s", startResponse.Code, http.StatusForbidden, startResponse.Body.String())
	}

	callbackResponse := httptest.NewRecorder()
	engine.ServeHTTP(callbackResponse, httptest.NewRequest(http.MethodGet, "/api/v1/sources/oauth/google/callback?code=missing&state=invalid", nil))
	if callbackResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid OAuth callback state status = %d, want %d: %s", callbackResponse.Code, http.StatusBadRequest, callbackResponse.Body.String())
	}
	if location := callbackResponse.Header().Get("Location"); location != "" {
		t.Fatalf("invalid OAuth callback redirected to %q", location)
	}
}
