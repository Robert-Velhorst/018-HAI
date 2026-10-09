package source

import (
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/whispercpp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type sourceTranscriberStub struct {
	transcripts []whispercpp.Transcript
	err         error
	folder      string
}

type sourceDocumentExtractorStub struct {
	documents []docling.Document
	err       error
	folder    string
}

type sourceSyncErrorTestService struct {
	*service
	err error
}

func (s *sourceSyncErrorTestService) SyncContext(context.Context, uuid.UUID, ImportRequest) (*SyncResult, error) {
	return nil, s.err
}

func (s *sourceDocumentExtractorStub) Status() docling.Status {
	return docling.Status{Configured: true}
}
func (s *sourceDocumentExtractorStub) Probe(context.Context) (*docling.ProbeResult, error) {
	return &docling.ProbeResult{Reachable: true, Configured: true}, nil
}
func (s *sourceDocumentExtractorStub) Extract(_ context.Context, folder string) ([]docling.Document, error) {
	s.folder = folder
	return s.documents, s.err
}

func (s *sourceTranscriberStub) Status() whispercpp.Status { return whispercpp.Status{} }
func (s *sourceTranscriberStub) Probe(context.Context) (*whispercpp.ProbeResult, error) {
	return &whispercpp.ProbeResult{Reachable: true}, nil
}
func (s *sourceTranscriberStub) Transcribe(_ context.Context, folder string) ([]whispercpp.Transcript, error) {
	s.folder = folder
	return s.transcripts, s.err
}

func TestHandlerOnlyListsVisibleSourcesAndRejectsForeignControls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	service := NewService(repo, nil)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
	})
	router.GET("/sources", handler.Sources)
	router.POST("/sources/:id/pause", handler.Pause)

	listRequest := httptest.NewRequest(http.MethodGet, "/sources", nil)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", listResponse.Code, listResponse.Body.String())
	}
	var sources []models.ConnectedSource
	if err := json.Unmarshal(listResponse.Body.Bytes(), &sources); err != nil {
		t.Fatalf("decode sources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != aliceID {
		t.Fatalf("visible sources = %#v, want only Alice source", sources)
	}
	if repo.lastVisibleSourceOwner != "alice" {
		t.Fatalf("source list was not filtered by owner in the repository: %q", repo.lastVisibleSourceOwner)
	}

	foreignRequest := httptest.NewRequest(http.MethodPost, "/sources/"+bobID.String()+"/pause", nil)
	foreignResponse := httptest.NewRecorder()
	router.ServeHTTP(foreignResponse, foreignRequest)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("foreign pause status = %d, body=%s", foreignResponse.Code, foreignResponse.Body.String())
	}
	if repo.lastMutableSourceID != bobID || repo.lastMutableSourceOwner != "alice" {
		t.Fatalf("mutable source lookup = %s/%q, want exact foreign source/alice", repo.lastMutableSourceID, repo.lastMutableSourceOwner)
	}
}

func TestHandlerReportsTrelloProviderFailureAsRetryableGatewayError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "999")
		http.Error(w, "provider-detail-must-not-leak", http.StatusTooManyRequests)
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	handler := NewHandler(NewService(repo, &fakeSourceMemoryService{}))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/sync", handler.Sync)

	request := httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/sync", strings.NewReader(`{"mode":"incremental_sync"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("sync status = %d, body=%s; want 503 provider failure", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "provider-detail-must-not-leak") {
		t.Fatalf("sync response leaked provider body: %s", response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sync failure: %v", err)
	}
	if body["retryable"] != true {
		t.Fatalf("retryable = %#v, want true", body["retryable"])
	}
	if len(repo.jobs) != 1 || repo.jobs[0].Status != "failed" {
		t.Fatalf("sync history = %#v, want one failed provider attempt", repo.jobs)
	}
}

func TestHandlerSeparatesSyncTimeoutCancellationAndProviderRetryMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Local notes",
		Enabled: true, LocalOnly: true, Status: "active", SyncTarget: ".",
	})
	tests := []struct {
		name             string
		err              error
		wantStatus       int
		wantRetryable    *bool
		wantRetryAfter   string
		wantPublicPhrase string
	}{
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantRetryable: boolPointer(true), wantPublicPhrase: "timed out"},
		{name: "cancellation", err: context.Canceled, wantStatus: http.StatusServiceUnavailable, wantRetryable: boolPointer(true), wantPublicPhrase: "canceled"},
		{name: "typed bad input remains a client error", err: fmt.Errorf("%w: invalid sync request", ErrInvalidSyncRequest), wantStatus: http.StatusBadRequest, wantPublicPhrase: "invalid sync request"},
		{name: "internal error is not mislabeled as bad input", err: errors.New("secret database detail"), wantStatus: http.StatusInternalServerError, wantPublicPhrase: "sync failed"},
		{
			name: "provider-specific retry metadata", err: newProviderSyncErrorWithOptions("GitHub", http.StatusForbidden, errors.New("secret provider body"), providerSyncErrorOptions{
				RetryableOverride: boolPointer(true), RetryAfterHeader: "30",
			}), wantStatus: http.StatusServiceUnavailable, wantRetryable: boolPointer(true), wantRetryAfter: "30", wantPublicPhrase: "temporarily refused",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := NewService(repo, nil).(*service)
			handler := NewHandler(&sourceSyncErrorTestService{service: base, err: test.err})
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
			router.POST("/sources/:id/sync", handler.Sync)

			request := httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/sync", strings.NewReader(`{"mode":"incremental_sync"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("sync status = %d, body=%s; want %d", response.Code, response.Body.String(), test.wantStatus)
			}
			if got := response.Header().Get("Retry-After"); got != test.wantRetryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, test.wantRetryAfter)
			}
			if !strings.Contains(response.Body.String(), test.wantPublicPhrase) || strings.Contains(response.Body.String(), "secret provider body") {
				t.Fatalf("sync response = %q, want safe message containing %q", response.Body.String(), test.wantPublicPhrase)
			}
			if test.wantRetryable != nil {
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if body["retryable"] != *test.wantRetryable {
					t.Fatalf("retryable = %#v, want %v", body["retryable"], *test.wantRetryable)
				}
			}
			if strings.Contains(response.Body.String(), "secret database detail") {
				t.Fatalf("sync response leaked internal error: %s", response.Body.String())
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestHandlerListsOnlyVisibleConnectionHealthInOneBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	handler := NewHandler(NewService(newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", ConnectorKey: "local-folder", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", ConnectorKey: "local-folder", Enabled: true, Status: "active"},
	), nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/connection-health", handler.ConnectionHealths)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/connection-health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("connection health status = %d, body=%s", response.Code, response.Body.String())
	}
	var health []ConnectionHealth
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode connection health: %v", err)
	}
	if len(health) != 1 || health[0].SourceID != aliceID {
		t.Fatalf("visible connection health = %#v, want only Alice source", health)
	}
}

func TestHandlerLoadsOwnerScopedRecentActivityFromRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID, bobID := uuid.New(), uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	repo.jobs = []models.SourceSyncJob{{ID: uuid.New(), SourceID: aliceID}, {ID: uuid.New(), SourceID: bobID}}
	repo.auditLogs = []models.SourceAuditLog{{ID: uuid.New(), SourceID: aliceID}, {ID: uuid.New(), SourceID: bobID}}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/sync-jobs", handler.SyncJobs)
	router.GET("/sources/audit-logs", handler.AuditLogs)

	for _, path := range []string{"/sources/sync-jobs", "/sources/audit-logs"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, response.Code, response.Body.String())
		}
	}
	if len(repo.lastSyncJobSourceIDs) != 1 || repo.lastSyncJobSourceIDs[0] != aliceID {
		t.Fatalf("sync-job query source IDs = %#v, want only Alice source", repo.lastSyncJobSourceIDs)
	}
	if repo.lastSyncJobLimit != 100 {
		t.Fatalf("sync-job history limit = %d, want 100", repo.lastSyncJobLimit)
	}
	if len(repo.lastAuditLogSourceIDs) != 1 || repo.lastAuditLogSourceIDs[0] != aliceID {
		t.Fatalf("audit-log query source IDs = %#v, want only Alice source", repo.lastAuditLogSourceIDs)
	}
	if repo.lastAuditLogLimit != 100 {
		t.Fatalf("audit-log history limit = %d, want 100", repo.lastAuditLogLimit)
	}
}

func TestGoogleOAuthStartRejectsForeignSourceBeforeConfigurationLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	foreignID := uuid.New()
	handler := NewHandler(NewService(newFakeSourceRepo(
		&models.ConnectedSource{ID: foreignID, OwnerIdentity: "bob", ConnectorKey: gmailConnectorKey, Name: "Bob Gmail", Enabled: true, Status: "active"},
	), nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/oauth/google/start", handler.StartGoogleOAuth)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/oauth/google/start?sourceId="+foreignID.String(), nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign OAuth start status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestGoogleOAuthCallbackRequiresMatchingBrowserStateCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureGoogleOAuthHandlerState(t)
	handler := NewHandler(NewService(newFakeSourceRepo(), nil))
	router := gin.New()
	router.GET("/sources/oauth/google/callback", handler.GoogleOAuthCallback)
	validState, err := signState(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	tamperedState := validState + "x"
	expiredState := googleOAuthHandlerStateWithExpiry(t, time.Now().Add(-time.Minute))

	tests := []struct {
		name       string
		queryState string
		cookie     string
	}{
		{name: "missing state", cookie: validState},
		{name: "cookie mismatch", queryState: validState, cookie: "different-state"},
		{name: "tampered signed state", queryState: tamperedState, cookie: tamperedState},
		{name: "expired signed state", queryState: expiredState, cookie: expiredState},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/sources/oauth/google/callback?error=access_denied"
			if tc.queryState != "" {
				path += "&state=" + tc.queryState
			}
			request := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: googleOAuthStateCookieName, Value: tc.cookie})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("callback status=%d location=%q body=%q, want state rejection", response.Code, response.Header().Get("Location"), response.Body.String())
			}
			if response.Header().Get("Location") != "" {
				t.Fatalf("invalid state triggered redirect to %q", response.Header().Get("Location"))
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("invalid state cleared the pending cookie: %q", response.Header().Get("Set-Cookie"))
			}
		})
	}
}

func TestGoogleOAuthValidDenialRedirectsAndClearsStateCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureGoogleOAuthHandlerState(t)
	handler := NewHandler(NewService(newFakeSourceRepo(), nil))
	router := gin.New()
	router.GET("/sources/oauth/google/callback", handler.GoogleOAuthCallback)
	state, err := signState(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/sources/oauth/google/callback?error=access_denied&state="+state, nil)
	request.AddCookie(&http.Cookie{Name: googleOAuthStateCookieName, Value: state})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/connected-sources?oauth=denied" {
		t.Fatalf("valid denial status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	setCookie := response.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, googleOAuthStateCookieName+"=") || !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("valid denial did not clear its consumed state cookie: %q", setCookie)
	}
}

func configureGoogleOAuthHandlerState(t *testing.T) {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthStateSigningKey = "handler-test-oauth-state-key"
}

func googleOAuthHandlerStateWithExpiry(t *testing.T, expiry time.Time) string {
	t.Helper()
	payload := fmt.Sprintf("%s|%d|%s", uuid.NewString(), expiry.Unix(), uuid.NewString())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	secret, err := stateSecret()
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestGoogleOAuthStateFromAuthorizeURLRequiresState(t *testing.T) {
	state, err := googleOAuthStateFromAuthorizeURL("https://accounts.example.test/authorize?state=signed-state")
	if err != nil || state != "signed-state" {
		t.Fatalf("state = %q, err=%v", state, err)
	}
	if _, err := googleOAuthStateFromAuthorizeURL("https://accounts.example.test/authorize"); err == nil {
		t.Fatal("expected authorize URL without state to fail closed")
	}
}

func TestHandlerRunsDueSyncsOnlyForAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := NewService(newFakeSourceRepo(), nil)
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/sources/sync-due", handler.RunDueScheduledSyncs)

	unauthenticatedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodPost, "/sources/sync-due", nil))
	if unauthenticatedResponse.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d, want 403: %s", unauthenticatedResponse.Code, unauthenticatedResponse.Body.String())
	}

	authenticatedRouter := gin.New()
	authenticatedRouter.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
	})
	authenticatedRouter.POST("/sources/sync-due", handler.RunDueScheduledSyncs)
	authenticatedResponse := httptest.NewRecorder()
	authenticatedRouter.ServeHTTP(authenticatedResponse, httptest.NewRequest(http.MethodPost, "/sources/sync-due", nil))
	if authenticatedResponse.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200: %s", authenticatedResponse.Code, authenticatedResponse.Body.String())
	}
}

func TestHandlerListsOnlyOwnerScopedExtractionsFromRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: uuid.New(), SourceID: aliceID, Summary: "Alice private context"}); err != nil {
		t.Fatalf("SaveExtraction Alice: %v", err)
	}
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: uuid.New(), SourceID: bobID, Summary: "Bob private context"}); err != nil {
		t.Fatalf("SaveExtraction Bob: %v", err)
	}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
	})
	router.GET("/sources/extractions", handler.Extractions)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/extractions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("extractions status = %d, body=%s", response.Code, response.Body.String())
	}
	var extractions []models.SourceExtraction
	if err := json.Unmarshal(response.Body.Bytes(), &extractions); err != nil {
		t.Fatalf("decode extractions: %v", err)
	}
	if len(extractions) != 1 || extractions[0].SourceID != aliceID {
		t.Fatalf("visible extractions = %#v, want only Alice extraction", extractions)
	}
	if total := response.Header().Get("X-Total-Count"); total != "1" {
		t.Fatalf("X-Total-Count = %q, want 1", total)
	}
	for _, sourceID := range repo.lastExtractionSourceIDs {
		if sourceID == bobID {
			t.Fatalf("handler repository query included Bob's private source")
		}
	}
}

func TestHandlerRejectsInvalidExtractionPageLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(newFakeSourceRepo(), nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/extractions", handler.Extractions)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/extractions?limit=501", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestHandlerExtractionPatchSubmitsDurableIntentAndOwnerScopedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, extraction, service, _ := newExtractionCorrectionFixture(t, false)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.PATCH("/sources/extractions/:id", handler.UpdateExtraction)
	router.GET("/api/v1/sources/extraction-corrections/:id", handler.ExtractionCorrection)
	path := "/sources/extractions/" + extraction.ID.String()
	patch := `{"summary":"corrected summary"}`
	headers := map[string]string{
		"If-Match":        extraction.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"Idempotency-Key": "handler-correction-key-01",
	}
	makeRequest := func(method, requestPath, body string, requestHeaders map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, requestPath, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		for key, value := range requestHeaders {
			request.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	response := makeRequest(http.MethodPatch, path, patch, headers)
	if response.Code != http.StatusAccepted {
		t.Fatalf("PATCH status = %d, want 202: %s", response.Code, response.Body.String())
	}
	var view ExtractionCorrectionView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	location := "/api/v1/sources/extraction-corrections/" + view.ID
	if !view.IntentPersisted || view.PatchSaved || !view.RecoveryPending || view.ID == "" || response.Header().Get("Location") != location {
		t.Fatalf("accepted correction state = %#v, Location=%q", view, response.Header().Get("Location"))
	}
	if got := repo.extractions[extraction.ID].Summary; got != "before" {
		t.Fatalf("PATCH applied before worker execution: summary=%q", got)
	}
	retry := makeRequest(http.MethodPatch, path, patch, headers)
	if retry.Code != http.StatusAccepted || retry.Header().Get("Location") != location || len(repo.corrections) != 1 {
		t.Fatalf("idempotent resubmission created new work or changed response: code=%d location=%q intents=%d", retry.Code, retry.Header().Get("Location"), len(repo.corrections))
	}
	status := makeRequest(http.MethodGet, location, "", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200: %s", status.Code, status.Body.String())
	}
	if err := json.Unmarshal(status.Body.Bytes(), &view); err != nil || view.PatchSaved || !view.RecoveryPending {
		t.Fatalf("owner correction status = %#v, err=%v", view, err)
	}
	foreign := gin.New()
	foreign.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "bob") })
	foreign.GET("/api/v1/sources/extraction-corrections/:id", handler.ExtractionCorrection)
	foreignResponse := httptest.NewRecorder()
	foreign.ServeHTTP(foreignResponse, httptest.NewRequest(http.MethodGet, location, nil))
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("foreign owner status = %d, want concealed 404: %s", foreignResponse.Code, foreignResponse.Body.String())
	}
}

func TestHandlerExtractionCorrectionStatusReturns200ForEveryExistingState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	states := []string{
		models.SourceExtractionCorrectionPending,
		"running",
		models.SourceExtractionCorrectionCompleted,
		models.SourceExtractionCorrectionConflict,
		models.SourceExtractionCorrectionFailed,
	}
	for _, wantStatus := range states {
		t.Run(wantStatus, func(t *testing.T) {
			repo, extraction, service, _ := newExtractionCorrectionFixture(t, false)
			patchSummary := "saved correction"
			view := submitTestCorrection(t, service, extraction, ExtractionPatch{Summary: &patchSummary}, "status-contract-"+wantStatus)
			correctionID, err := uuid.Parse(view.ID)
			if err != nil {
				t.Fatalf("parse correction ID: %v", err)
			}
			correction := repo.corrections[correctionID]
			if correction == nil {
				t.Fatal("submitted correction was not persisted")
			}
			if wantStatus == "running" {
				repo.correctionJobs[correction.DurableJobID].Status = models.DurableJobRunning
			} else {
				correction.Status = wantStatus
			}

			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
			router.GET("/api/v1/sources/extraction-corrections/:id", NewHandler(service).ExtractionCorrection)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/sources/extraction-corrections/"+view.ID, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET status = %d, want 200 for existing %q correction: %s", response.Code, wantStatus, response.Body.String())
			}
			var got ExtractionCorrectionView
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode status response: %v", err)
			}
			if got.Status != wantStatus {
				t.Fatalf("GET JSON status = %q, want %q", got.Status, wantStatus)
			}
		})
	}
}

func TestHandlerRejectsEmptyExtractionPatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active",
	})
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(), Uncertain: true}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.PATCH("/sources/extractions/:id", NewHandler(NewService(repo, nil)).UpdateExtraction)
	request := httptest.NewRequest(http.MethodPatch, "/sources/extractions/"+extractionID.String(), strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", repo.extractions[extractionID].UpdatedAt.UTC().Format(time.RFC3339Nano))
	request.Header.Set("Idempotency-Key", "empty-patch-handler-key-01")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status = %d, body=%s", response.Code, response.Body.String())
	}
	if !repo.extractions[extractionID].Uncertain {
		t.Fatal("empty patch changed the uncertainty flag")
	}
}

func TestHandlerExtractionPatchRejectsStaleRevisionWithoutSavingIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, extraction, service, _ := newExtractionCorrectionFixture(t, false)
	current := repo.extractions[extraction.ID]
	current.Summary = "newer external value"
	current.UpdatedAt = extraction.UpdatedAt.Add(time.Second)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.PATCH("/sources/extractions/:id", NewHandler(service).UpdateExtraction)
	request := httptest.NewRequest(http.MethodPatch, "/sources/extractions/"+extraction.ID.String(), strings.NewReader(`{"summary":"stale correction"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", extraction.UpdatedAt.UTC().Format(time.RFC3339Nano))
	request.Header.Set("Idempotency-Key", "stale-patch-handler-key-01")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "source_extraction_revision_conflict") {
		t.Fatalf("stale PATCH = %d %s", response.Code, response.Body.String())
	}
	if len(repo.corrections) != 0 || current.Summary != "newer external value" {
		t.Fatalf("stale PATCH mutated data: corrections=%d summary=%q", len(repo.corrections), current.Summary)
	}
}

func TestHandlerBoundsExtractionHistoryAndReportsExactTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"})
	for index := 0; index < 3; index++ {
		if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: uuid.New(), SourceID: sourceID, Summary: "private context"}); err != nil {
			t.Fatalf("SaveExtraction: %v", err)
		}
	}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/extractions", handler.Extractions)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/extractions?limit=2", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var extractions []models.SourceExtraction
	if err := json.Unmarshal(response.Body.Bytes(), &extractions); err != nil {
		t.Fatalf("decode extractions: %v", err)
	}
	if len(extractions) != 2 {
		t.Fatalf("returned records = %d, want 2", len(extractions))
	}
	if total := response.Header().Get("X-Total-Count"); total != "3" {
		t.Fatalf("X-Total-Count = %q, want 3", total)
	}
}

func TestHandlerRejectsOwnerlessLegacySourceAndExtractionMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:      sourceID,
		Name:    "Legacy local source",
		Enabled: true,
		Status:  "active",
	})
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: extractionID, SourceID: sourceID, Summary: "Legacy context"}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
	})
	router.POST("/sources/:id/pause", handler.Pause)
	router.POST("/sources/extractions/:id/archive", handler.ArchiveExtraction)

	pauseResponse := httptest.NewRecorder()
	router.ServeHTTP(pauseResponse, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/pause", nil))
	if pauseResponse.Code != http.StatusNotFound {
		t.Fatalf("ownerless source pause status = %d, want 404: %s", pauseResponse.Code, pauseResponse.Body.String())
	}

	archiveResponse := httptest.NewRecorder()
	router.ServeHTTP(archiveResponse, httptest.NewRequest(http.MethodPost, "/sources/extractions/"+extractionID.String()+"/archive", nil))
	if archiveResponse.Code != http.StatusNotFound {
		t.Fatalf("ownerless extraction archive status = %d, want 404: %s", archiveResponse.Code, archiveResponse.Body.String())
	}
	if repo.lastMutableExtractionID != extractionID || repo.lastMutableExtractionOwner != "alice" {
		t.Fatalf("mutable extraction lookup = %s/%q, want exact extraction/alice", repo.lastMutableExtractionID, repo.lastMutableExtractionOwner)
	}

	storedSource, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if !storedSource.Enabled || storedSource.Status != "active" {
		t.Fatalf("ownerless source was mutated: %#v", storedSource)
	}
	storedExtraction, err := repo.FindExtraction(extractionID)
	if err != nil {
		t.Fatalf("FindExtraction: %v", err)
	}
	if storedExtraction.Archived {
		t.Fatalf("ownerless extraction was archived: %#v", storedExtraction)
	}
}

func TestHandlerTranscribesOnlyAnOwnedExplicitAudioSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "whisper-audio", Name: "Meeting notes", Category: "audio",
		Enabled: true, LocalOnly: true, Status: "active", SyncTarget: "voice-notes/2026-07", DefaultProjectKey: "Robert-life-os",
	})
	stub := &sourceTranscriberStub{transcripts: []whispercpp.Transcript{{Path: "voice-notes/2026-07/meeting.m4a", Text: "Follow up with the lawyer.", ModelID: "ggml-base.en.bin", Language: "en"}}}
	handler := NewHandler(NewService(repo, nil), stub)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/transcribe", handler.Transcribe)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/transcribe", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("transcribe status = %d, body=%s", response.Code, response.Body.String())
	}
	if stub.folder != "voice-notes/2026-07" {
		t.Fatalf("folder = %q", stub.folder)
	}
	if repo.lastMutableSourceID != sourceID || repo.lastMutableSourceOwner != "alice" {
		t.Fatalf("transcription must resolve the exact owned source, got id=%s owner=%q", repo.lastMutableSourceID, repo.lastMutableSourceOwner)
	}
	if len(repo.rawItems) != 1 {
		t.Fatalf("raw items = %#v", repo.rawItems)
	}
	var raw *models.SourceRawItem
	for _, item := range repo.rawItems {
		raw = item
	}
	if raw == nil || raw.SourceURI == "" || raw.ItemType != "audio_transcript" {
		t.Fatalf("raw item = %#v", raw)
	}
	if !strings.HasPrefix(raw.SourceURI, "audio://selected-source/") {
		t.Fatalf("source uri = %q", raw.SourceURI)
	}
}

func TestHandlerTranscriptionRejectsCallerPayloadAndNonAudioSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Files", Category: "local_folder", Enabled: true, LocalOnly: true, Status: "active", SyncTarget: "notes"})
	handler := NewHandler(NewService(repo, nil), &sourceTranscriberStub{})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/transcribe", handler.Transcribe)

	payloadResponse := httptest.NewRecorder()
	router.ServeHTTP(payloadResponse, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/transcribe", strings.NewReader(`{"path":"anywhere"}`)))
	if payloadResponse.Code != http.StatusBadRequest {
		t.Fatalf("payload status = %d, body=%s", payloadResponse.Code, payloadResponse.Body.String())
	}

	nonAudioResponse := httptest.NewRecorder()
	nonAudioRequest := httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/transcribe", nil)
	router.ServeHTTP(nonAudioResponse, nonAudioRequest)
	if nonAudioResponse.Code != http.StatusBadRequest {
		t.Fatalf("non-audio status = %d, body=%s", nonAudioResponse.Code, nonAudioResponse.Body.String())
	}
}

func TestHandlerExtractsOnlyAnOwnedExplicitDoclingSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: doclingDocumentsConnectorKey, Name: "Legal evidence", Category: "document",
		Enabled: true, LocalOnly: true, Status: "active", SyncTarget: "legal/vivare", DefaultProjectKey: "Vivare dispute",
	})
	stub := &sourceDocumentExtractorStub{documents: []docling.Document{{
		Path: "legal/vivare/evidence.docx", Text: "The hearing is scheduled for 9 September.", Format: "docx", PageCount: 2,
		ContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}}
	service := NewService(repo, nil)
	handler := NewHandlerWithDocumentExtractor(service, stub)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/extract-documents", handler.ExtractDocuments)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/extract-documents", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("extract status = %d, body=%s", response.Code, response.Body.String())
	}
	if stub.folder != "legal/vivare" {
		t.Fatalf("folder = %q", stub.folder)
	}
	if repo.lastMutableSourceID != sourceID || repo.lastMutableSourceOwner != "alice" {
		t.Fatalf("document extraction must resolve the exact owned source, got id=%s owner=%q", repo.lastMutableSourceID, repo.lastMutableSourceOwner)
	}
	if len(repo.rawItems) != 1 {
		t.Fatalf("raw items = %#v", repo.rawItems)
	}
	for _, raw := range repo.rawItems {
		if raw.ItemType != "document_extraction" || !strings.HasPrefix(raw.SourceURI, "document://selected-source/"+sourceID.String()+"/") {
			t.Fatalf("raw item = %#v", raw)
		}
	}
	if _, err := service.Sync(sourceID, ImportRequest{}); err == nil || !strings.Contains(err.Error(), "controlled document extraction route") {
		t.Fatalf("generic docling sync error = %v", err)
	}
}

func TestHandlerDocumentExtractionRejectsPayloadAndNonDoclingSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Files", Enabled: true, LocalOnly: true, Status: "active", SyncTarget: "notes"})
	handler := NewHandlerWithDocumentExtractor(NewService(repo, nil), &sourceDocumentExtractorStub{})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/extract-documents", handler.ExtractDocuments)

	payloadResponse := httptest.NewRecorder()
	router.ServeHTTP(payloadResponse, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/extract-documents", strings.NewReader(`{"path":"anywhere"}`)))
	if payloadResponse.Code != http.StatusBadRequest {
		t.Fatalf("payload status = %d, body=%s", payloadResponse.Code, payloadResponse.Body.String())
	}

	nonDoclingResponse := httptest.NewRecorder()
	router.ServeHTTP(nonDoclingResponse, httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/extract-documents", nil))
	if nonDoclingResponse.Code != http.StatusBadRequest {
		t.Fatalf("non-Docling status = %d, body=%s", nonDoclingResponse.Code, nonDoclingResponse.Body.String())
	}
}
