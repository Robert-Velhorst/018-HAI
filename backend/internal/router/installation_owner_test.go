package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestInstallationOwnerGuardUsesVerifiedScopeWithoutReplacingSubject(t *testing.T) {
	previous := config.AppConfig.JWTSecret
	config.AppConfig.JWTSecret = "installation-owner-test-signing-secret"
	t.Cleanup(func() { config.AppConfig.JWTSecret = previous })
	for _, test := range []struct {
		name, expected, subject, role, scope string
		want                                 int
	}{
		{"signed first-run admin", "configured-owner", "admin-uuid", "owner", "configured-owner", 204},
		{"legacy configured identity", "configured-owner", "configured-owner", "owner", "", 204},
		{"other owner", "configured-owner", "other-uuid", "owner", "", 403},
		{"wrong installation", "configured-owner", "admin-uuid", "owner", "other-installation", 403},
		{"conflicting scope cannot fall back", "configured-owner", "configured-owner", "owner", "other-installation", 403},
		{"operator cannot claim scope", "configured-owner", "admin-uuid", "operator", "configured-owner", 403},
		{"operator cannot match legacy identity", "configured-owner", "configured-owner", "operator", "", 403},
		{"empty configuration", "", "admin-uuid", "owner", "configured-owner", 403},
		{"missing principal", "configured-owner", "", "owner", "configured-owner", 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(identityMiddleware(), requireAuthenticatedOwner(), requirePrincipal(test.expected))
			engine.POST("/control", func(c *gin.Context) {
				if got := c.GetString(contextSubjectKey); got != test.subject {
					t.Errorf("audit subject = %q, want original %q", got, test.subject)
				}
				c.Status(http.StatusNoContent)
			})
			now := time.Now()
			token := identity.SignToken(identity.Claims{
				Subject: test.subject, Role: test.role, OperatorScope: test.scope,
				Issuer: "hai-idp", Audience: "hai", TokenType: "access",
				IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix(),
			}, config.AppConfig.JWTSecret)
			request := httptest.NewRequest(http.MethodPost, "/control", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			// Client-selected scope must have no authority.
			request.Header.Set("X-HAI-Operator-Scope", "configured-owner")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

// Also runs in the bounded identity-only test target without compiling the
// entire runtime dependency graph. Full route integration is tested separately.
func TestEveryGlobalBackgroundMutationHasInstallationOwnerGuard(t *testing.T) {
	content, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		`bg.POST("/pause",`, `bg.POST("/resume/approval",`, `bg.POST("/resume",`,
		`bg.POST("/resume-approval",`, `bg.POST("/resume-approval/:id/approve-and-resume",`,
		`bg.POST("/control-approvals",`, `bg.POST("/control-approvals/:id/decision",`,
		`bg.POST("/mode/approval",`, `bg.PATCH("/mode",`,
	} {
		found := false
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), route) {
				found = true
				if !strings.Contains(line, "requirePermission(rbac.PermAdmin), installationOwner,") {
					t.Errorf("global mutation lacks role and installation guard: %s", route)
				}
			}
		}
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}
