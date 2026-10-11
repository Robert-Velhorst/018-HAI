package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type manualSyncSubmitTestRepo struct {
	*fakeSourceRepo
	jobs    map[string]models.SourceSyncJob
	durable map[uuid.UUID]models.DurableJob
}

func (r *manualSyncSubmitTestRepo) FindManualSyncJobByIdempotencyKey(owner string, sourceID uuid.UUID, keyHash string) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	source, sourceExists := r.sources[sourceID]
	if !sourceExists || source.OwnerIdentity != owner {
		return nil, nil, false, nil
	}
	key := owner + ":" + sourceID.String() + ":" + keyHash
	stored, ok := r.jobs[key]
	if !ok || stored.OwnerIdentity != owner || stored.SourceID != sourceID || stored.Mode != ModeManualAsyncSync {
		return nil, nil, false, nil
	}
	if stored.DurableJobID == nil {
		return nil, nil, false, gorm.ErrRecordNotFound
	}
	durable, ok := r.durable[*stored.DurableJobID]
	if !ok {
		return nil, nil, false, gorm.ErrRecordNotFound
	}
	job := stored
	return &job, &durable, true, nil
}

func (r *manualSyncSubmitTestRepo) CreateManualSyncJob(
	job *models.SourceSyncJob,
	durable *models.DurableJob,
) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	key := job.OwnerIdentity + ":" + job.SourceID.String() + ":" + job.IdempotencyKeyHash
	if stored, ok := r.jobs[key]; ok {
		if stored.RequestHash != job.RequestHash {
			return nil, nil, false, ErrManualSyncIdempotencyConflict
		}
		storedDurable := r.durable[*stored.DurableJobID]
		return &stored, &storedDurable, false, nil
	}
	storedJob := *job
	storedDurable := *durable
	storedJob.DurableJobID = &storedDurable.ID
	r.jobs[key] = storedJob
	r.durable[storedDurable.ID] = storedDurable
	return &storedJob, &storedDurable, true, nil
}

func (r *manualSyncSubmitTestRepo) FindManualSyncJobForOwner(owner string, id uuid.UUID) (*models.SourceSyncJob, *models.DurableJob, error) {
	for _, stored := range r.jobs {
		source, sourceExists := r.sources[stored.SourceID]
		if stored.ID != id || stored.OwnerIdentity != owner || stored.DurableJobID == nil || !sourceExists || source.OwnerIdentity != owner {
			continue
		}
		job := stored
		durable, ok := r.durable[*stored.DurableJobID]
		if !ok {
			return nil, nil, gorm.ErrRecordNotFound
		}
		return &job, &durable, nil
	}
	return nil, nil, gorm.ErrRecordNotFound
}

func (r *manualSyncSubmitTestRepo) FindManualSyncJob(id uuid.UUID) (*models.SourceSyncJob, error) {
	for _, stored := range r.jobs {
		if stored.ID == id {
			job := stored
			return &job, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *manualSyncSubmitTestRepo) StartManualSyncJob(id, sourceID uuid.UUID, now time.Time) (*models.SourceSyncJob, error) {
	for key, stored := range r.jobs {
		if stored.ID != id || stored.SourceID != sourceID {
			continue
		}
		stored.Status = "running"
		stored.StartedAt = now.UTC()
		r.jobs[key] = stored
		return &stored, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *manualSyncSubmitTestRepo) UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	updated, err := r.fakeSourceRepo.UpdateSyncJob(job)
	if err != nil {
		return nil, err
	}
	for key, stored := range r.jobs {
		if stored.ID == job.ID {
			r.jobs[key] = *updated
			break
		}
	}
	return updated, nil
}

func TestSubmitManualSyncRetryKeepsOriginalJobWhenDefaultProjectChanges(t *testing.T) {
	const (
		owner          = "alice"
		idempotencyKey = "stable-manual-sync-request-key"
		firstDefault   = "project-v1"
	)
	sourceID := uuid.New()
	baseRepo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		OwnerIdentity:     owner,
		ConnectorKey:      "trello",
		Name:              "Trello",
		Enabled:           true,
		Status:            "active",
		DefaultProjectKey: firstDefault,
	})
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: baseRepo,
		jobs:           map[string]models.SourceSyncJob{},
		durable:        map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	svc.setManualSyncWorkerReady(true)

	first, created, err := svc.SubmitManualSync(owner, sourceID, idempotencyKey, ManualSyncRequest{})
	if err != nil {
		t.Fatalf("first SubmitManualSync: %v", err)
	}
	if !created {
		t.Fatal("first SubmitManualSync created=false, want true")
	}
	stored := onlyManualSyncTestRecord(t, repo)
	var firstPayload manualSyncPayload
	if err := json.Unmarshal([]byte(repo.durable[*stored.DurableJobID].Payload), &firstPayload); err != nil {
		t.Fatalf("decode first queue payload: %v", err)
	}
	if firstPayload.ProjectKey != firstDefault {
		t.Fatalf("queued project key = %q, want captured default %q", firstPayload.ProjectKey, firstDefault)
	}

	// The caller retries the same empty request after the mutable source default
	// changes. Its idempotency fingerprint must still describe the empty input.
	baseRepo.sources[sourceID].DefaultProjectKey = "project-v2"
	second, created, err := svc.SubmitManualSync(owner, sourceID, idempotencyKey, ManualSyncRequest{})
	if err != nil {
		t.Fatalf("retry after default change: %v", err)
	}
	if created {
		t.Fatal("retry created a second queue record")
	}
	if second.ID != first.ID {
		t.Fatalf("retry job id = %q, want original %q", second.ID, first.ID)
	}
	if second.Status != first.Status {
		t.Fatalf("retry status = %q, want original %q", second.Status, first.Status)
	}
	if len(repo.jobs) != 1 || len(repo.durable) != 1 {
		t.Fatalf("stored history/queue counts = %d/%d, want 1/1", len(repo.jobs), len(repo.durable))
	}
	var retryPayload manualSyncPayload
	if err := json.Unmarshal([]byte(repo.durable[*stored.DurableJobID].Payload), &retryPayload); err != nil {
		t.Fatalf("decode retained queue payload: %v", err)
	}
	if retryPayload.ProjectKey != firstDefault {
		t.Fatalf("retained queued project key = %q, want original %q", retryPayload.ProjectKey, firstDefault)
	}

	expectedRequest, err := json.Marshal(struct {
		SourceID   string `json:"sourceId"`
		ProjectKey string `json:"projectKey,omitempty"`
	}{SourceID: sourceID.String()})
	if err != nil {
		t.Fatalf("marshal normalized caller request: %v", err)
	}
	digest := sha256.Sum256(expectedRequest)
	if stored.RequestHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("request hash does not match normalized caller request: got %s", stored.RequestHash)
	}
}

func TestQueuedLocalFolderSyncKeepsCapturedProjectWithoutOverwritingNewDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root+"/project-note.md", "Decision: keep the queued project assignment. Follow up: verify the imported source.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	const owner = "alice"
	sourceID := uuid.New()
	baseRepo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: owner, ConnectorKey: "local-folder", Name: "Local project folder",
		Category: "local_folder", Enabled: true, LocalOnly: true, Status: "active",
		SyncTarget: ".", DefaultProjectKey: "project-v1",
	})
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: baseRepo,
		jobs:           map[string]models.SourceSyncJob{},
		durable:        map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	svc.setManualSyncWorkerReady(true)

	accepted, created, err := svc.SubmitManualSync(owner, sourceID, "local-folder-captured-project-key", ManualSyncRequest{})
	if err != nil || !created {
		t.Fatalf("SubmitManualSync = (%#v, %v, %v), want accepted new job", accepted, created, err)
	}
	stored := onlyManualSyncTestRecord(t, repo)
	var payload manualSyncPayload
	if err := json.Unmarshal([]byte(repo.durable[*stored.DurableJobID].Payload), &payload); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if payload.ProjectKey != "project-v1" {
		t.Fatalf("queued project key = %q, want captured project-v1", payload.ProjectKey)
	}

	baseRepo.sources[sourceID].DefaultProjectKey = "project-v2"
	durableID := *stored.DurableJobID
	if err := svc.RunManualSyncJob(context.Background(), payload, durableID, 1, 3); err != nil {
		t.Fatalf("RunManualSyncJob: %v", err)
	}
	if got := baseRepo.sources[sourceID].DefaultProjectKey; got != "project-v2" {
		t.Fatalf("source default after queued sync = %q, want newer project-v2", got)
	}
	if len(baseRepo.extractions) != 1 {
		t.Fatalf("imported extraction count = %d, want one", len(baseRepo.extractions))
	}
	for _, extraction := range baseRepo.extractions {
		if extraction.ProjectKey != "project-v1" {
			t.Fatalf("queued import project key = %q, want captured project-v1", extraction.ProjectKey)
		}
	}
}

func TestQueuedManualTrelloSyncAppliesOnlyExplicitProjectOverride(t *testing.T) {
	const (
		owner             = "alice"
		defaultProject    = "project-default-at-submit"
		changedDefault    = "project-default-at-execution"
		activityTimestamp = "2026-06-01T09:45:00Z"
	)
	cards := `[{"id":"card-override","name":"Preserve project assignment","desc":"A durable manual Trello sync test.","dateLastActivity":"2026-06-01T09:45:00Z","idList":"list-1"}]`
	tests := []struct {
		name                   string
		explicitProjectKey     string
		wantProjectKey         string
		wantProjectKeyOverride bool
	}{
		{
			name:                   "explicit override survives durable queue",
			explicitProjectKey:     "project-explicit-override",
			wantProjectKey:         "project-explicit-override",
			wantProjectKeyOverride: true,
		},
		{
			name:           "omitted override uses Trello adapter project",
			wantProjectKey: changedDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := trelloTestServer(t, cards)
			defer server.Close()
			configureTrelloTest(t, server.URL)

			sourceID := uuid.New()
			source := newTrelloSource(sourceID, "abc123XY", "")
			source.OwnerIdentity = owner
			source.DefaultProjectKey = defaultProject
			baseRepo := newFakeSourceRepo(source)
			repo := &manualSyncSubmitTestRepo{
				fakeSourceRepo: baseRepo,
				jobs:           map[string]models.SourceSyncJob{},
				durable:        map[uuid.UUID]models.DurableJob{},
			}
			svc := NewService(repo, nil).(*service)
			svc.setManualSyncWorkerReady(true)

			_, created, err := svc.SubmitManualSync(owner, sourceID, "manual-trello-project-override-test", ManualSyncRequest{
				ProjectKey: tt.explicitProjectKey,
			})
			if err != nil || !created {
				t.Fatalf("SubmitManualSync created=%t, err=%v; want new queued sync", created, err)
			}
			stored := onlyManualSyncTestRecord(t, repo)
			durable := repo.durable[*stored.DurableJobID]
			var payload manualSyncPayload
			if err := json.Unmarshal([]byte(durable.Payload), &payload); err != nil {
				t.Fatalf("decode durable payload: %v", err)
			}
			if payload.ProjectKeyOverride != tt.wantProjectKeyOverride {
				t.Fatalf("queued projectKeyOverride = %t, want %t", payload.ProjectKeyOverride, tt.wantProjectKeyOverride)
			}
			wantQueuedProject := firstNonEmpty(tt.explicitProjectKey, defaultProject)
			if payload.ProjectKey != wantQueuedProject {
				t.Fatalf("queued projectKey = %q, want %q", payload.ProjectKey, wantQueuedProject)
			}

			// The worker must use the explicit queued override, but an omitted
			// override must leave the adapter's own current project assignment intact.
			source.DefaultProjectKey = changedDefault
			if err := svc.RunManualSyncJob(context.Background(), payload, durable.ID, 1, 3); err != nil {
				t.Fatalf("RunManualSyncJob: %v", err)
			}

			if len(baseRepo.rawItems) != 1 {
				t.Fatalf("stored raw item count = %d, want 1", len(baseRepo.rawItems))
			}
			var raw *models.SourceRawItem
			for _, item := range baseRepo.rawItems {
				raw = item
			}
			if raw.ExternalID != "trello:card:card-override" || raw.Title != "Preserve project assignment" {
				t.Fatalf("stored raw item = %#v, want the fetched Trello card", raw)
			}
			if raw.ProjectKey != tt.wantProjectKey {
				t.Fatalf("stored raw item project = %q, want %q", raw.ProjectKey, tt.wantProjectKey)
			}
			if len(baseRepo.extractions) != 1 {
				t.Fatalf("stored extraction count = %d, want 1", len(baseRepo.extractions))
			}
			for _, extraction := range baseRepo.extractions {
				if extraction.RawItemID != raw.ID {
					t.Fatalf("extraction raw item id = %q, want %q", extraction.RawItemID, raw.ID)
				}
				if extraction.ProjectKey != tt.wantProjectKey {
					t.Fatalf("stored extraction project = %q, want %q", extraction.ProjectKey, tt.wantProjectKey)
				}
			}
			persistedSource := baseRepo.sources[sourceID]
			if persistedSource == nil {
				t.Fatal("synced source was not persisted")
			}
			if persistedSource.DefaultProjectKey != changedDefault {
				t.Fatalf("sync changed source project default to %q, want unchanged %q", persistedSource.DefaultProjectKey, changedDefault)
			}
			cursorIDs := assertTrelloCursor(t, persistedSource.Cursor, activityTimestamp)
			if len(cursorIDs) != 0 || !strings.HasPrefix(persistedSource.Cursor, trelloCursorV2Prefix) {
				t.Fatalf("persisted Trello cursor = %q with tie ids %#v; want timestamp-only v2 cursor", persistedSource.Cursor, cursorIDs)
			}
		})
	}
}

func TestManualSyncRetryProjectionRemainsNonterminalUntilDurableJobSettles(t *testing.T) {
	completedAt := time.Now().UTC()
	job := &models.SourceSyncJob{Status: "failed", Message: "temporary attempt error", CompletedAt: &completedAt}
	durable := &models.DurableJob{Status: models.DurableJobRunning}
	if view := manualSyncView(job, durable); view.Status != "running" || view.CompletedAt != nil || !strings.Contains(view.Message, "retrying") {
		t.Fatalf("running retry projection = %#v, want nonterminal retry status and message", view)
	}

	durable.Status = models.DurableJobPending
	if view := manualSyncView(job, durable); view.Status != "queued" || view.CompletedAt != nil || !strings.Contains(view.Message, "queued for retry") {
		t.Fatalf("pending retry projection = %#v, want queued status and retry message", view)
	}

	durable.Status = models.DurableJobDead
	if view := manualSyncView(job, durable); view.Status != "failed" {
		t.Fatalf("dead queue projection status = %q, want terminal failed", view.Status)
	}
	if manualSyncProjectedStatus("partial_failure", models.DurableJobRunning) != "running" ||
		manualSyncProjectedStatus("partial_failure", models.DurableJobPending) != "queued" ||
		manualSyncProjectedStatus("partial_failure", models.DurableJobDead) != "failed" {
		t.Fatal("history projection did not track running, pending, and dead durable states")
	}
}

func TestRunManualSyncJobDefersWhenSourceLeaseIsBusyWithoutConsumingAttempt(t *testing.T) {
	const owner = "alice"
	sourceID, syncJobID, durableJobID := uuid.New(), uuid.New(), uuid.New()
	base := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	base.sources[sourceID].OwnerIdentity = owner
	base.sourceLeaseAcquired = false
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: base,
		jobs: map[string]models.SourceSyncJob{
			"manual-job": {
				ID: syncJobID, SourceID: sourceID, OwnerIdentity: owner, Mode: ModeManualAsyncSync,
				Status: "queued", DurableJobID: &durableJobID,
			},
		},
		durable: map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	payload := manualSyncPayload{SourceID: sourceID.String(), SyncJobID: syncJobID.String()}
	err := svc.RunManualSyncJob(context.Background(), payload, durableJobID, 1, 3)
	var deferred *durablejob.DeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("busy source sync error = %v, want a deferred job", err)
	}
	stored := repo.jobs["manual-job"]
	if stored.Status != "queued" || stored.CompletedAt != nil || stored.Message != "" {
		t.Fatalf("source-lock contention changed manual sync history: %#v", stored)
	}
}

func TestRunManualSyncJobMarksPermanentProviderFailureFailedWithoutRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private-provider-response-must-not-leak", http.StatusUnauthorized)
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	syncJobID := uuid.New()
	durableJobID := uuid.New()
	const validCursor = "2026-07-01T00:00:00Z"
	source := newTrelloSource(sourceID, "abc123XY", validCursor)
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: newFakeSourceRepo(source),
		jobs: map[string]models.SourceSyncJob{
			"manual-job": {
				ID: syncJobID, SourceID: sourceID, OwnerIdentity: "alice", Mode: ModeManualAsyncSync,
				Status: "queued", CursorBefore: validCursor, CursorAfter: validCursor, DurableJobID: &durableJobID,
			},
		},
		durable: map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	payload := manualSyncPayload{SourceID: sourceID.String(), SyncJobID: syncJobID.String()}

	err := svc.RunManualSyncJob(context.Background(), payload, durableJobID, 1, 5)
	if err == nil {
		t.Fatal("RunManualSyncJob succeeded, want permanent provider failure")
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.Retryable() {
		t.Fatalf("manual sync error = %T %v, want a non-retryable provider error", err, err)
	}
	if strings.Contains(err.Error(), "private-provider-response-must-not-leak") {
		t.Fatalf("error exposed provider response: %v", err)
	}
	stored := repo.jobs["manual-job"]
	if stored.Status != "failed" || stored.CompletedAt == nil {
		t.Fatalf("manual sync history status/completedAt = %q/%v, want failed/completed", stored.Status, stored.CompletedAt)
	}
	if stored.CursorAfter != stored.CursorBefore {
		t.Fatalf("failed sync advanced cursor to %q, want unchanged cursor %q", stored.CursorAfter, stored.CursorBefore)
	}
	if !strings.Contains(stored.Message, "credentials") || !strings.Contains(stored.Message, "reconnect") || strings.Contains(stored.Message, "private-provider-response-must-not-leak") {
		t.Fatalf("manual sync public message = %q, want safe provider guidance", stored.Message)
	}
}

func TestManualSyncIdempotentRetryWorksWhenPausedOrWorkerUnavailable(t *testing.T) {
	const owner = "alice"
	sourceID := uuid.New()
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: newFakeSourceRepo(&models.ConnectedSource{
			ID: sourceID, OwnerIdentity: owner, ConnectorKey: "trello", Name: "Trello",
			Enabled: true, Status: "active", DefaultProjectKey: "project-v1",
		}),
		jobs:    map[string]models.SourceSyncJob{},
		durable: map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	svc.setManualSyncWorkerReady(true)
	const key = "paused-source-idempotency-retry-key"
	first, created, err := svc.SubmitManualSync(owner, sourceID, key, ManualSyncRequest{})
	if err != nil || !created {
		t.Fatalf("initial submit = (%#v, %v, %v), want accepted new job", first, created, err)
	}

	repo.sources[sourceID].Enabled = false
	repo.sources[sourceID].Status = "paused"
	svc.setManualSyncWorkerReady(false)
	retry, created, err := svc.SubmitManualSync(owner, sourceID, key, ManualSyncRequest{})
	if err != nil || created || retry == nil || retry.ID != first.ID {
		t.Fatalf("same-payload retry while paused/unavailable = (%#v, %v, %v), want original accepted job", retry, created, err)
	}
	if _, _, err := svc.SubmitManualSync(owner, sourceID, key, ManualSyncRequest{ProjectKey: "different-project"}); err != ErrManualSyncIdempotencyConflict {
		t.Fatalf("mismatched retry while paused/unavailable error = %v, want idempotency conflict", err)
	}
	if _, _, err := svc.SubmitManualSync(owner, sourceID, "new-work-worker-unavailable-key", ManualSyncRequest{}); err != ErrManualSyncWorkerUnavailable {
		t.Fatalf("new request while worker unavailable error = %v, want worker unavailable", err)
	}

	svc.setManualSyncWorkerReady(true)
	if _, _, err := svc.SubmitManualSync(owner, sourceID, "new-work-paused-source-key", ManualSyncRequest{}); err != ErrManualSyncSourceDisabled {
		t.Fatalf("new request for paused source error = %v, want source disabled", err)
	}
	if len(repo.jobs) != 1 || len(repo.durable) != 1 {
		t.Fatalf("stored history/queue counts = %d/%d, want 1/1", len(repo.jobs), len(repo.durable))
	}
}

func TestManualSyncHTTPSubmissionAndOwnerScopedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const owner = "alice"
	sourceID := uuid.New()
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: newFakeSourceRepo(&models.ConnectedSource{
			ID: sourceID, OwnerIdentity: owner, ConnectorKey: "trello", Name: "Trello",
			Enabled: true, Status: "active", DefaultProjectKey: "project-v1",
		}),
		jobs:    map[string]models.SourceSyncJob{},
		durable: map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	svc.setManualSyncWorkerReady(true)
	handler := NewHandler(svc)

	newRouter := func(subject string) *gin.Engine {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(identity.ContextSubjectKey, subject)
		})
		router.POST("/sources/:id/sync-jobs", handler.SubmitManualSync)
		router.GET("/sources/sync-jobs/:id", handler.ManualSyncJob)
		return router
	}

	request := httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/sync-jobs", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "manual-sync-http-request-key")
	accepted := httptest.NewRecorder()
	newRouter(owner).ServeHTTP(accepted, request)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, body=%s; want 202", accepted.Code, accepted.Body.String())
	}
	if accepted.Header().Get("Location") == "" || accepted.Header().Get("Retry-After") != "3" {
		t.Fatalf("submission headers = %#v, want Location and Retry-After 3", accepted.Header())
	}
	var job ManualSyncJobView
	if err := json.Unmarshal(accepted.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode accepted job: %v", err)
	}
	if job.Status != "queued" || job.SourceID != sourceID.String() {
		t.Fatalf("accepted job = %#v, want queued job for source %s", job, sourceID)
	}
	if strings.Contains(accepted.Body.String(), owner) || strings.Contains(accepted.Body.String(), "project-v1") {
		t.Fatalf("response exposes private identity or queued payload: %s", accepted.Body.String())
	}

	status := httptest.NewRecorder()
	newRouter(owner).ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/sources/sync-jobs/"+job.ID, nil))
	if status.Code != http.StatusOK || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("owner status response = %d headers=%v body=%s", status.Code, status.Header(), status.Body.String())
	}
	foreign := httptest.NewRecorder()
	newRouter("bob").ServeHTTP(foreign, httptest.NewRequest(http.MethodGet, "/sources/sync-jobs/"+job.ID, nil))
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign status response = %d body=%s, want 404", foreign.Code, foreign.Body.String())
	}
}

func onlyManualSyncTestRecord(t *testing.T, repo *manualSyncSubmitTestRepo) models.SourceSyncJob {
	t.Helper()
	if len(repo.jobs) != 1 {
		t.Fatalf("manual sync record count = %d, want 1", len(repo.jobs))
	}
	for _, job := range repo.jobs {
		if job.DurableJobID == nil {
			t.Fatal("manual sync record is missing its durable job id")
		}
		if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
			t.Fatalf("manual sync timestamps not initialized: created=%s updated=%s", job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano))
		}
		return job
	}
	t.Fatal("manual sync record missing")
	return models.SourceSyncJob{}
}
