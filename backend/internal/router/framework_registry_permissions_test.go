package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestFrameworkRegistryPermissionsSeparatePlanningFromPolicyAdministration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := frameworkregistry.NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	initializeFrameworkRegistryRoutes(engine.Group("/api/v1"), frameworkregistry.NewHandler(service))

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		role   string
		want   int
	}{
		{name: "viewer reads catalog", method: http.MethodGet, path: "/api/v1/framework-registry/frameworks", role: "viewer", want: http.StatusOK},
		{name: "viewer reads Constitution history", method: http.MethodGet, path: "/api/v1/framework-registry/constitution/history", role: "viewer", want: http.StatusOK},
		{name: "viewer cannot create selection audit", method: http.MethodPost, path: "/api/v1/framework-registry/select", body: `{"request":"Plan a safe internal task"}`, role: "viewer", want: http.StatusForbidden},
		{name: "operator may create planning selection", method: http.MethodPost, path: "/api/v1/framework-registry/select", body: `{"request":"Plan a safe internal task"}`, role: "operator", want: http.StatusOK},
		{name: "operator cannot change policy preference", method: http.MethodPatch, path: "/api/v1/framework-registry/frameworks/communication/preference", body: `{"state":"disabled"}`, role: "operator", want: http.StatusForbidden},
		{name: "owner may lower policy preference", method: http.MethodPatch, path: "/api/v1/framework-registry/frameworks/communication/preference", body: `{"state":"disabled"}`, role: "owner", want: http.StatusOK},
		{name: "operator cannot draft Constitution", method: http.MethodPost, path: "/api/v1/framework-registry/constitution/drafts", body: `{"baseVersion":1,"changeSummary":"Operator tried to change policy"}`, role: "operator", want: http.StatusForbidden},
		{name: "owner may create Constitution draft", method: http.MethodPost, path: "/api/v1/framework-registry/constitution/drafts", body: `{"baseVersion":1,"changeSummary":"Owner-reviewed policy clarification"}`, role: "owner", want: http.StatusCreated},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-Verified-Role", test.role)
			engine.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestFrameworkRegistryConstitutionActivationRequiresExactOwnerConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := frameworkregistry.NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	engine := gin.New()
	engine.Use(testIdentityMiddleware())
	initializeFrameworkRegistryRoutes(engine.Group("/api/v1"), frameworkregistry.NewHandler(service))

	draftRecorder := httptest.NewRecorder()
	draftRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/framework-registry/constitution/drafts",
		bytes.NewBufferString(`{"baseVersion":1,"changeSummary":"Owner-reviewed policy clarification"}`),
	)
	draftRequest.Header.Set("Content-Type", "application/json")
	draftRequest.Header.Set("X-Test-Verified-Role", "owner")
	engine.ServeHTTP(draftRecorder, draftRequest)
	if draftRecorder.Code != http.StatusCreated {
		t.Fatalf("draft status = %d: %s", draftRecorder.Code, draftRecorder.Body.String())
	}
	var draft frameworkregistry.Constitution
	if err := json.Unmarshal(draftRecorder.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}

	for _, test := range []struct {
		name         string
		role         string
		confirmation string
		want         int
	}{
		{name: "operator lacks policy authority", role: "operator", confirmation: "ACTIVATE CONSTITUTION", want: http.StatusForbidden},
		{name: "owner must type exact confirmation", role: "owner", confirmation: "activate", want: http.StatusBadRequest},
		{name: "owner confirmation is case sensitive", role: "owner", confirmation: "activate constitution", want: http.StatusBadRequest},
		{name: "owner confirmation rejects trailing whitespace", role: "owner", confirmation: "ACTIVATE CONSTITUTION ", want: http.StatusBadRequest},
		{name: "owner can explicitly activate", role: "owner", confirmation: "ACTIVATE CONSTITUTION", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body, _ := json.Marshal(frameworkregistry.ActivateConstitutionRequest{
				Confirmation: test.confirmation,
				ApprovalNote: "I reviewed and approve this Constitution version.",
			})
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/framework-registry/constitution/"+draft.ID+"/activate",
				bytes.NewReader(body),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-Verified-Role", test.role)
			engine.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestFrameworkRegistryUnknownAndForgedRolesRemainLeastPrivilege(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	fixedViewer := gin.New()
	fixedViewer.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "viewer")
		c.Next()
	})
	initializeFrameworkRegistryRoutes(
		fixedViewer.Group("/api/v1"),
		frameworkregistry.NewHandler(service),
	)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/framework-registry/frameworks/communication/preference",
		bytes.NewBufferString(`{"state":"disabled"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	for _, header := range []string{
		"X-Test-Verified-Role",
		"X-Role",
		"X-User-Role",
		"X-HAI-Role",
		"Role",
	} {
		request.Header.Set(header, "owner")
	}
	request.Header.Set("X-Owner-Identity", "bob")
	recorder := httptest.NewRecorder()
	fixedViewer.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("forged role status = %d, want %d: %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}

	dynamicIdentity := newFrameworkRegistrySecurityEngine(service)
	for _, role := range []string{"super-owner", "administrator", "root", "owner,viewer", ""} {
		t.Run("unknown role "+role, func(t *testing.T) {
			recorder := performFrameworkRegistryRouteRequest(
				dynamicIdentity,
				http.MethodPost,
				"/api/v1/framework-registry/select",
				`{"request":"Plan a safe internal task"}`,
				"alice",
				role,
			)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("unknown role %q status = %d, want %d: %s", role, recorder.Code, http.StatusForbidden, recorder.Body.String())
			}
		})
	}

	recorder = performFrameworkRegistryRouteRequest(
		dynamicIdentity,
		http.MethodGet,
		"/api/v1/framework-registry/frameworks",
		"",
		"alice",
		"super-owner",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unknown role did not fall back to viewer read: %d %s", recorder.Code, recorder.Body.String())
	}

	view, err := service.Get("alice", "communication")
	if err != nil {
		t.Fatalf("Get communication: %v", err)
	}
	if !view.Enabled {
		t.Fatal("forged or unknown role changed framework preference")
	}
}

func TestFrameworkRegistryRoutesKeepSelectionsPreferencesAndConstitutionsOwnerScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := frameworkregistry.NewMemoryRepository()
	service, err := frameworkregistry.NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	engine := newFrameworkRegistrySecurityEngine(service)
	const requestSecret = "alice-private-selection-secret"

	aliceSelection := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodPost,
		"/api/v1/framework-registry/select",
		`{"request":"Plan a source review with token=`+requestSecret+`","needsDocuments":true}`,
		"alice",
		"operator",
	)
	if aliceSelection.Code != http.StatusOK {
		t.Fatalf("Alice select status = %d: %s", aliceSelection.Code, aliceSelection.Body.String())
	}
	if strings.Contains(aliceSelection.Body.String(), requestSecret) {
		t.Fatalf("selection response leaked request secret: %s", aliceSelection.Body.String())
	}

	aliceSelections := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/selections",
		"",
		"alice",
		"viewer",
	)
	if aliceSelections.Code != http.StatusOK {
		t.Fatalf("Alice selections status = %d: %s", aliceSelections.Code, aliceSelections.Body.String())
	}
	var aliceSelectionBody struct {
		Selections []frameworkregistry.SelectionDecision `json:"selections"`
	}
	if err := json.Unmarshal(aliceSelections.Body.Bytes(), &aliceSelectionBody); err != nil {
		t.Fatalf("decode Alice selections: %v", err)
	}
	if len(aliceSelectionBody.Selections) != 1 {
		t.Fatalf("Alice selections = %#v", aliceSelectionBody.Selections)
	}
	selectionID := aliceSelectionBody.Selections[0].ID
	aliceExactSelection := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/selections/"+selectionID,
		"",
		"alice",
		"viewer",
	)
	if aliceExactSelection.Code != http.StatusOK {
		t.Fatalf("Alice exact selection status = %d: %s", aliceExactSelection.Code, aliceExactSelection.Body.String())
	}
	var exactSelection frameworkregistry.SelectionDecision
	if err := json.Unmarshal(aliceExactSelection.Body.Bytes(), &exactSelection); err != nil {
		t.Fatalf("decode Alice exact selection: %v", err)
	}
	if exactSelection.ID != selectionID {
		t.Fatalf("Alice exact selection id = %q, want %q", exactSelection.ID, selectionID)
	}
	bobExactSelection := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/selections/"+selectionID,
		"",
		"bob",
		"viewer",
	)
	if bobExactSelection.Code != http.StatusNotFound {
		t.Fatalf("Bob exact selection status = %d, want %d: %s", bobExactSelection.Code, http.StatusNotFound, bobExactSelection.Body.String())
	}
	if strings.Contains(aliceSelections.Body.String(), requestSecret) {
		t.Fatalf("selection audit response leaked raw request secret: %s", aliceSelections.Body.String())
	}

	bobSelections := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/selections?ownerIdentity=alice&owner=alice",
		"",
		"bob",
		"viewer",
	)
	if bobSelections.Code != http.StatusOK {
		t.Fatalf("Bob selections status = %d: %s", bobSelections.Code, bobSelections.Body.String())
	}
	var bobSelectionBody struct {
		Selections []frameworkregistry.SelectionDecision `json:"selections"`
	}
	if err := json.Unmarshal(bobSelections.Body.Bytes(), &bobSelectionBody); err != nil {
		t.Fatalf("decode Bob selections: %v", err)
	}
	if len(bobSelectionBody.Selections) != 0 {
		t.Fatalf("Alice selections leaked to Bob: %#v", bobSelectionBody.Selections)
	}

	alicePreference := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodPatch,
		"/api/v1/framework-registry/frameworks/communication/preference",
		`{"state":"disabled"}`,
		"alice",
		"owner",
	)
	if alicePreference.Code != http.StatusOK {
		t.Fatalf("Alice preference status = %d: %s", alicePreference.Code, alicePreference.Body.String())
	}
	bobFramework := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/frameworks/communication",
		"",
		"bob",
		"viewer",
	)
	if bobFramework.Code != http.StatusOK {
		t.Fatalf("Bob framework status = %d: %s", bobFramework.Code, bobFramework.Body.String())
	}
	var bobFrameworkView frameworkregistry.FrameworkView
	if err := json.Unmarshal(bobFramework.Body.Bytes(), &bobFrameworkView); err != nil {
		t.Fatalf("decode Bob framework: %v", err)
	}
	if !bobFrameworkView.Enabled {
		t.Fatalf("Alice preference leaked to Bob: %#v", bobFrameworkView)
	}

	aliceDraft := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodPost,
		"/api/v1/framework-registry/constitution/drafts",
		`{"baseVersion":1,"changeSummary":"Alice reviewed an owner-scoped clarification."}`,
		"alice",
		"owner",
	)
	if aliceDraft.Code != http.StatusCreated {
		t.Fatalf("Alice draft status = %d: %s", aliceDraft.Code, aliceDraft.Body.String())
	}
	var draft frameworkregistry.Constitution
	if err := json.Unmarshal(aliceDraft.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode Alice draft: %v", err)
	}

	bobActivation := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodPost,
		"/api/v1/framework-registry/constitution/"+draft.ID+"/activate",
		`{"confirmation":"ACTIVATE CONSTITUTION","approvalNote":"Bob cannot approve Alice policy."}`,
		"bob",
		"owner",
	)
	if bobActivation.Code != http.StatusNotFound {
		t.Fatalf("Bob activation status = %d, want %d: %s", bobActivation.Code, http.StatusNotFound, bobActivation.Body.String())
	}
	if strings.Contains(bobActivation.Body.String(), draft.ID) ||
		strings.Contains(strings.ToLower(bobActivation.Body.String()), "alice") {
		t.Fatalf("cross-owner activation leaked Alice record details: %s", bobActivation.Body.String())
	}

	aliceActivation := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodPost,
		"/api/v1/framework-registry/constitution/"+draft.ID+"/activate",
		`{"confirmation":"ACTIVATE CONSTITUTION","approvalNote":"Alice reviewed and approved this version."}`,
		"alice",
		"owner",
	)
	if aliceActivation.Code != http.StatusOK {
		t.Fatalf("Alice activation status = %d: %s", aliceActivation.Code, aliceActivation.Body.String())
	}

	aliceHistory := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/constitution/history?limit=1",
		"",
		"alice",
		"viewer",
	)
	if aliceHistory.Code != http.StatusOK {
		t.Fatalf("Alice Constitution history status = %d: %s", aliceHistory.Code, aliceHistory.Body.String())
	}
	var aliceHistoryBody frameworkregistry.ConstitutionHistoryPage
	if err := json.Unmarshal(aliceHistory.Body.Bytes(), &aliceHistoryBody); err != nil {
		t.Fatalf("decode Alice Constitution history: %v", err)
	}
	if len(aliceHistoryBody.History) != 1 ||
		aliceHistoryBody.History[0].ID != draft.ID ||
		aliceHistoryBody.History[0].ApprovedBy != "alice" {
		t.Fatalf("Alice Constitution history = %#v", aliceHistoryBody)
	}

	bobHistory := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/constitution/history?ownerIdentity=alice&owner=alice",
		"",
		"bob",
		"viewer",
	)
	if bobHistory.Code != http.StatusOK {
		t.Fatalf("Bob Constitution history status = %d: %s", bobHistory.Code, bobHistory.Body.String())
	}
	var bobHistoryBody frameworkregistry.ConstitutionHistoryPage
	if err := json.Unmarshal(bobHistory.Body.Bytes(), &bobHistoryBody); err != nil {
		t.Fatalf("decode Bob Constitution history: %v", err)
	}
	if len(bobHistoryBody.History) != 0 {
		t.Fatalf("Alice Constitution history leaked to Bob: %#v", bobHistoryBody.History)
	}

	bobConstitution := performFrameworkRegistryRouteRequest(
		engine,
		http.MethodGet,
		"/api/v1/framework-registry/constitution?ownerIdentity=alice",
		"",
		"bob",
		"viewer",
	)
	if bobConstitution.Code != http.StatusOK {
		t.Fatalf("Bob Constitution status = %d: %s", bobConstitution.Code, bobConstitution.Body.String())
	}
	var bobConstitutionBody struct {
		Constitution frameworkregistry.Constitution `json:"constitution"`
		Source       string                         `json:"source"`
	}
	if err := json.Unmarshal(bobConstitution.Body.Bytes(), &bobConstitutionBody); err != nil {
		t.Fatalf("decode Bob Constitution: %v", err)
	}
	if bobConstitutionBody.Constitution.Version != 1 ||
		bobConstitutionBody.Source != "builtin-robert-constitution-v1:v1" {
		t.Fatalf("Alice Constitution leaked to Bob: %#v", bobConstitutionBody)
	}
}

func TestFrameworkRegistryOwnerGateRejectsMissingOrMalformedSubjects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		switch c.GetHeader("X-Test-Subject-Type") {
		case "slice":
			c.Set(identity.ContextSubjectKey, []string{"alice"})
		case "spaces":
			c.Set(identity.ContextSubjectKey, "   ")
		}
		c.Set(identity.ContextRoleKey, "owner")
		c.Next()
	})
	initializeFrameworkRegistryRoutes(
		engine.Group("/api/v1"),
		frameworkregistry.NewHandler(service),
	)

	for _, subjectType := range []string{"missing", "slice", "spaces"} {
		t.Run(subjectType, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/framework-registry/frameworks",
				nil,
			)
			request.Header.Set("X-Test-Subject-Type", subjectType)
			request.Header.Set("X-Owner-Identity", "alice")
			engine.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("subject type %s status = %d, want %d: %s", subjectType, recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func newFrameworkRegistrySecurityEngine(
	service *frameworkregistry.Service,
) *gin.Engine {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if subject := c.GetHeader("X-Test-Verified-Subject"); subject != "" {
			c.Set(identity.ContextSubjectKey, subject)
		}
		if role := c.GetHeader("X-Test-Verified-Role"); role != "" {
			c.Set(identity.ContextRoleKey, role)
		}
		c.Next()
	})
	initializeFrameworkRegistryRoutes(
		engine.Group("/api/v1"),
		frameworkregistry.NewHandler(service),
	)
	return engine
}

func performFrameworkRegistryRouteRequest(
	engine *gin.Engine,
	method string,
	path string,
	body string,
	subject string,
	role string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if subject != "" {
		request.Header.Set("X-Test-Verified-Subject", subject)
	}
	if role != "" {
		request.Header.Set("X-Test-Verified-Role", role)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}
