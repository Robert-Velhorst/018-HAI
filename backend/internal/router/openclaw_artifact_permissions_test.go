package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/openclawreconcile"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestOpenClawArtifactRetentionRequiresOwnerAndWritePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role, owner string
		status      int
	}{
		{"owner", "", 401}, {"viewer", "alice", 403}, {"unknown", "alice", 403},
		{"operator", "alice", 503}, {"owner", "alice", 503},
	} {
		t.Run(tc.role+tc.owner, func(t *testing.T) {
			engine := gin.New()
			group := engine.Group("/api/v1")
			group.Use(func(c *gin.Context) {
				c.Set(identity.ContextSubjectKey, tc.owner)
				c.Set(identity.ContextRoleKey, tc.role)
			})
			initializeOpenClawArtifactRoutes(group, openclawreconcile.NewArtifactHandler(nil, nil))
			w := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/api/v1/openclaw-artifacts/"+uuid.NewString()+"/"+strings.Repeat("a", 64)+"/retain", nil)
			request.Header.Set("X-Role", "owner")
			engine.ServeHTTP(w, request)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestOpenClawArtifactForgetRequiresOwnerAndAdminPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role, owner string
		status      int
	}{
		{"owner", "", 401}, {"viewer", "alice", 403}, {"unknown", "alice", 403}, {"operator", "alice", 403}, {"owner", "alice", 503},
	} {
		t.Run(tc.role+tc.owner, func(t *testing.T) {
			engine := gin.New()
			group := engine.Group("/api/v1")
			group.Use(func(c *gin.Context) {
				c.Set(identity.ContextSubjectKey, tc.owner)
				c.Set(identity.ContextRoleKey, tc.role)
			})
			initializeOpenClawArtifactRoutes(group, openclawreconcile.NewArtifactHandler(nil, nil))
			request := httptest.NewRequest("DELETE", "/api/v1/openclaw-artifacts/"+uuid.NewString()+"/"+strings.Repeat("a", 64)+"/retained", nil)
			request.Header.Set("If-Match", `"`+strings.Repeat("b", 64)+`"`)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}

func TestOpenClawArtifactReadRoutesRequireOwnerScopeAndAllowReadOnlyRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct {
		name, method, path string
	}{
		{"metadata", "GET", "/api/v1/openclaw-artifacts/" + uuid.NewString()},
		{"download", "GET", "/api/v1/openclaw-artifacts/" + uuid.NewString() + "/" + strings.Repeat("a", 64) + "/download"},
	} {
		for _, tc := range []struct {
			role, owner string
			status      int
		}{
			{"owner", "", 401}, {"viewer", "alice", 503}, {"unknown", "alice", 503},
			{"operator", "alice", 503}, {"owner", "alice", 503},
		} {
			t.Run(route.name+"/"+tc.role+"/"+tc.owner, func(t *testing.T) {
				engine := gin.New()
				group := engine.Group("/api/v1")
				group.Use(func(c *gin.Context) {
					c.Set(identity.ContextSubjectKey, tc.owner)
					c.Set(identity.ContextRoleKey, tc.role)
				})
				initializeOpenClawArtifactRoutes(group, openclawreconcile.NewArtifactHandler(nil, nil))
				request := httptest.NewRequest(route.method, route.path, nil)
				request.Header.Set("X-Role", "owner")
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				if response.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
				}
			})
		}
	}
}
