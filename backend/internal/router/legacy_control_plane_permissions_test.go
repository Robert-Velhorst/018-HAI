package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/featureflags"
	"automation-hub-backend/internal/hardwareprofile"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/phase2"
	"automation-hub-backend/internal/privacyfilter"
	"automation-hub-backend/internal/runtimelab"

	"github.com/gin-gonic/gin"
)

const (
	legacyControlPlaneAPIKey    = "legacy-control-plane-test-key"
	legacyControlPlaneJWTSecret = "legacy-control-plane-test-jwt-secret"
)

func TestLegacyControlPlaneRejectsAPIKeyWithoutAuthenticatedOwner(t *testing.T) {
	engine := newLegacyControlPlanePermissionEngine(t)

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/operations"},
		{method: http.MethodPost, path: "/api/v1/background/run"},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals/not-a-uuid/decision", body: `{}`},
		{method: http.MethodGet, path: "/api/v1/account-feeds"},
		{method: http.MethodPost, path: "/api/v1/model-intelligence/profiles/missing/missing/benchmark"},
		{method: http.MethodPatch, path: "/api/v1/power/policy", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/privacy/scan", body: `{"content":"private"}`},
		{method: http.MethodPost, path: "/api/v1/runtime-lab/missing/self-test"},
		{method: http.MethodGet, path: "/api/v1/runtime-lab/feature-parity"},
		{method: http.MethodGet, path: "/api/v1/runtime-lab/capabilities"},
		{method: http.MethodGet, path: "/api/v1/system/info"},
		{method: http.MethodGet, path: "/api/v1/system/readiness"},
		{method: http.MethodGet, path: "/api/v1/flags"},
	} {
		recorder := performLegacyControlPlaneRequest(engine, test.method, test.path, test.body, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestLegacyControlPlaneViewerCanReadButCannotMutate(t *testing.T) {
	engine := newLegacyControlPlanePermissionEngine(t)

	for _, path := range []string{
		"/api/v1/operations",
		"/api/v1/model-intelligence/overview",
		"/api/v1/privacy/scans",
		"/api/v1/runtime-lab/feature-parity",
		"/api/v1/runtime-lab/capabilities",
		"/api/v1/runtime-lab/openclaw/feature-parity",
		"/api/v1/system/info",
		"/api/v1/system/readiness",
		"/api/v1/flags",
	} {
		recorder := performLegacyControlPlaneRequest(engine, http.MethodGet, path, "", "viewer")
		if recorder.Code != http.StatusOK {
			t.Errorf("viewer GET %s status = %d, want 200: %s", path, recorder.Code, recorder.Body.String())
		}
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/approve"},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/evidence-pack"},
		{method: http.MethodPost, path: "/api/v1/background/run"},
		{method: http.MethodPost, path: "/api/v1/account-feeds", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/account-feeds/sync-due"},
		{method: http.MethodPatch, path: "/api/v1/account-feeds/not-a-uuid", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/model-intelligence/profiles/missing/missing/benchmark"},
		{method: http.MethodDelete, path: "/api/v1/model-intelligence/cache/missing"},
		{method: http.MethodPatch, path: "/api/v1/model-intelligence/token-budgets", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/hardware/detect"},
		{method: http.MethodPatch, path: "/api/v1/hardware/profile", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/power/policy", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/privacy/scan", body: `{"content":"private"}`},
		{method: http.MethodPost, path: "/api/v1/background/pause", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals/not-a-uuid/decision", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/resume"},
		{method: http.MethodPatch, path: "/api/v1/background/mode", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/windows-runtime/recovery"},
		{method: http.MethodPost, path: "/api/v1/windows-runtime/emergency-stop/verify"},
		{method: http.MethodPost, path: "/api/v1/runtime-lab/missing/probe"},
		{method: http.MethodPost, path: "/api/v1/runtime-lab/missing/self-test"},
		{method: http.MethodGet, path: "/api/v1/system/support-bundle"},
	} {
		recorder := performLegacyControlPlaneRequest(engine, test.method, test.path, test.body, "viewer")
		if recorder.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s status = %d, want 403: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestLegacyControlPlaneOperatorGetsOperationalButNotAdministrativeAuthority(t *testing.T) {
	engine := newLegacyControlPlanePermissionEngine(t)

	for _, test := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodGet, path: "/api/v1/operations", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/approve", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/reject", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/block-similar", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/later", want: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/v1/operations/not-a-uuid/run", want: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/v1/background/run", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/account-feeds/sync-due", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/model-intelligence/profiles/missing/missing/benchmark", want: http.StatusNotFound},
		{method: http.MethodDelete, path: "/api/v1/model-intelligence/cache/missing", want: http.StatusNotFound},
		{method: http.MethodPost, path: "/api/v1/hardware/detect", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/privacy/scan", body: `{"content":"private"}`, want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/background/pause", body: `{}`, want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/windows-runtime/recovery", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/runtime-lab/missing/probe", want: http.StatusNotFound},
		{method: http.MethodPost, path: "/api/v1/runtime-lab/missing/self-test", want: http.StatusNotFound},
	} {
		recorder := performLegacyControlPlaneRequest(engine, test.method, test.path, test.body, "operator")
		if recorder.Code != test.want {
			t.Errorf("operator %s %s status = %d, want %d: %s", test.method, test.path, recorder.Code, test.want, recorder.Body.String())
		}
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/account-feeds", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/account-feeds/not-a-uuid", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/model-intelligence/token-budgets", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/hardware/profile", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/power/policy", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals/not-a-uuid/decision", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/resume"},
		{method: http.MethodPatch, path: "/api/v1/background/mode", body: `{}`},
		{method: http.MethodGet, path: "/api/v1/system/support-bundle"},
	} {
		recorder := performLegacyControlPlaneRequest(engine, test.method, test.path, test.body, "operator")
		if recorder.Code != http.StatusForbidden {
			t.Errorf("operator admin %s %s status = %d, want 403: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestLegacyControlPlaneOwnerCanReachAdministrativeHandlers(t *testing.T) {
	engine := newLegacyControlPlanePermissionEngine(t)

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/account-feeds", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/model-intelligence/token-budgets", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/hardware/profile", body: `{}`},
		{method: http.MethodPatch, path: "/api/v1/power/policy", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/control-approvals/not-a-uuid/decision", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/background/resume"},
		{method: http.MethodPatch, path: "/api/v1/background/mode", body: `{}`},
		{method: http.MethodGet, path: "/api/v1/system/support-bundle"},
	} {
		recorder := performLegacyControlPlaneRequest(engine, test.method, test.path, test.body, "owner")
		if recorder.Code == http.StatusUnauthorized || recorder.Code == http.StatusForbidden {
			t.Errorf("owner %s %s stopped at auth boundary with %d: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}

	for _, path := range []string{
		"/api/v1/operations/not-a-uuid/approve",
		"/api/v1/operations/not-a-uuid/reject",
		"/api/v1/operations/not-a-uuid/block-similar",
	} {
		recorder := performLegacyControlPlaneRequest(engine, http.MethodPost, path, "", "owner")
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("owner decision POST %s status = %d, want handler validation 400: %s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func newLegacyControlPlanePermissionEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	previousAPIKey := config.AppConfig.BackendAPIKey
	previousJWTSecret := config.AppConfig.JWTSecret
	config.AppConfig.BackendAPIKey = legacyControlPlaneAPIKey
	config.AppConfig.JWTSecret = legacyControlPlaneJWTSecret
	t.Cleanup(func() {
		config.AppConfig.BackendAPIKey = previousAPIKey
		config.AppConfig.JWTSecret = previousJWTSecret
	})

	root := t.TempDir()
	operationService := operations.NewService(operations.NewMemoryRepository())
	module := phase2.NewModule(operationService, phase2.Config{
		OwnerUserID:  "configured-owner",
		WorkspaceID:  "local",
		WorkspaceDir: filepath.Join(root, "workspace"),
		FeedsDir:     filepath.Join(root, "feeds"),
		StateDir:     filepath.Join(root, "state"),
	})
	privacyService := privacyfilter.NewService()
	feedRegistry := accountfeed.NewRegistry(operationService, privacyService, accountfeed.FetchOptions{
		FeedsRoot: filepath.Join(root, "feeds"),
	})
	modelService := modelintelligence.NewService(modelintelligence.NewRegistryFromEnv())

	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(backendAPIKeyMiddleware())
	api.Use(identityMiddleware())
	initializePhase2Routes(api, phase2.NewHandler(module))
	initializeAccountFeedRoutes(api, accountfeed.NewHandler(feedRegistry, "configured-owner", "local"))
	initializeModelIntelligenceRoutes(api, modelintelligence.NewHandler(modelService))
	initializeHardwareRoutes(api, hardwareprofile.NewHandler(hardwareprofile.NewService("configured-owner", "local")))
	initializePrivacyRoutes(api, privacyfilter.NewHandler(privacyService))
	initializeOpsControlRoutes(api, opscontrol.NewHandler(module.OpsControl()))
	initializeRuntimeLabRoutes(api, runtimelab.NewHandler(runtimelab.NewService(
		module.Broker(),
		operationService,
		"configured-owner",
		"local",
	)))
	initializeFeatureFlagRoutes(api, featureflags.New())
	initializeSystemRoutes(
		api,
		func() doctor.Report { return doctor.Report{} },
		func(context.Context) doctor.Report { return doctor.Report{} },
		func() map[string]int { return map[string]int{} },
	)
	return engine
}

func performLegacyControlPlaneRequest(engine *gin.Engine, method, path, body, role string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(backendAPIKeyHeader, legacyControlPlaneAPIKey)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		request.Header.Set("Authorization", "Bearer "+identity.SignToken(identity.Claims{
			Subject: role + "-user",
			Role:    role,
			Expiry:  time.Now().Add(time.Hour).Unix(),
		}, legacyControlPlaneJWTSecret))
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}
