package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type brainSkillsRouteStore struct {
	stateCalls            int
	stateOwners           []string
	setCalls              int
	setOwner              string
	setSkillID            string
	setEnabled            bool
	setCatalogFingerprint string
}

func (s *brainSkillsRouteStore) State(_ context.Context, owner string, _ brainskills.Skill) (brainskills.SelectionState, error) {
	s.stateCalls++
	s.stateOwners = append(s.stateOwners, owner)
	return brainskills.SelectionState{}, nil
}

func (s *brainSkillsRouteStore) Set(_ context.Context, owner string, skill brainskills.Skill, enabled bool, reviewedCatalogFingerprint string) (brainskills.SelectionState, error) {
	s.setCalls++
	s.setOwner = owner
	s.setSkillID = skill.ID
	s.setEnabled = enabled
	s.setCatalogFingerprint = reviewedCatalogFingerprint
	return brainskills.SelectionState{Enabled: enabled}, nil
}

func newBrainSkillsRouteTestEngine(store *brainSkillsRouteStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	initializeBrainSkillsRoutes(engine.Group("/api/v1"), brainskills.NewHandler(store))
	return engine
}

func TestBrainSkillsInventoryRequiresAuthenticatedReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &brainSkillsRouteStore{}
	unauthenticated := gin.New()
	initializeBrainSkillsRoutes(unauthenticated.Group("/api/v1"), brainskills.NewHandler(store))

	recorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/brain-skills/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing owner identity status = %d, want %d: %s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if store.stateCalls != 0 || store.setCalls != 0 {
		t.Fatalf("unauthenticated request reached store: state=%d set=%d", store.stateCalls, store.setCalls)
	}

	for _, role := range []string{"viewer", "operator", "owner"} {
		t.Run(role, func(t *testing.T) {
			store := &brainSkillsRouteStore{}
			engine := newBrainSkillsRouteTestEngine(store)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/brain-skills/", nil)
			request.Header.Set("X-Test-Verified-Role", role)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("GET status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if store.stateCalls == 0 {
				t.Fatal("authorized GET did not reach the owner-scoped store")
			}
			for _, owner := range store.stateOwners {
				if owner != "alice" {
					t.Fatalf("inventory store owner = %q, want authenticated subject alice", owner)
				}
			}
			if store.setCalls != 0 {
				t.Fatalf("GET unexpectedly changed consent: set calls = %d", store.setCalls)
			}
		})
	}
}

func TestBrainSkillsSelectionRequiresOwnerApprovalPermission(t *testing.T) {
	for _, test := range []struct {
		role string
		want int
	}{
		{role: "viewer", want: http.StatusForbidden},
		{role: "operator", want: http.StatusForbidden},
		{role: "owner", want: http.StatusOK},
	} {
		t.Run(test.role, func(t *testing.T) {
			store := &brainSkillsRouteStore{}
			engine := newBrainSkillsRouteTestEngine(store)
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/brain-skills/frontend-design/selection",
				strings.NewReader(brainSkillEnableBody(t, "frontend-design")),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-Verified-Role", test.role)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			if recorder.Code != test.want {
				t.Fatalf("PUT status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
			if test.role != "owner" {
				if store.setCalls != 0 || store.stateCalls != 0 {
					t.Fatalf("denied consent reached store: state=%d set=%d", store.stateCalls, store.setCalls)
				}
				return
			}
			if store.setCalls != 1 {
				t.Fatalf("owner consent store calls = %d, want 1", store.setCalls)
			}
			if store.setOwner != "alice" || store.setSkillID != "frontend-design" || !store.setEnabled || store.setCatalogFingerprint != brainskills.DefaultCatalog().Fingerprint() {
				t.Fatalf("owner consent was not scoped correctly: owner=%q skill=%q enabled=%t catalogFingerprint=%q", store.setOwner, store.setSkillID, store.setEnabled, store.setCatalogFingerprint)
			}
		})
	}
}

func TestBrainSkillsGuidancePreviewRequiresOwnerApprovalPermission(t *testing.T) {
	unauthenticated := gin.New()
	unauthStore := &brainSkillsRouteStore{}
	initializeBrainSkillsRoutes(unauthenticated.Group("/api/v1"), brainskills.NewHandler(unauthStore))
	unauthenticatedResponse := httptest.NewRecorder()
	unauthenticated.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodGet, "/api/v1/brain-skills/frontend-design/guidance-preview", nil))
	if unauthenticatedResponse.Code != http.StatusUnauthorized || unauthStore.stateCalls != 0 {
		t.Fatalf("unauthenticated preview status=%d store calls=%d", unauthenticatedResponse.Code, unauthStore.stateCalls)
	}

	for _, test := range []struct {
		role string
		want int
	}{
		{role: "viewer", want: http.StatusForbidden},
		{role: "operator", want: http.StatusForbidden},
		{role: "owner", want: http.StatusOK},
	} {
		t.Run(test.role, func(t *testing.T) {
			store := &brainSkillsRouteStore{}
			engine := newBrainSkillsRouteTestEngine(store)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/brain-skills/frontend-design/guidance-preview", nil)
			request.Header.Set("X-Test-Verified-Role", test.role)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("preview status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
			if test.role != "owner" {
				if store.stateCalls != 0 || store.setCalls != 0 {
					t.Fatalf("denied preview reached owner store: state=%d set=%d", store.stateCalls, store.setCalls)
				}
				return
			}
			if store.stateCalls != 1 || store.stateOwners[0] != "alice" {
				t.Fatalf("preview was not owner-scoped: calls=%d owners=%v", store.stateCalls, store.stateOwners)
			}
		})
	}
}

func brainSkillEnableBody(t *testing.T, id string) string {
	t.Helper()
	for _, skill := range brainskills.DefaultCatalog().List() {
		if skill.ID == id {
			return `{"enabled":true,"reviewedGuidanceSHA256":"` + skill.GuidanceSHA256 + `","reviewedCatalogFingerprint":"` + brainskills.DefaultCatalog().Fingerprint() + `"}`
		}
	}
	t.Fatalf("skill %q is absent from the catalog", id)
	return ""
}

func TestBrainSkillsSelectionRequiresAuthenticatedOwnerIdentity(t *testing.T) {
	store := &brainSkillsRouteStore{}
	engine := gin.New()
	initializeBrainSkillsRoutes(engine.Group("/api/v1"), brainskills.NewHandler(store))
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/brain-skills/frontend-design/selection",
		strings.NewReader(brainSkillEnableBody(t, "frontend-design")),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing owner identity status = %d, want %d: %s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if store.setCalls != 0 || store.stateCalls != 0 {
		t.Fatalf("unauthenticated request reached store: state=%d set=%d", store.stateCalls, store.setCalls)
	}
}

func TestBrainSkillsJWTRejectsWhitespaceSubjectInsteadOfAliasingOwner(t *testing.T) {
	previousSecret := config.AppConfig.JWTSecret
	previousRunMode := config.AppConfig.RunMode
	config.AppConfig.JWTSecret = "brain-skills-test-secret"
	config.AppConfig.RunMode = "demo"
	t.Cleanup(func() {
		config.AppConfig.JWTSecret = previousSecret
		config.AppConfig.RunMode = previousRunMode
	})

	store := &brainSkillsRouteStore{}
	engine := gin.New()
	engine.Use(identityMiddleware())
	initializeBrainSkillsRoutes(engine.Group("/api/v1"), brainskills.NewHandler(store))
	now := time.Now()
	for _, claims := range []identity.Claims{
		{Subject: " alice ", UserID: "alice", Role: "owner"},
		{Subject: " \t", UserID: "alice", Role: "owner"},
		{UserID: " alice ", Role: "owner"},
	} {
		claims.Issuer = "hai-idp"
		claims.Audience = "hai"
		claims.TokenType = "access"
		claims.IssuedAt = now.Unix()
		claims.Expiry = now.Add(time.Hour).Unix()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/brain-skills/", nil)
		request.Header.Set("Authorization", "Bearer "+identity.SignToken(claims, "brain-skills-test-secret"))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("malformed owner claim %+v status = %d, want %d: %s", claims, response.Code, http.StatusUnauthorized, response.Body.String())
		}
	}
	if store.stateCalls != 0 || store.setCalls != 0 {
		t.Fatalf("malformed owner claims reached selection store: state=%d set=%d", store.stateCalls, store.setCalls)
	}
}
