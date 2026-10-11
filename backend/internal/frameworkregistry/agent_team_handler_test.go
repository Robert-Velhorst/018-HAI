package frameworkregistry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestRegisterAgentTeamRoutesRefusesMissingSecurityGuards(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	err := RegisterAgentTeamRoutes(
		engine.Group("/api/v1/framework-registry"),
		NewAgentTeamHandler(NewAgentTeamService(NewMemoryAgentTeamRepository())),
		AgentTeamRouteGuards{},
	)
	if err == nil {
		t.Fatal("unguarded agent team routes were registered")
	}
}

func TestAgentTeamHTTPRoutesAreOwnerScopedAndPermissionGated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("http"))
	engine := gin.New()
	err := RegisterAgentTeamRoutes(
		engine.Group("/api/v1/framework-registry"),
		NewAgentTeamHandler(service),
		testAgentTeamRouteGuards(),
	)
	if err != nil {
		t.Fatalf("RegisterAgentTeamRoutes: %v", err)
	}
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET /api/v1/framework-registry/teams",
		"GET /api/v1/framework-registry/teams/message-attention",
		"POST /api/v1/framework-registry/teams",
		"POST /api/v1/framework-registry/teams/guided",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/messages",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/decision-messages",
		"GET /api/v1/framework-registry/teams/:id/versions/:version/decision-overview",
		"GET /api/v1/framework-registry/teams/:id/versions/:version/message-attention",
		"GET /api/v1/framework-registry/teams/:id/versions/:version/messages/:messageId/acknowledgments",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/messages/:messageId/acknowledgments",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/messages/:messageId/acknowledgments/guided",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/delegations/assess",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/consensus",
		"POST /api/v1/framework-registry/teams/:id/versions/:version/revoke",
	} {
		if !registered[expected] {
			t.Fatalf("required route not registered: %s", expected)
		}
	}

	requestPayload, err := json.Marshal(testTeamRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	create := performAgentTeamRequest(engine, http.MethodPost, "/api/v1/framework-registry/teams", requestPayload, "robert", "owner")
	if create.Code != http.StatusCreated {
		t.Fatalf("owner create status = %d: %s", create.Code, create.Body.String())
	}
	var team AgentTeamContract
	if err := json.Unmarshal(create.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	if team.ID == "" || !team.AdvisoryOnly || team.GrantsExecutionAuthority || !team.ExecutionAuthorizationRequired {
		t.Fatalf("unsafe or invalid HTTP team response: %#v", team)
	}
	decisionOverview := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams/"+team.ID+"/versions/"+team.Version+"/decision-overview", nil, "robert", "viewer")
	if decisionOverview.Code != http.StatusOK || !bytes.Contains(decisionOverview.Body.Bytes(), []byte(`"messages":[]`)) || !bytes.Contains(decisionOverview.Body.Bytes(), []byte(`"attention":[]`)) {
		t.Fatalf("viewer decision overview status = %d: %s", decisionOverview.Code, decisionOverview.Body.String())
	}

	attentionIndex := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams/message-attention", nil, "robert", "viewer")
	if attentionIndex.Code != http.StatusOK || !bytes.Contains(attentionIndex.Body.Bytes(), []byte(`"teamId":"`+team.ID+`"`)) {
		t.Fatalf("viewer attention index status = %d: %s", attentionIndex.Code, attentionIndex.Body.String())
	}
	otherAttentionIndex := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams/message-attention", nil, "alice", "viewer")
	if otherAttentionIndex.Code != http.StatusOK || bytes.Contains(otherAttentionIndex.Body.Bytes(), []byte(team.ID)) {
		t.Fatalf("cross-owner attention index status = %d: %s", otherAttentionIndex.Code, otherAttentionIndex.Body.String())
	}

	viewerList := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams", nil, "robert", "viewer")
	if viewerList.Code != http.StatusOK || !bytes.Contains(viewerList.Body.Bytes(), []byte(team.ID)) {
		t.Fatalf("viewer list status = %d: %s", viewerList.Code, viewerList.Body.String())
	}
	operatorCreate := performAgentTeamRequest(engine, http.MethodPost, "/api/v1/framework-registry/teams", requestPayload, "robert", "operator")
	if operatorCreate.Code != http.StatusForbidden {
		t.Fatalf("operator create status = %d", operatorCreate.Code)
	}
	unknownRole := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams", nil, "robert", "unknown")
	if unknownRole.Code != http.StatusForbidden {
		t.Fatalf("unknown role status = %d", unknownRole.Code)
	}
	missingIdentity := performAgentTeamRequest(engine, http.MethodGet, "/api/v1/framework-registry/teams", nil, "", "viewer")
	if missingIdentity.Code != http.StatusUnauthorized {
		t.Fatalf("missing identity status = %d", missingIdentity.Code)
	}
	otherOwner := performAgentTeamRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/teams/"+team.ID+"/versions/"+team.Version,
		nil,
		"alice",
		"owner",
	)
	if otherOwner.Code != http.StatusNotFound {
		t.Fatalf("cross-owner read status = %d: %s", otherOwner.Code, otherOwner.Body.String())
	}
}

func testAgentTeamRouteGuards() AgentTeamRouteGuards {
	authenticated := func(c *gin.Context) {
		subject := c.GetHeader("X-Test-Subject")
		if subject == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		c.Set(identity.ContextSubjectKey, subject)
		c.Next()
	}
	recognized := func(c *gin.Context) {
		switch c.GetHeader("X-Test-Role") {
		case "viewer", "operator", "owner":
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "recognized role required"})
		}
	}
	permission := func(allowed ...string) gin.HandlerFunc {
		return func(c *gin.Context) {
			role := c.GetHeader("X-Test-Role")
			for _, candidate := range allowed {
				if role == candidate {
					c.Next()
					return
				}
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		}
	}
	return AgentTeamRouteGuards{
		AuthenticatedOwner: authenticated,
		RecognizedRole:     recognized,
		Read:               permission("viewer", "operator", "owner"),
		Write:              permission("operator", "owner"),
		Govern:             permission("owner"),
	}
}

func performAgentTeamRequest(engine *gin.Engine, method, path string, body []byte, subject, role string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if subject != "" {
		request.Header.Set("X-Test-Subject", subject)
	}
	if role != "" {
		request.Header.Set("X-Test-Role", role)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}
