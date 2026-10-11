package automation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/rbac"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func credentialAutomation() *models.Automation {
	checked := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return &models.Automation{
		ID: uuid.New(), Name: "Credential automation", URLPath: "credential-automation",
		Host: "localhost", Port: 8080, Position: 7, LaunchType: "api",
		LaunchTarget:    "POST https://launch-user:launch-password@example.invalid/start?token=launch-token&mode=fast",
		PublicURL:       "https://public-user:public-password@example.invalid/app?api_key=public-key&view=table",
		LocalURL:        "http://local-user:local-password@localhost:8080/app?secret=local-secret&view=compact",
		HealthCheckURL:  "https://health-user:health-password@example.invalid/health?access_token=health-token&verbose=true",
		HealthCheckType: "http", HealthCheckIntervalSeconds: 60, ExpectedHTTPStatus: 202,
		RoutePath: "credential-automation", ServiceName: "fixture-service", DependencyNotes: "requires fixture",
		Status: "warning", LastCheckedAt: &checked, AverageLatencyMs: 42, ConsecutiveFailures: 1,
	}
}

func assertNoAutomationCredentials(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"launch-user", "launch-password", "launch-token", "public-user", "public-password", "public-key", "local-user", "local-password", "local-secret", "health-user", "health-password", "health-token", "history-secret", "history-password"} {
		if strings.Contains(value, secret) {
			t.Fatalf("public response contains credential %q: %s", secret, value)
		}
	}
}

func viewerAutomationRouter(service Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "viewer-other-owner")
		c.Set(identity.ContextRoleKey, string(rbac.RoleViewer))
		c.Next()
	}, RequireAuthenticatedOperator())
	handler := NewHandler(service)
	router.GET("/automations", handler.GetAll)
	router.GET("/automations/:id", handler.GetByID)
	router.GET("/automations/:id/diagnostics", handler.Diagnostics)
	return router
}

func TestPublicAutomationViewerGETsPreserveRawConfiguration(t *testing.T) {
	for _, endpoint := range []string{"list", "detail", "diagnostics"} {
		t.Run(endpoint, func(t *testing.T) {
			raw := credentialAutomation()
			original := *raw
			repo := newFakeAutomationRepo(raw)
			repo.healthEvents = []models.AutomationHealthEvent{{Target: raw.HealthCheckURL, Status: "warning", LatencyMs: 42, FailureReason: "password=history-password"}}
			repo.launchEvents = []models.AutomationLaunchEvent{{AutomationID: raw.ID, OwnerIdentity: "viewer-other-owner", Target: raw.LaunchTarget, Status: "completed", ExitCode: 202, Output: `{"access_token":"history-secret","ok":true}`, AuditEvents: []string{"token=history-secret"}, RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{Intent: "password=history-password"}}}
			originalHealth := repo.healthEvents[0]
			originalLaunch := repo.launchEvents[0]
			service := newTestService(repo, events.Publisher{})
			bindingBefore := automationActionDigest(raw, TaskLaunchRequest{OwnerIdentity: "alice"})
			path := "/automations"
			if endpoint != "list" {
				path += "/" + raw.ID.String()
			}
			if endpoint == "diagnostics" {
				path += "/diagnostics"
			}
			response := httptest.NewRecorder()
			viewerAutomationRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
			}
			assertNoAutomationCredentials(t, response.Body.String())
			if !reflect.DeepEqual(*repo.automation, original) || !reflect.DeepEqual(repo.healthEvents[0], originalHealth) || !reflect.DeepEqual(repo.launchEvents[0], originalLaunch) {
				t.Fatal("read projection mutated persisted configuration/history")
			}
			if bindingBefore != automationActionDigest(repo.automation, TaskLaunchRequest{OwnerIdentity: "alice"}) {
				t.Fatal("read projection changed approval binding")
			}
			for _, metadata := range []string{"POST https://example.invalid/start", "mode=fast", "localhost", "8080", "warning", "42"} {
				if !strings.Contains(response.Body.String(), metadata) {
					t.Fatalf("missing operational metadata %q: %s", metadata, response.Body.String())
				}
			}
		})
	}
}

func TestTrimOutputRedactsCompleteJSONBeforeTruncation(t *testing.T) {
	output := []byte(`{"nested":[{"access_token":"prefix-secret\"suffix-secret"}],"padding":"` + strings.Repeat("x", 5000) + `"}`)
	redacted := trimOutput(output, 80)
	if strings.Contains(redacted, "prefix-secret") || strings.Contains(redacted, "suffix-secret") || len(redacted) > 80 || !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("trimmed output leaked escaped JSON secret or exceeded bound: %q", redacted)
	}
}

func TestLaunchJSONOutputReturnedAndSavedWithoutSecrets(t *testing.T) {
	for _, padding := range []int{0, 5000, (1 << 20) + 1} {
		t.Run(strconv.Itoa(padding), func(t *testing.T) {
			payload := `{"nested":[{"access_token":"response-access\"escaped-tail","refresh_token":"response-refresh","password":"response-password","client_secret":"response-client"}],"ok":true,"padding":"` + strings.Repeat("x", padding) + `"}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("token") != "original-execution-token" {
					t.Error("execution did not receive original credential")
				}
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			raw := credentialAutomation()
			raw.LaunchTarget = "POST " + server.URL + "/start?token=original-execution-token"
			raw.ExpectedHTTPStatus = 200
			repo := newFakeAutomationRepo(raw)
			service := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, service, raw.ID, TaskLaunchRequest{})
			response := httptest.NewRecorder()
			viewerAutomationRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/automations", nil))
			if request.ApprovalProof.ActionDigest != automationActionDigest(repo.automation, request) {
				t.Fatal("public GET changed original action digest")
			}
			result, err := service.LaunchTask(raw.ID, request)
			if err != nil || result == nil || result.Status != "completed" || len(repo.launchEvents) != 1 {
				t.Fatalf("launch result=%#v err=%v events=%d", result, err, len(repo.launchEvents))
			}
			for _, output := range []string{result.Output, repo.launchEvents[0].Output} {
				for _, secret := range []string{"response-access", "escaped-tail", "response-refresh", "response-password", "response-client", "original-execution-token"} {
					if strings.Contains(output, secret) {
						t.Fatalf("returned/persisted output leaked %q", secret)
					}
				}
				if len(output) > 4096 || output == "" {
					t.Fatalf("unexpected output length %d", len(output))
				}
				if padding <= 5000 && !strings.Contains(output, `"ok":true`) {
					t.Fatal("redaction removed non-secret JSON metadata")
				}
			}
			if repo.automation.LaunchTarget != raw.LaunchTarget {
				t.Fatal("execution configuration changed")
			}
		})
	}
}

func TestPublicAutomationUpdateRejectsChangedMaskBeforeWrites(t *testing.T) {
	for _, field := range []string{"launchTarget", "publicUrl", "localUrl", "healthCheckUrl"} {
		t.Run(field, func(t *testing.T) {
			raw := credentialAutomation()
			original := *raw
			repo := newFakeAutomationRepo(raw)
			service := newTestService(repo, events.Publisher{})
			visible := *publicAutomation(raw)
			visible.Name = "Changed name"
			switch field {
			case "launchTarget":
				visible.LaunchTarget += "&mode=changed"
			case "publicUrl":
				visible.PublicURL = "https://other.invalid/app?api_key=[REDACTED]"
			case "localUrl":
				visible.LocalURL = "http://localhost/app?secret=%5BREDACTED%5D"
			case "healthCheckUrl":
				visible.HealthCheckURL = "https://example.invalid/health?access_token=arbitrary-[REDACTED]"
			}
			payload, _ := json.Marshal(visible)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.PATCH("/automations", NewHandler(service).Update)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/automations", bytes.NewReader(payload)))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unmasked replacement") {
				t.Fatalf("changed mask status=%d body=%s", response.Code, response.Body.String())
			}
			if repo.updateCalls != 0 || !reflect.DeepEqual(*repo.automation, original) {
				t.Fatal("rejected masked update changed persisted configuration")
			}
		})
	}
}

func TestPublicAutomationUpdateAcceptsUnmaskedReplacements(t *testing.T) {
	raw := credentialAutomation()
	repo := newFakeAutomationRepo(raw)
	service := newTestService(repo, events.Publisher{})
	incoming := *publicAutomation(raw)
	incoming.LaunchTarget = "POST https://new-user:new-password@example.invalid/new?token=new-token&mode=slow"
	incoming.PublicURL = "https://new-user:new-password@example.invalid/app?api_key=new-key"
	incoming.LocalURL = "http://new-user:new-password@localhost:8080/app?secret=new-secret"
	incoming.HealthCheckURL = "https://new-user:new-password@example.invalid/health?access_token=new-health-token"
	updated, err := service.Update(&incoming)
	if err != nil || updated == nil {
		t.Fatalf("replacement failed: %v", err)
	}
	if repo.automation.LaunchTarget != incoming.LaunchTarget || repo.automation.PublicURL != incoming.PublicURL || repo.automation.LocalURL != incoming.LocalURL || repo.automation.HealthCheckURL != incoming.HealthCheckURL {
		t.Fatal("complete replacement was not persisted verbatim")
	}
}

type rawWriteResponseService struct {
	Service
	raw *models.Automation
}

func (s rawWriteResponseService) Create(*models.Automation) (*models.Automation, error) {
	return s.raw, nil
}
func (s rawWriteResponseService) Update(*models.Automation) (*models.Automation, error) {
	return s.raw, nil
}

func TestPublicAutomationWriteResponsesDoNotExposeRawConfiguration(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			raw := credentialAutomation()
			original := *raw
			gin.SetMode(gin.TestMode)
			router := gin.New()
			handler := NewHandler(rawWriteResponseService{raw: raw})
			router.POST("/automations", handler.Create)
			router.PATCH("/automations", handler.Update)
			request := httptest.NewRequest(method, "/automations", bytes.NewBufferString(`{}`))
			if method == http.MethodPost {
				request = httptest.NewRequest(method, "/automations", strings.NewReader("name=fixture"))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusOK
			if method == http.MethodPost {
				want = http.StatusCreated
			}
			if response.Code != want {
				t.Fatalf("write response status=%d body=%s", response.Code, response.Body.String())
			}
			assertNoAutomationCredentials(t, response.Body.String())
			if !reflect.DeepEqual(*raw, original) {
				t.Fatal("write projection mutated service result")
			}
		})
	}
}

func TestPublicAutomationCreateRejectsMaskedCredentials(t *testing.T) {
	service := newTestService(newFakeAutomationRepo(credentialAutomation()), events.Publisher{})
	incoming := credentialAutomation()
	incoming.LaunchTarget = "POST https://example.invalid/start?token=%5BREDACTED%5D"
	if _, err := service.Create(incoming); err == nil {
		t.Fatal("create accepted masked execution configuration")
	}
}

func TestHealthCheckSanitizesPersistedAndReturnedFailure(t *testing.T) {
	raw := credentialAutomation()
	raw.HealthCheckURL = "http://health-user:health-password@127.0.0.1:1/health?access_token=health-token&verbose=true"
	repo := newFakeAutomationRepo(raw)
	service := newTestService(repo, events.Publisher{})
	result, err := service.RunHealthCheck(raw.ID)
	if err != nil || result == nil || len(repo.healthEvents) != 1 {
		t.Fatalf("health result=%#v err=%v events=%d", result, err, len(repo.healthEvents))
	}
	for _, text := range []string{result.FailureReason, repo.automation.LastFailureReason, repo.healthEvents[0].FailureReason, repo.healthEvents[0].Target} {
		assertNoAutomationCredentials(t, text)
	}
	if repo.automation.HealthCheckURL != raw.HealthCheckURL || !strings.Contains(repo.healthEvents[0].Target, "verbose=true") {
		t.Fatal("health redaction changed execution target or removed operational query")
	}
}

type referenceAutomationRepo struct{ *fakeAutomationRepo }

func (r referenceAutomationRepo) FindByID(uuid.UUID) (*models.Automation, error) {
	return r.automation, nil
}

func TestPublicDiagnosticsDefaultsDoNotMutateRepositoryPointer(t *testing.T) {
	raw := credentialAutomation()
	raw.LaunchTarget, raw.HealthCheckURL, raw.RoutePath = "", "", ""
	original := *raw
	repo := referenceAutomationRepo{newFakeAutomationRepo(raw)}
	service := NewService(repo, events.Publisher{})
	result, err := service.DiagnosticsForOwner(raw.ID, "viewer-other-owner")
	if err != nil || result == nil {
		t.Fatalf("diagnostics failed: %v", err)
	}
	if !reflect.DeepEqual(*repo.automation, original) {
		t.Fatal("diagnostic defaults mutated raw repository pointer")
	}
	payload, _ := json.Marshal(result)
	assertNoAutomationCredentials(t, string(payload))
}

func TestPublicAutomationEditorRoundTripPreservesCredentials(t *testing.T) {
	for _, userinfoOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "query-and-userinfo", true: "userinfo-only"}[userinfoOnly], func(t *testing.T) {
			raw := credentialAutomation()
			if userinfoOnly {
				raw.LaunchTarget = "POST https://launch-user:launch-password@example.invalid/start"
				raw.PublicURL = "https://public-user:public-password@example.invalid/app"
				raw.LocalURL = "http://local-user:local-password@localhost:8080/app"
				raw.HealthCheckURL = "https://health-user:health-password@example.invalid/health"
			}
			original := *raw
			repo := newFakeAutomationRepo(raw)
			service := newTestService(repo, events.Publisher{})
			router := viewerAutomationRouter(service)
			router.PATCH("/automations", NewHandler(service).Update)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/automations/"+raw.ID.String(), nil))
			var visible models.Automation
			if err := json.Unmarshal(response.Body.Bytes(), &visible); err != nil {
				t.Fatal(err)
			}
			visible.DependencyNotes = "edited operational notes"
			payload, _ := json.Marshal(visible)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/automations", bytes.NewReader(payload)))
			if response.Code != http.StatusOK {
				t.Fatalf("editor save status=%d body=%s", response.Code, response.Body.String())
			}
			assertNoAutomationCredentials(t, response.Body.String())
			stored := repo.automation
			if stored.LaunchTarget != original.LaunchTarget || stored.PublicURL != original.PublicURL || stored.LocalURL != original.LocalURL || stored.HealthCheckURL != original.HealthCheckURL || stored.DependencyNotes != visible.DependencyNotes || stored.Position != original.Position {
				t.Fatal("editor round trip lost credentials or non-secret edit")
			}
		})
	}
}
