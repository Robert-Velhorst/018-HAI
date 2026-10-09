package autonomy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
)

type failingHandlerService struct {
	overviewErr error
	stressErr   error
}

func (s failingHandlerService) Overview() (*Overview, error) {
	return nil, s.overviewErr
}

func (s failingHandlerService) OverviewForOwner(_ string) (*Overview, error) {
	return nil, s.overviewErr
}

func (s failingHandlerService) RunStressSuite() (*models.AutonomyStressRun, []StressCaseResult, error) {
	return nil, nil, s.stressErr
}

type ownerCapturingService struct {
	failingHandlerService
	owner        string
	globalCalled bool
}

func (s *ownerCapturingService) Overview() (*Overview, error) {
	s.globalCalled = true
	return &Overview{}, nil
}

func (s *ownerCapturingService) OverviewForOwner(owner string) (*Overview, error) {
	s.owner = owner
	return &Overview{}, nil
}

func TestOverviewRequiresPrincipalAndNeverUsesGlobalTelemetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, principal := range []string{"", "   ", " robert ", "other-owner"} {
		t.Run(principal, func(t *testing.T) {
			service := &ownerCapturingService{}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, principal); c.Next() })
			router.GET("/overview", NewHandler(service).Overview)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/overview?owner=foreign", nil))
			want := http.StatusOK
			if strings.TrimSpace(principal) == "" {
				want = http.StatusUnauthorized
			}
			if recorder.Code != want || service.globalCalled || service.owner != strings.TrimSpace(principal) {
				t.Fatalf("status=%d owner=%q global=%v", recorder.Code, service.owner, service.globalCalled)
			}
		})
	}
}

func TestHandlerDoesNotExposeUnexpectedServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(failingHandlerService{overviewErr: errors.New(`postgres password=not-for-http at C:\\private`), stressErr: errors.New(`token=not-for-http`)})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "robert")
		c.Next()
	})
	router.GET("/overview", handler.Overview)
	router.POST("/stress", handler.Stress)

	for _, test := range []struct {
		name      string
		method    string
		path      string
		wantError string
	}{
		{name: "overview", method: http.MethodGet, path: "/overview", wantError: "autonomy overview is unavailable"},
		{name: "stress", method: http.MethodPost, path: "/stress", wantError: "autonomy stress suite could not be completed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
			}
			for _, forbidden := range []string{"password", "token", "not-for-http", "C:\\\\private"} {
				if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(forbidden)) {
					t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
				}
			}
			if !strings.Contains(recorder.Body.String(), test.wantError) {
				t.Fatalf("response lacks stable error %q: %s", test.wantError, recorder.Body.String())
			}
		})
	}
}
