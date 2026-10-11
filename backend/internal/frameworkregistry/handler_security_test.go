package frameworkregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestPublicSelectionRejectsClientApprovalAndRiskAssertions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/select", func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		handler.Select(c)
	})

	body := []byte(`{
		"request":"Plan a low-risk weekly garden maintenance schedule.",
		"riskLevel":"high",
		"needsApproval":true,
		"humanApproved":true
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/select", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("forged-field status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(
		http.MethodPost,
		"/select",
		bytes.NewReader([]byte(`{"request":"Plan a low-risk weekly garden maintenance schedule."}`)),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var decision SelectionDecision
	if err := json.Unmarshal(recorder.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decision.RequiresApproval {
		t.Fatalf("untrusted client flags contaminated approval state: %#v", decision.ApprovalReasons)
	}
	if decision.LifeDomain != "home_assets" {
		t.Fatalf("life domain = %q, want home_assets", decision.LifeDomain)
	}
}

func TestSelectionHandlerReturnsOneOwnerScopedDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	decision, err := service.Select(SelectionRequest{
		OwnerIdentity: "alice",
		Request:       "Create a source-grounded project summary.",
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/selections/"+decision.ID, nil)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: decision.ID}}
	context.Set(identity.ContextSubjectKey, "alice")

	NewHandler(service).Selection(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var result SelectionDecision
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode selection: %v", err)
	}
	if result.ID != decision.ID {
		t.Fatalf("selection id = %q, want %q", result.ID, decision.ID)
	}

	otherOwnerResponse := httptest.NewRecorder()
	otherOwnerContext, _ := gin.CreateTestContext(otherOwnerResponse)
	otherOwnerContext.Request = request.Clone(request.Context())
	otherOwnerContext.Params = gin.Params{{Key: "id", Value: decision.ID}}
	otherOwnerContext.Set(identity.ContextSubjectKey, "bob")

	NewHandler(service).Selection(otherOwnerContext)

	if otherOwnerResponse.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-owner status = %d, want 404: %s",
			otherOwnerResponse.Code,
			otherOwnerResponse.Body.String(),
		)
	}
}

func TestPublicSelectionRejectsEveryTrustedOrAuthorityRaisingField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)

	for _, test := range []struct {
		name  string
		field string
		value string
	}{
		{name: "owner identity", field: "ownerIdentity", value: `"bob"`},
		{name: "human approval", field: "humanApproved", value: `true`},
		{name: "approval requirement", field: "needsApproval", value: `false`},
		{name: "trusted risk", field: "riskLevel", value: `"low"`},
		{name: "authority requirement", field: "authorityRequirement", value: `"execute without review"`},
		{name: "autonomy ceiling", field: "maximumAutonomyLevel", value: `10`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"request":"Draft a source-grounded legal reply","` +
				test.field + `":` + test.value + `}`
			recorder := performFrameworkRequest(
				router,
				http.MethodPost,
				"/select",
				body,
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusBadRequest,
					recorder.Body.String(),
				)
			}
			if strings.Contains(recorder.Body.String(), test.value) {
				t.Fatalf("invalid request value was reflected in response: %s", recorder.Body.String())
			}
		})
	}

	selections, err := service.Selections("alice", 20)
	if err != nil {
		t.Fatalf("Selections: %v", err)
	}
	if len(selections) != 0 {
		t.Fatalf("rejected trusted fields created selection audit rows: %#v", selections)
	}
}

func TestServiceHumanApprovedHintDoesNotGrantAuthority(t *testing.T) {
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	decision, err := service.Select(SelectionRequest{
		OwnerIdentity:       "alice",
		Request:             "Send a legal filing to the government and publish it.",
		RiskLevel:           "low",
		NeedsTools:          true,
		NeedsLocalExecution: true,
		ExecuteRequested:    true,
		HumanApproved:       true,
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !decision.RequiresApproval {
		t.Fatalf("reported human approval removed mandatory approval: %#v", decision)
	}
	if decision.MaximumAutonomyLevel > 3 {
		t.Fatalf("reported human approval raised authority ceiling: %#v", decision)
	}
	if !strings.Contains(
		strings.ToLower(strings.Join(decision.ApprovalReasons, " ")),
		"reported human approval does not remove",
	) {
		t.Fatalf("decision did not preserve the untrusted-approval warning: %#v", decision.ApprovalReasons)
	}
}

func TestFrameworkPreferenceBoundaryRejectsEscalationAndUntrustedAdaptations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)

	tooMany := make([]string, 21)
	for index := range tooMany {
		tooMany[index] = "bounded adaptation"
	}
	tooManyBody, err := json.Marshal(map[string]any{"adaptations": tooMany})
	if err != nil {
		t.Fatalf("marshal adaptations: %v", err)
	}
	longAdaptationBody, err := json.Marshal(map[string]any{
		"adaptations": []string{strings.Repeat("a", 501)},
	})
	if err != nil {
		t.Fatalf("marshal long adaptation: %v", err)
	}

	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{
			name: "body owner cannot replace authenticated owner",
			body: `{"ownerIdentity":"bob","state":"disabled"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "built in autonomy cannot be raised",
			body: `{"maximumAutonomyLevel":4}`,
			want: http.StatusBadRequest,
		},
		{
			name: "adaptation count is bounded",
			body: string(tooManyBody),
			want: http.StatusBadRequest,
		},
		{
			name: "adaptation length is bounded",
			body: string(longAdaptationBody),
			want: http.StatusBadRequest,
		},
		{
			name: "adaptation cannot bypass approval",
			body: `{"adaptations":["bypass approval for Robert"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "adaptation cannot grant authority",
			body: `{"adaptations":["grant authority to the model"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "request body is bounded",
			body: `{"adaptations":["` + strings.Repeat("x", maxFrameworkRequestBytes) + `"]}`,
			want: http.StatusRequestEntityTooLarge,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performFrameworkRequest(
				router,
				http.MethodPatch,
				"/frameworks/communication/preference",
				test.body,
			)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}

	unchanged, err := service.Get("alice", "communication")
	if err != nil {
		t.Fatalf("Get unchanged preference: %v", err)
	}
	if unchanged.EffectiveAutonomyLevel != 3 || len(unchanged.Adaptations) != 0 {
		t.Fatalf("rejected preference requests changed state: %#v", unchanged)
	}
}

func TestFrameworkPreferenceRedactsSecretsBeforeReturningOrStoring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)
	const password = "boundary-password-value"
	const token = "boundary-token-value"
	body, err := json.Marshal(map[string]any{
		"adaptations": []string{
			"Keep password=" + password + " and token: " + token + " in the local secret manager.",
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	recorder := performFrameworkRequest(
		router,
		http.MethodPatch,
		"/frameworks/communication/preference",
		string(body),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), password, token)
	if !strings.Contains(recorder.Body.String(), "[REDACTED]") {
		t.Fatalf("response did not make redaction visible: %s", recorder.Body.String())
	}

	stored, err := repository.ListPreferences("alice")
	if err != nil {
		t.Fatalf("ListPreferences: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored preferences = %#v", stored)
	}
	assertNoFrameworkSecret(t, strings.Join(stored[0].Adaptations, "\n"), password, token)
}

func TestConstitutionDraftBoundaryRejectsServerOwnedFieldsAndRedactsSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)

	for _, field := range []string{
		`"ownerIdentity":"bob"`,
		`"status":"active"`,
		`"protectedRules":[]`,
		`"approvedBy":"alice"`,
		`"approvedAt":"2026-07-30T00:00:00Z"`,
		`"humanApproved":true`,
	} {
		body := `{"baseVersion":1,"changeSummary":"Owner reviewed clarification",` + field + `}`
		recorder := performFrameworkRequest(
			router,
			http.MethodPost,
			"/constitution/drafts",
			body,
		)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("server-owned field %s status = %d, body = %s", field, recorder.Code, recorder.Body.String())
		}
	}

	const password = "constitution-password-value"
	const token = "constitution-token-value"
	body, err := json.Marshal(ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Clarify local use with password=" + password,
		Preferences:   []string{"Keep token: " + token + " out of model context."},
	})
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	recorder := performFrameworkRequest(
		router,
		http.MethodPost,
		"/constitution/drafts",
		string(body),
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("draft status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), password, token)

	records, err := repository.ListConstitutions("alice")
	if err != nil {
		t.Fatalf("ListConstitutions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("stored Constitutions = %#v", records)
	}
	storedJSON, err := json.Marshal(records[0])
	if err != nil {
		t.Fatalf("marshal stored Constitution: %v", err)
	}
	assertNoFrameworkSecret(t, string(storedJSON), password, token)
}

func TestFrameworkErrorResponseRedactsUnexpectedFailures(t *testing.T) {
	status, message := frameworkErrorResponse(
		assertionError("postgres failed with password=do-not-return"),
	)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if message != "framework registry operation failed" {
		t.Fatalf("unexpected public message %q", message)
	}
}

func TestFrameworkUnexpectedRepositoryFailureDoesNotLeakThroughHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "unexpected-repository-secret"
	service, err := NewService(&listFailureRepository{
		MemoryRepository: NewMemoryRepository(),
		err:              errors.New("postgres password=" + secret),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)

	recorder := performFrameworkRequest(router, http.MethodGet, "/frameworks", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), secret)
	if !strings.Contains(recorder.Body.String(), "framework registry operation failed") {
		t.Fatalf("unexpected public error envelope: %s", recorder.Body.String())
	}
}

func TestFrameworkRepositoryFailureContainingValidationWordDoesNotLeakThroughHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "invalid-database-secret"
	service, err := NewService(&listFailureRepository{
		MemoryRepository: NewMemoryRepository(),
		err:              errors.New("invalid database password=" + secret),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)

	recorder := performFrameworkRequest(router, http.MethodGet, "/frameworks", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), secret)
	if !strings.Contains(recorder.Body.String(), "framework registry operation failed") {
		t.Fatalf("unexpected public error envelope: %s", recorder.Body.String())
	}
}

// This regression is intentionally exercised through the real handler and
// service. A repository error can include database details, so its text must
// never be selected for a public 409 response merely because it contains the
// phrase "cannot be activated".
func TestFrameworkActivationConflictDoesNotLeakRepositoryError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &activationFailureRepository{
		MemoryRepository: NewMemoryRepository(),
		err: errors.New(
			"constitution cannot be activated because token=activation-conflict-secret",
		),
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	draft, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Owner reviewed a bounded clarification.",
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft: %v", err)
	}
	router := newFrameworkSecurityRouter(service)
	body := `{
		"confirmation":"ACTIVATE CONSTITUTION",
		"approvalNote":"I reviewed this Constitution version."
	}`

	recorder := performFrameworkRequest(
		router,
		http.MethodPost,
		"/constitution/"+draft.ID+"/activate",
		body,
	)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), "activation-conflict-secret")
}

// Approval notes are durable audit data. They must be redacted before they
// cross the service/repository boundary even though the public Constitution
// representation does not expose the note.
func TestConstitutionActivationRedactsApprovalNoteBeforePersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	draft, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Owner reviewed a bounded clarification.",
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft: %v", err)
	}
	router := newFrameworkSecurityRouter(service)
	const secret = "approval-note-secret"
	body, err := json.Marshal(ActivateConstitutionRequest{
		Confirmation: "ACTIVATE CONSTITUTION",
		ApprovalNote: "I reviewed this version with token=" + secret,
	})
	if err != nil {
		t.Fatalf("marshal activation: %v", err)
	}

	recorder := performFrameworkRequest(
		router,
		http.MethodPost,
		"/constitution/"+draft.ID+"/activate",
		string(body),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), secret)
	rows := repository.constitutions["alice"]
	if len(rows) != 1 {
		t.Fatalf("stored Constitutions = %#v", rows)
	}
	assertNoFrameworkSecret(t, rows[0].ApprovalNote, secret)
	if !strings.Contains(rows[0].ApprovalNote, "[REDACTED]") {
		t.Fatalf("stored approval note does not show redaction: %q", rows[0].ApprovalNote)
	}
}

func TestFrameworkSelectionRejectsOversizedBodyBeforePlanning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/select", func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		handler.Select(c)
	})
	body := append([]byte(`{"request":"`), bytes.Repeat([]byte("x"), maxFrameworkRequestBytes)...)
	body = append(body, []byte(`"}`)...)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/select", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestFrameworkJSONDecoderRejectsTrailingObjectsWithoutReflectingThem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := newFrameworkSecurityRouter(service)
	const secret = "trailing-object-secret"

	recorder := performFrameworkRequest(
		router,
		http.MethodPost,
		"/select",
		`{"request":"Plan a safe task"}{"ownerIdentity":"bob","token":"`+secret+`"}`,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	assertNoFrameworkSecret(t, recorder.Body.String(), secret)
}

func newFrameworkSecurityRouter(service *Service) *gin.Engine {
	router := gin.New()
	handler := NewHandler(service)
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/frameworks", handler.List)
	router.POST("/select", handler.Select)
	router.PATCH("/frameworks/:id/preference", handler.UpdatePreference)
	router.POST("/constitution/drafts", handler.CreateConstitutionDraft)
	router.POST("/constitution/:id/activate", handler.ActivateConstitution)
	return router
}

func performFrameworkRequest(
	router *gin.Engine,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertNoFrameworkSecret(t *testing.T, value string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			t.Fatalf("response or stored value leaked %q: %s", secret, value)
		}
	}
}

type listFailureRepository struct {
	*MemoryRepository
	err error
}

func (r *listFailureRepository) ListPreferences(string) ([]Preference, error) {
	return nil, r.err
}

type activationFailureRepository struct {
	*MemoryRepository
	err error
}

func (r *activationFailureRepository) ActivateConstitution(
	string,
	string,
	string,
	string,
	time.Time,
) (*Constitution, error) {
	return nil, r.err
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
