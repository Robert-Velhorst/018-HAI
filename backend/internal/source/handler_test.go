package source

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/whispercpp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	t.Setenv(identity.LegacyDataOwnerEnv, "")
	aliceID := uuid.New()
	bobID := uuid.New()
	legacyID := uuid.New()
	service := NewService(newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: legacyID, Name: "Ownerless legacy source", Enabled: true, Status: "active"},
	), nil)
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

	t.Setenv(identity.LegacyDataOwnerEnv, "alice")
	legacyResponse := httptest.NewRecorder()
	router.ServeHTTP(legacyResponse, httptest.NewRequest(http.MethodGet, "/sources", nil))
	if legacyResponse.Code != http.StatusOK {
		t.Fatalf("legacy-owner list status = %d, body=%s", legacyResponse.Code, legacyResponse.Body.String())
	}
	if err := json.Unmarshal(legacyResponse.Body.Bytes(), &sources); err != nil {
		t.Fatalf("decode legacy-owner sources: %v", err)
	}
	if len(sources) != 2 || !containsSourceID(sources, aliceID) || !containsSourceID(sources, legacyID) {
		t.Fatalf("legacy-owner visible sources = %#v, want Alice and ownerless legacy sources", sources)
	}

	unauthenticatedRouter := gin.New()
	unauthenticatedRouter.GET("/sources", handler.Sources)
	unauthenticatedResponse := httptest.NewRecorder()
	unauthenticatedRouter.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodGet, "/sources", nil))
	if unauthenticatedResponse.Code != http.StatusOK {
		t.Fatalf("ownerless request status = %d, body=%s", unauthenticatedResponse.Code, unauthenticatedResponse.Body.String())
	}
	if err := json.Unmarshal(unauthenticatedResponse.Body.Bytes(), &sources); err != nil {
		t.Fatalf("decode ownerless request sources: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("ownerless request exposed sources: %#v", sources)
	}

	foreignRequest := httptest.NewRequest(http.MethodPost, "/sources/"+bobID.String()+"/pause", nil)
	foreignResponse := httptest.NewRecorder()
	router.ServeHTTP(foreignResponse, foreignRequest)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("foreign pause status = %d, body=%s", foreignResponse.Code, foreignResponse.Body.String())
	}
}

func containsSourceID(sources []models.ConnectedSource, id uuid.UUID) bool {
	for _, source := range sources {
		if source.ID == id {
			return true
		}
	}
	return false
}

func TestHandlerReturnsOwnerScopedSourceOverview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	now := time.Now().UTC()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	repo.extractions[uuid.New()] = &models.SourceExtraction{
		ID: uuid.New(), SourceID: aliceID, ProjectKey: "project-1", Sensitive: true, Uncertain: true,
	}
	repo.extractions[uuid.New()] = &models.SourceExtraction{
		ID: uuid.New(), SourceID: aliceID, ProjectKey: "project-1", Archived: true,
	}
	repo.extractions[uuid.New()] = &models.SourceExtraction{
		ID: uuid.New(), SourceID: bobID, ProjectKey: "project-1", Sensitive: true,
	}
	repo.jobs = []models.SourceSyncJob{
		{ID: uuid.New(), SourceID: aliceID, Status: "running", CreatedAt: now},
		{ID: uuid.New(), SourceID: aliceID, Status: "failed", CreatedAt: now.Add(-time.Minute)},
		{ID: uuid.New(), SourceID: bobID, Status: "failed", CreatedAt: now.Add(time.Minute)},
	}

	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/overview", handler.Overview)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/overview?projectKey=project-1&includeArchived=false", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("overview status = %d, body=%s", response.Code, response.Body.String())
	}
	var overview struct {
		ExtractionCount          int64             `json:"extractionCount"`
		SensitiveExtractionCount int64             `json:"sensitiveExtractionCount"`
		UncertainExtractionCount int64             `json:"uncertainExtractionCount"`
		FailedJobs               int64             `json:"failedJobs"`
		PendingJobs              int64             `json:"pendingJobs"`
		ExtractionCountsBySource map[string]int64  `json:"extractionCountsBySource"`
		LatestJobStatusBySource  map[string]string `json:"latestJobStatusBySource"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.ExtractionCount != 1 || overview.SensitiveExtractionCount != 1 || overview.UncertainExtractionCount != 1 {
		t.Fatalf("owner extraction summary = %#v", overview)
	}
	if overview.FailedJobs != 1 || overview.PendingJobs != 1 {
		t.Fatalf("owner job summary = %#v", overview)
	}
	if overview.ExtractionCountsBySource[aliceID.String()] != 1 || overview.ExtractionCountsBySource[bobID.String()] != 0 {
		t.Fatalf("per-source extraction counts = %#v", overview.ExtractionCountsBySource)
	}
	if overview.LatestJobStatusBySource[aliceID.String()] != "running" || overview.LatestJobStatusBySource[bobID.String()] != "" {
		t.Fatalf("latest owner job statuses = %#v", overview.LatestJobStatusBySource)
	}
}

func TestHandlerBoundsAuditLogsAfterOwnerFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	repo.auditLogs = []models.SourceAuditLog{
		{ID: uuid.New(), SourceID: bobID, Action: "bob.hidden", Message: "must be filtered first"},
		{ID: uuid.New(), SourceID: aliceID, Action: "alice.latest", Message: "latest"},
		{ID: uuid.New(), SourceID: aliceID, Action: "alice.previous", Message: "previous"},
		{ID: uuid.New(), SourceID: aliceID, Action: "alice.old", Message: "old"},
	}

	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/audit-logs", handler.AuditLogs)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/audit-logs?limit=2", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("audit logs status = %d, body=%s", response.Code, response.Body.String())
	}
	var logs []models.SourceAuditLog
	if err := json.Unmarshal(response.Body.Bytes(), &logs); err != nil {
		t.Fatalf("decode audit logs: %v", err)
	}
	if len(logs) != 2 || logs[0].SourceID != aliceID || logs[1].SourceID != aliceID {
		t.Fatalf("bounded owner logs = %#v, want two Alice logs", logs)
	}

	for _, query := range []string{"limit=0", "limit=501", "limit=invalid"} {
		invalid := httptest.NewRecorder()
		router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/sources/audit-logs?"+query, nil))
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body=%s", query, invalid.Code, invalid.Body.String())
		}
	}
}

func TestHandlerBoundsSyncJobsInsideOwnerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	repo.jobs = []models.SourceSyncJob{
		{ID: uuid.New(), SourceID: bobID, Status: "completed"},
		{ID: uuid.New(), SourceID: aliceID, Status: "completed"},
		{ID: uuid.New(), SourceID: aliceID, Status: "failed"},
	}

	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/sync-jobs", handler.SyncJobs)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources/sync-jobs?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("sync jobs status = %d, body=%s", response.Code, response.Body.String())
	}
	var jobs []models.SourceSyncJob
	if err := json.Unmarshal(response.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("decode sync jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].SourceID != aliceID {
		t.Fatalf("bounded owner jobs = %#v, want one Alice job", jobs)
	}

	unauthenticatedRouter := gin.New()
	unauthenticatedRouter.GET("/sources/sync-jobs", handler.SyncJobs)
	unauthenticatedResponse := httptest.NewRecorder()
	unauthenticatedRouter.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodGet, "/sources/sync-jobs", nil))
	if unauthenticatedResponse.Code != http.StatusOK {
		t.Fatalf("ownerless sync jobs status = %d, body=%s", unauthenticatedResponse.Code, unauthenticatedResponse.Body.String())
	}
	if err := json.Unmarshal(unauthenticatedResponse.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("decode ownerless sync jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("ownerless request exposed sync jobs: %#v", jobs)
	}

	for _, query := range []string{"limit=0", "limit=501", "limit=invalid"} {
		invalid := httptest.NewRecorder()
		router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/sources/sync-jobs?"+query, nil))
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body=%s", query, invalid.Code, invalid.Body.String())
		}
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

func TestGoogleOAuthDeniedCallbackBurnsOwnerBoundState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureGoogleOAuthTest(t)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: gmailConnectorKey,
		Name: "Alice Gmail", Enabled: true, Status: "active",
	})
	service := NewService(repo, nil).(*service)
	authorizeURL, err := service.StartGoogleOAuth(sourceID)
	if err != nil {
		t.Fatalf("StartGoogleOAuth: %v", err)
	}
	state := oauthStateFromAuthorizeURL(t, authorizeURL)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/oauth/google/callback", handler.GoogleOAuthCallback)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/sources/oauth/google/callback?error=access_denied&state="+url.QueryEscape(state),
		nil,
	))
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/connected-sources?oauth=denied" {
		t.Fatalf("denied callback = %d %q", response.Code, response.Header().Get("Location"))
	}
	if _, err := service.CompleteGoogleOAuth(context.Background(), "", state, "alice"); !errors.Is(err, ErrOAuthStateInvalid) {
		t.Fatalf("denied state replay error = %v, want ErrOAuthStateInvalid", err)
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

func TestHandlerCancelsDueSyncBatchWithRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "json-feed", Name: "Cancelable feed",
		Category: "generic_feed", Enabled: true, Status: "active", SyncFrequency: "1m",
		SyncTarget: server.URL, Cursor: "cursor-before",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	handler := NewHandler(NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
	})
	router.POST("/sources/sync-due", handler.RunDueScheduledSyncs)

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/sources/sync-due", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled HTTP batch did not reach the remote connector")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled HTTP batch ignored request cancellation")
	}
	if response.Body.Len() != 0 {
		t.Fatalf("canceled handler wrote a misleading response: %s", response.Body.String())
	}
	if len(repo.jobs) != 1 || repo.jobs[0].Status != "failed" || repo.jobs[0].CursorAfter != "cursor-before" {
		t.Fatalf("canceled scheduled sync job = %#v", repo.jobs)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != "cursor-before" || stored.LastSyncedAt != nil {
		t.Fatalf("canceled scheduled sync advanced source state: %#v", stored)
	}
	if len(workflowSpy.requests) != 0 {
		t.Fatalf("request cancellation created %d misleading failure workflows", len(workflowSpy.requests))
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
	for _, sourceID := range repo.lastExtractionSourceIDs {
		if sourceID == bobID {
			t.Fatalf("handler repository query included Bob's private source")
		}
	}
}

func TestHandlerBoundsExtractionsAfterOwnerFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob source", Enabled: true, Status: "active"},
	)
	for _, extraction := range []models.SourceExtraction{
		{ID: uuid.New(), SourceID: bobID, Summary: "Bob hidden context"},
		{ID: uuid.New(), SourceID: aliceID, Summary: "Alice latest context"},
		{ID: uuid.New(), SourceID: aliceID, Summary: "Alice previous context"},
		{ID: uuid.New(), SourceID: aliceID, Summary: "Alice oldest context"},
	} {
		if _, err := repo.SaveExtraction(&extraction); err != nil {
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
		t.Fatalf("extractions status = %d, body=%s", response.Code, response.Body.String())
	}
	var extractions []models.SourceExtraction
	if err := json.Unmarshal(response.Body.Bytes(), &extractions); err != nil {
		t.Fatalf("decode extractions: %v", err)
	}
	if len(extractions) != 2 {
		t.Fatalf("bounded extractions = %d, want 2", len(extractions))
	}
	for _, extraction := range extractions {
		if extraction.SourceID != aliceID {
			t.Fatalf("bounded extractions exposed foreign source: %#v", extractions)
		}
	}

	for _, query := range []string{"limit=0", "limit=501", "limit=invalid"} {
		invalid := httptest.NewRecorder()
		router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/sources/extractions?"+query, nil))
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body=%s", query, invalid.Code, invalid.Body.String())
		}
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

func TestHandlerRejectsEveryRevokedSourceMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	extractionID := uuid.New()
	revokedAt := time.Now().UTC().Add(-time.Minute)
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Revoked files",
		Enabled: false, Status: "revoked", RevokedAt: &revokedAt, LocalOnly: true, SyncTarget: "notes",
	})
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: extractionID, SourceID: sourceID, Summary: "Retained audit context"}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	handler := NewHandler(NewService(repo, nil), &sourceTranscriberStub{})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/oauth/google/start", handler.StartGoogleOAuth)
	router.PATCH("/sources/:id", handler.UpdateSource)
	router.POST("/sources/:id/sync", handler.Sync)
	router.POST("/sources/:id/transcribe", handler.Transcribe)
	router.POST("/sources/:id/reindex", handler.Reindex)
	router.POST("/sources/:id/pause", handler.Pause)
	router.POST("/sources/:id/resume", handler.Resume)
	router.POST("/sources/extractions/:id/archive", handler.ArchiveExtraction)

	requests := []struct {
		name, method, target, body string
	}{
		{name: "oauth", method: http.MethodGet, target: "/sources/oauth/google/start?sourceId=" + sourceID.String()},
		{name: "update", method: http.MethodPatch, target: "/sources/" + sourceID.String(), body: `{}`},
		{name: "sync", method: http.MethodPost, target: "/sources/" + sourceID.String() + "/sync", body: `{}`},
		{name: "transcribe", method: http.MethodPost, target: "/sources/" + sourceID.String() + "/transcribe"},
		{name: "reindex", method: http.MethodPost, target: "/sources/" + sourceID.String() + "/reindex"},
		{name: "pause", method: http.MethodPost, target: "/sources/" + sourceID.String() + "/pause"},
		{name: "resume", method: http.MethodPost, target: "/sources/" + sourceID.String() + "/resume"},
		{name: "archive extraction", method: http.MethodPost, target: "/sources/extractions/" + extractionID.String() + "/archive"},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			httpRequest := httptest.NewRequest(request.method, request.target, strings.NewReader(request.body))
			if request.body != "" {
				httpRequest.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(response, httpRequest)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", response.Code, response.Body.String())
			}
		})
	}

	storedSource, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if storedSource.Enabled || storedSource.Status != "revoked" || storedSource.RevokedAt == nil {
		t.Fatalf("revoked source changed: %#v", storedSource)
	}
	storedExtraction, err := repo.FindExtraction(extractionID)
	if err != nil {
		t.Fatalf("FindExtraction: %v", err)
	}
	if storedExtraction.Archived {
		t.Fatalf("revoked extraction was mutated: %#v", storedExtraction)
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
