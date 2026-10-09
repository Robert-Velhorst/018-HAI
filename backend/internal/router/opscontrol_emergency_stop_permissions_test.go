package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/phase2"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type emergencyStopFanoutStub struct {
	calls int
}

func (f *emergencyStopFanoutStub) FanOutEmergencyStop(_ context.Context) (int, int, int, int, error) {
	f.calls++
	return 0, 0, 0, 0, nil
}

func TestGlobalEmergencyStopUsesVerifiedAdminRoleNotWorkerOwnerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSecret := config.AppConfig.JWTSecret
	config.AppConfig.JWTSecret = "global-stop-test-secret"
	t.Cleanup(func() { config.AppConfig.JWTSecret = previousSecret })
	t.Setenv("HAI_PHASE2_OWNER", "local-operator")
	workerOwner := phase2.ConfigFromEnv().OwnerUserID

	newEngine := func(t *testing.T) (*gin.Engine, *opscontrol.Service, *emergencyStopFanoutStub, string) {
		t.Helper()
		stateDir := t.TempDir()
		service := opscontrol.NewService(stateDir, nil, nil, workerOwner, "local")
		fanout := &emergencyStopFanoutStub{}
		service.WithOpenClawEmergencyStopFanout(fanout)
		engine := gin.New()
		engine.Use(identityMiddleware())
		api := engine.Group("/api/v1")
		initializeOpsControlRoutes(api, opscontrol.NewHandler(service))
		return engine, service, fanout, stateDir
	}
	request := func(engine *gin.Engine, role, principal string) *httptest.ResponseRecorder {
		t.Helper()
		now := time.Now()
		token := identity.SignToken(identity.Claims{
			UserID: principal, Role: role, Issuer: "hai-idp", Audience: "hai",
			TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix(),
		}, "global-stop-test-secret")
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/background/pause", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	t.Run("configured installation owner can engage global stop", func(t *testing.T) {
		engine, service, fanout, _ := newEngine(t)
		ownerID := workerOwner
		response := request(engine, "owner", ownerID)
		if response.Code != http.StatusOK {
			t.Fatalf("owner pause status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		state := service.Control().EmergencyState()
		if !state.Engaged || state.Actor != ownerID {
			t.Fatalf("global stop state = %#v, want engaged with UUID audit actor %q", state, ownerID)
		}
		if fanout.calls != 1 {
			t.Fatalf("fan-out calls = %d, want 1", fanout.calls)
		}
	})

	t.Run("another owner identity is rejected before persistence and fan-out", func(t *testing.T) {
		engine, service, fanout, stateDir := newEngine(t)
		response := request(engine, "owner", uuid.NewString())
		if response.Code != http.StatusForbidden {
			t.Fatalf("non-installation owner pause status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
		}
		state := service.Control().EmergencyState()
		if state.Engaged || state.Revision != 0 {
			t.Fatalf("non-installation owner request mutated global stop: %#v", state)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "emergency_stop.json")); !os.IsNotExist(err) {
			t.Fatalf("non-installation owner request persisted emergency-stop state: stat error = %v", err)
		}
		if fanout.calls != 0 {
			t.Fatalf("non-installation owner request triggered %d fan-out calls", fanout.calls)
		}
	})

	t.Run("operator is rejected before persistence and fan-out", func(t *testing.T) {
		engine, service, fanout, stateDir := newEngine(t)
		response := request(engine, "operator", uuid.NewString())
		if response.Code != http.StatusForbidden {
			t.Fatalf("operator pause status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
		}
		state := service.Control().EmergencyState()
		if state.Engaged || state.Revision != 0 {
			t.Fatalf("operator request mutated global stop before authorization: %#v", state)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "emergency_stop.json")); !os.IsNotExist(err) {
			t.Fatalf("operator request persisted emergency-stop state before authorization: stat error = %v", err)
		}
		if fanout.calls != 0 {
			t.Fatalf("operator request triggered %d fan-out calls before authorization", fanout.calls)
		}
	})
}
