package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestRevokeRejectsCrossOwnerBeforeAuthorizationConsumption(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "alice"))
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(NewService(repo, nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	_, err := service.RevokeAuthorized(
		context.Background(),
		sourceID,
		testSourceAuthorization("mallory"),
	)
	if !errors.Is(err, ErrDestructiveOwnerMismatch) {
		t.Fatalf("Revoke error = %v, want ErrDestructiveOwnerMismatch", err)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0", authorizer.calls)
	}
	if source, _ := repo.FindSource(sourceID); source.Status != "active" {
		t.Fatalf("cross-owner revoke changed source: %#v", source)
	}
}

func TestDeleteExtractionRejectsCrossOwnerBeforeAuthorizationConsumption(
	t *testing.T,
) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "alice"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("b", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(
		NewService(repo, nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	err := service.DeleteExtractionAuthorized(
		context.Background(),
		extractionID,
		testSourceAuthorization("mallory"),
	)
	if !errors.Is(err, ErrDestructiveOwnerMismatch) {
		t.Fatalf(
			"DeleteExtraction error = %v, want owner mismatch",
			err,
		)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0", authorizer.calls)
	}
	if _, err := repo.FindExtraction(extractionID); err != nil {
		t.Fatalf("cross-owner deletion changed extraction: %v", err)
	}
}

func TestDestructiveEffectsFailClosedWithoutAuthorizer(t *testing.T) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("b", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	service := configuredSourceEffectService(
		NewService(repo, nil),
		nil,
		clearSourceEmergencyStop,
	)

	if _, err := service.RevokeAuthorized(
		context.Background(),
		sourceID,
		testSourceAuthorization("robert"),
	); !errors.Is(err, ErrDestructiveAuthorizationRequired) {
		t.Fatalf("Revoke error = %v, want authorization required", err)
	}
	if err := service.DeleteExtractionAuthorized(
		context.Background(),
		extractionID,
		testSourceAuthorization("robert"),
	); !errors.Is(err, ErrDestructiveAuthorizationRequired) {
		t.Fatalf("DeleteExtraction error = %v, want authorization required", err)
	}
}

func TestInvalidDestructiveTargetDoesNotConsumeAuthorization(t *testing.T) {
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(
		NewService(newFakeSourceRepo(), nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	if _, err := service.RevokeAuthorized(
		context.Background(),
		uuid.New(),
		testSourceAuthorization("robert"),
	); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Revoke error = %v, want not found", err)
	}
	if err := service.DeleteExtractionAuthorized(
		context.Background(),
		uuid.New(),
		testSourceAuthorization("robert"),
	); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("DeleteExtraction error = %v, want not found", err)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0", authorizer.calls)
	}
}

func TestRevokeConsumesExactAuthorizationOnceAtMutationBoundary(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: "google",
		AccessToken: []byte("encrypted"),
	}); err != nil {
		t.Fatalf("SaveOAuthToken: %v", err)
	}
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(NewService(repo, nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	updated, err := service.RevokeAuthorized(
		context.Background(),
		sourceID,
		testSourceAuthorization("robert"),
	)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorization calls = %d, want 1", authorizer.calls)
	}
	request := authorizer.request
	if request.OwnerIdentity != "robert" ||
		request.ActorIdentity != "robert" ||
		request.Action != revokeSourceAction ||
		request.Stage != executionauth.StageDeletion ||
		request.ResourceType != connectedSourceResourceType ||
		request.ResourceID != sourceID.String() ||
		request.ProjectKey != "project-1" ||
		request.ApprovalSourceID != "approval-1" ||
		request.ApprovalBindingDigest != strings.Repeat("a", 64) ||
		len(request.EffectDigest) != 64 {
		t.Fatalf("authorization request is not exact: %#v", request)
	}
	if authorizer.consumer != sourceAuthorizationConsumer ||
		authorizer.target != "source-effect:"+request.EffectDigest {
		t.Fatalf(
			"consumer/target = %q / %q",
			authorizer.consumer,
			authorizer.target,
		)
	}
	if updated.Enabled || updated.Status != "revoked" || updated.RevokedAt == nil {
		t.Fatalf("source was not revoked: %#v", updated)
	}
	if _, err := repo.FindOAuthToken(sourceID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoked source retained OAuth credentials: %v", err)
	}
}

func TestMismatchedAuthorizationReceiptBlocksRevoke(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	authorizer := &recordingSourceAuthorizer{
		mutateReceipt: func(receipt *executionauth.Receipt) {
			receipt.ResourceID = uuid.NewString()
		},
	}
	service := configuredSourceEffectService(NewService(repo, nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	_, err := service.RevokeAuthorized(
		context.Background(),
		sourceID,
		testSourceAuthorization("robert"),
	)
	if !errors.Is(err, ErrDestructiveAuthorizationMismatch) {
		t.Fatalf("Revoke error = %v, want receipt mismatch", err)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorization calls = %d, want 1", authorizer.calls)
	}
	if source, _ := repo.FindSource(sourceID); source.Status != "active" {
		t.Fatalf("mismatched receipt changed source: %#v", source)
	}
}

func TestEmergencyStopBeforeDeletionSideEffects(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision safety.EmergencyStopDecision
	}{
		{
			name: "active stop",
			decision: safety.EmergencyStopDecision{
				Active: true, Reason: "operator stop", Source: "test",
			},
		},
		{
			name: "unreadable stop state fails closed",
			decision: safety.EmergencyStopDecision{
				Active: true, Reason: "persisted emergency-stop state is unavailable", Source: "persisted_control_error",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceID := uuid.New()
			extractionID := uuid.New()
			repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
			if _, err := repo.SaveExtraction(&models.SourceExtraction{
				ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
				ProjectKey: "project-1", ContentHash: strings.Repeat("c", 64),
			}); err != nil {
				t.Fatalf("SaveExtraction: %v", err)
			}
			authorizer := &recordingSourceAuthorizer{}
			workflowSpy := &fakeSourceWorkflowService{}
			stopChecks := 0
			service := configuredSourceEffectService(NewServiceWithWorkflow(repo, nil, workflowSpy),
				authorizer,
				func() safety.EmergencyStopDecision {
					stopChecks++
					return test.decision
				},
			)

			err := service.DeleteExtractionAuthorized(
				context.Background(), extractionID, testSourceAuthorization("robert"),
			)
			if !errors.Is(err, ErrSourceEmergencyStopActive) {
				t.Fatalf("DeleteExtraction error = %v, want emergency stop", err)
			}
			if stopChecks != 1 {
				t.Fatalf("emergency-stop checks = %d, want only the preflight check", stopChecks)
			}
			if authorizer.calls != 0 {
				t.Fatalf("authorization was consumed %d times while stop was active/unreadable", authorizer.calls)
			}
			if len(workflowSpy.retractions) != 0 {
				t.Fatalf("workflow was retracted while stop was active/unreadable: %#v", workflowSpy.retractions)
			}
			if _, err := repo.FindExtraction(extractionID); err != nil {
				t.Fatalf("active/unreadable stop deleted extraction: %v", err)
			}
		})
	}
}

func TestPostAuthorizationEmergencyStopStillRollsBackDatabaseDeletion(t *testing.T) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("c", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	authorizer := &recordingSourceAuthorizer{}
	workflowSpy := &fakeSourceWorkflowService{}
	stopChecks := 0
	service := configuredSourceEffectService(NewServiceWithWorkflow(repo, nil, workflowSpy),
		authorizer,
		func() safety.EmergencyStopDecision {
			stopChecks++
			return safety.EmergencyStopDecision{
				Active: stopChecks >= 4,
				Reason: "operator stop",
				Source: "test",
			}
		},
	)

	err := service.DeleteExtractionAuthorized(
		context.Background(), extractionID, testSourceAuthorization("robert"),
	)
	if !errors.Is(err, ErrSourceEmergencyStopActive) {
		t.Fatalf("DeleteExtraction error = %v, want emergency stop", err)
	}
	if stopChecks != 4 || authorizer.calls != 1 {
		t.Fatalf("stop checks/authorization calls = %d/%d, want 4/1 for a stop immediately after authorization", stopChecks, authorizer.calls)
	}
	if len(workflowSpy.retractions) != 0 {
		t.Fatalf("workflow retractions = %d, want no post-commit projection after the late stop", len(workflowSpy.retractions))
	}
	if _, err := repo.FindExtraction(extractionID); err != nil {
		t.Fatalf("late stop deleted extraction: %v", err)
	}
}

func TestPostAuthorizationSnapshotChangeRollsBackDeletion(t *testing.T) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("c", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	authorizer := &recordingSourceAuthorizer{
		afterAuthorize: func(executionauth.Request) {
			repo.extractions[extractionID].ProjectKey = "changed-project"
		},
	}
	service := configuredSourceEffectService(
		NewService(repo, nil),
		authorizer,
		clearSourceEmergencyStop,
	)

	err := service.DeleteExtractionAuthorized(
		context.Background(),
		extractionID,
		testSourceAuthorization("robert"),
	)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("DeleteExtraction error = %v, want snapshot mismatch", err)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorization calls = %d, want 1", authorizer.calls)
	}
	if _, err := repo.FindExtraction(extractionID); err != nil {
		t.Fatalf("snapshot mismatch deleted extraction: %v", err)
	}
}

func TestDeletionAuditFailureRollsBackAuthorizedDeletion(t *testing.T) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("d", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	auditFailure := errors.New("audit store unavailable")
	repo.auditLogErr = auditFailure
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(NewService(repo, nil), authorizer, clearSourceEmergencyStop)

	err := service.DeleteExtractionAuthorized(
		context.Background(), extractionID, testSourceAuthorization("robert"),
	)
	if !errors.Is(err, auditFailure) {
		t.Fatalf("DeleteExtraction error = %v, want audit-store failure", err)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorization calls = %d, want 1 before transactional audit", authorizer.calls)
	}
	if _, err := repo.FindExtraction(extractionID); err != nil {
		t.Fatalf("audit failure deleted extraction: %v", err)
	}
	if len(repo.auditLogs) != 0 {
		t.Fatalf("audit logs after failed transactional write = %d, want none", len(repo.auditLogs))
	}
}

func TestDeleteExtractionPreflightRejectionsDoNotConsumeOrRetract(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "worker lock", err: ErrExtractionCorrectionWorkerActive},
		{name: "active correction job", err: ErrExtractionCorrectionWorkerActive},
		{name: "stale extraction revision", err: gorm.ErrRecordNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceID, extractionID := uuid.New(), uuid.New()
			baseRepo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
			if _, err := baseRepo.SaveExtraction(&models.SourceExtraction{
				ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
				ProjectKey: "project-1", ContentHash: strings.Repeat("c", 64),
			}); err != nil {
				t.Fatalf("SaveExtraction: %v", err)
			}
			workflowSpy := &fakeSourceWorkflowService{}
			authorizer := &recordingSourceAuthorizer{}
			repo := &guardedDeleteRejectingSourceRepo{Repository: baseRepo, rejection: test.err}
			service := configuredSourceEffectService(
				NewServiceWithWorkflow(repo, nil, workflowSpy),
				authorizer,
				clearSourceEmergencyStop,
			)

			err := service.DeleteExtractionAuthorized(
				context.Background(), extractionID, testSourceAuthorization("robert"),
			)
			if !errors.Is(err, test.err) {
				t.Fatalf("DeleteExtraction error = %v, want %v", err, test.err)
			}
			if authorizer.calls != 0 {
				t.Fatalf("authorization was consumed %d times before guarded preflight completed", authorizer.calls)
			}
			if len(workflowSpy.retractions) != 0 {
				t.Fatalf("workflow was retracted before guarded preflight completed: %#v", workflowSpy.retractions)
			}
			if _, err := baseRepo.FindExtraction(extractionID); err != nil {
				t.Fatalf("preflight rejection removed extraction: %v", err)
			}
		})
	}
}

func TestDeleteExtractionFailsClosedWithoutGuardedRepository(t *testing.T) {
	sourceID, extractionID := uuid.New(), uuid.New()
	baseRepo := newFakeSourceRepo(testOwnedSource(sourceID, "robert"))
	if _, err := baseRepo.SaveExtraction(&models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: uuid.New(),
		ProjectKey: "project-1", ContentHash: strings.Repeat("c", 64),
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	legacyRepo := struct{ Repository }{Repository: baseRepo}
	authorizer := &recordingSourceAuthorizer{}
	service := configuredSourceEffectService(
		NewService(legacyRepo, nil), authorizer, clearSourceEmergencyStop,
	)

	err := service.DeleteExtractionAuthorized(
		context.Background(), extractionID, testSourceAuthorization("robert"),
	)
	if !errors.Is(err, ErrGuardedExtractionDeleteUnavailable) {
		t.Fatalf("DeleteExtraction error = %v, want guarded-repository failure", err)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorization was consumed %d times without guarded deletion", authorizer.calls)
	}
	if _, err := baseRepo.FindExtraction(extractionID); err != nil {
		t.Fatalf("unguarded repository fallback deleted extraction: %v", err)
	}
}

type guardedDeleteRejectingSourceRepo struct {
	Repository
	rejection error
}

func (r *guardedDeleteRejectingSourceRepo) DeleteExtractionForOwnerGuarded(
	_ *models.SourceExtraction,
	_ *models.ConnectedSource,
	_ string,
	_ func() error,
) error {
	return r.rejection
}

func (r *guardedDeleteRejectingSourceRepo) DeleteExtractionForOwnerGuardedInTransaction(
	_ *models.SourceExtraction,
	_ *models.ConnectedSource,
	_ string,
	_ func(*gorm.DB) (func(bool), error),
) error {
	return r.rejection
}

func (r *guardedDeleteRejectingSourceRepo) SaveAuditLogInTransaction(*gorm.DB, *models.SourceAuditLog) error {
	return r.rejection
}

func TestSourceHTTPHandlersRequireAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID := uuid.New()
	extractionID := uuid.New()
	handler := NewHandler(NewService(newFakeSourceRepo(testOwnedSource(sourceID, "alice")), nil))

	tests := []struct {
		name, method, route, path, body string
		handler                         gin.HandlerFunc
	}{
		{"connectors", http.MethodGet, "/sources/connectors", "/sources/connectors", "", handler.Connectors},
		{"source list", http.MethodGet, "/sources", "/sources?ownerIdentity=bob", "", handler.Sources},
		{"source create", http.MethodPost, "/sources", "/sources", `{"ownerIdentity":"bob"}`, handler.CreateSource},
		{"search", http.MethodPost, "/sources/search", "/sources/search?ownerIdentity=bob", `{"ownerIdentity":"bob","query":"private"}`, handler.Search},
		{"due sync", http.MethodPost, "/sources/sync-due", "/sources/sync-due", "", handler.RunDueScheduledSyncs},
		{"sync jobs", http.MethodGet, "/sources/sync-jobs", "/sources/sync-jobs?sourceId=" + sourceID.String(), "", handler.SyncJobs},
		{"extraction list", http.MethodGet, "/sources/extractions", "/sources/extractions", "", handler.Extractions},
		{"audit logs", http.MethodGet, "/sources/audit-logs", "/sources/audit-logs", "", handler.AuditLogs},
		{"health overview", http.MethodGet, "/sources/connection-health", "/sources/connection-health", "", handler.ConnectionHealths},
		{"source health", http.MethodGet, "/sources/:id/health", "/sources/" + sourceID.String() + "/health", "", handler.ConnectionHealth},
		{"source update", http.MethodPatch, "/sources/:id", "/sources/" + sourceID.String(), `{"ownerIdentity":"bob"}`, handler.UpdateSource},
		{"sync import", http.MethodPost, "/sources/:id/sync", "/sources/" + sourceID.String() + "/sync", `{"ownerIdentity":"bob","items":[]}`, handler.Sync},
		{"transcription", http.MethodPost, "/sources/:id/transcribe", "/sources/" + sourceID.String() + "/transcribe", "", handler.Transcribe},
		{"document extraction", http.MethodPost, "/sources/:id/extract-documents", "/sources/" + sourceID.String() + "/extract-documents", "", handler.ExtractDocuments},
		{"reindex", http.MethodPost, "/sources/:id/reindex", "/sources/" + sourceID.String() + "/reindex", "", handler.Reindex},
		{"pause", http.MethodPost, "/sources/:id/pause", "/sources/" + sourceID.String() + "/pause", "", handler.Pause},
		{"resume", http.MethodPost, "/sources/:id/resume", "/sources/" + sourceID.String() + "/resume", "", handler.Resume},
		{"revoke", http.MethodPost, "/sources/:id/revoke", "/sources/" + sourceID.String() + "/revoke", "", handler.Revoke},
		{"extraction update", http.MethodPatch, "/sources/extractions/:id", "/sources/extractions/" + extractionID.String(), `{"ownerIdentity":"bob"}`, handler.UpdateExtraction},
		{"extraction archive", http.MethodPost, "/sources/extractions/:id/archive", "/sources/extractions/" + extractionID.String() + "/archive", "", handler.ArchiveExtraction},
		{"extraction delete", http.MethodDelete, "/sources/extractions/:id", "/sources/extractions/" + extractionID.String(), "", handler.DeleteExtraction},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Handle(test.method, test.route, test.handler)
			request := newSourceHTTPRequest(test.method, test.path, test.body)
			request.Header.Set("X-Owner-Identity", "bob")
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 for missing trusted identity: %s", response.Code, response.Body.String())
			}
		})
	}
	// GoogleOAuthCallback is intentionally excluded: its short-lived signed state
	// is the callback's authorization proof after the browser returns from Google.
}

func TestSourceHTTPReadsUseAuthenticatedOwnerAndExcludeLegacyRecords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID, bobID, legacyID := uuid.New(), uuid.New(), uuid.New()
	repo := newFakeSourceRepo(
		testOwnedSource(aliceID, "alice"),
		testOwnedSource(bobID, "bob"),
		testOwnedSource(legacyID, ""),
	)
	for _, sourceID := range []uuid.UUID{aliceID, bobID, legacyID} {
		if _, err := repo.SaveExtraction(&models.SourceExtraction{
			ID: uuid.New(), SourceID: sourceID, RawItemID: uuid.New(),
			ProjectKey: "project-1", Text: "tenant-private evidence marker", Summary: "tenant-private evidence marker",
		}); err != nil {
			t.Fatalf("SaveExtraction: %v", err)
		}
	}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources", handler.Sources)
	router.GET("/sources/extractions", handler.Extractions)
	router.POST("/sources/search", handler.Search)

	request := httptest.NewRequest(http.MethodGet, "/sources?ownerIdentity=bob", nil)
	request.Header.Set("X-Owner-Identity", "bob")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("source list status = %d: %s", response.Code, response.Body.String())
	}
	var sources []models.ConnectedSource
	if err := json.Unmarshal(response.Body.Bytes(), &sources); err != nil {
		t.Fatalf("decode source list: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != aliceID {
		t.Fatalf("source list exposed unowned data or honored forged owner: %#v", sources)
	}

	request = httptest.NewRequest(http.MethodGet, "/sources/extractions?ownerIdentity=bob", nil)
	request.Header.Set("X-Owner-Identity", "bob")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("extraction list status = %d: %s", response.Code, response.Body.String())
	}
	var extractions []models.SourceExtraction
	if err := json.Unmarshal(response.Body.Bytes(), &extractions); err != nil {
		t.Fatalf("decode extraction list: %v", err)
	}
	if len(extractions) != 1 || extractions[0].SourceID != aliceID || response.Header().Get("X-Total-Count") != "1" {
		t.Fatalf("extraction list or count crossed ownership boundary: items=%#v count=%q", extractions, response.Header().Get("X-Total-Count"))
	}

	request = httptest.NewRequest(http.MethodPost, "/sources/search?ownerIdentity=bob", strings.NewReader(`{"ownerIdentity":"bob","query":"tenant-private evidence marker"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Identity", "bob")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("source search status = %d: %s", response.Code, response.Body.String())
	}
	var result SearchResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode source search: %v", err)
	}
	if len(result.UsedContext) != 1 || result.UsedContext[0].Extraction.SourceID != aliceID {
		t.Fatalf("search exposed unowned evidence or honored forged owner: %#v", result.UsedContext)
	}
}

func TestCreateSourceUsesAuthenticatedOwnerInsteadOfRequestOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeSourceRepo()
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources", handler.CreateSource)
	request := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(
		`{"ownerIdentity":"bob","connectorKey":"github","name":"HAI repository","category":"github","enabled":true,"localOnly":false,"syncTarget":"org/repo","defaultProjectKey":"018-HAI"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Identity", "bob")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", response.Code, response.Body.String())
	}
	var created models.ConnectedSource
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created source: %v", err)
	}
	stored := repo.sources[created.ID]
	if stored == nil || stored.OwnerIdentity != "alice" {
		var owner string
		if stored != nil {
			owner = stored.OwnerIdentity
		}
		t.Fatalf("stored source owner = %q, want authenticated owner alice", owner)
	}
}

func TestSourceHTTPRejectsCrossOwnerReadAndMutationIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	aliceID, bobID, bobExtractionID := uuid.New(), uuid.New(), uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(aliceID, "alice"), testOwnedSource(bobID, "bob"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{
		ID: bobExtractionID, SourceID: bobID, RawItemID: uuid.New(), Summary: "Bob private evidence",
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	handler := NewHandler(NewService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/sources/sync-jobs", handler.SyncJobs)
	router.GET("/sources/audit-logs", handler.AuditLogs)
	router.GET("/sources/:id/health", handler.ConnectionHealth)
	router.PATCH("/sources/:id", handler.UpdateSource)
	router.POST("/sources/:id/sync", handler.Sync)
	router.POST("/sources/:id/reindex", handler.Reindex)
	router.POST("/sources/:id/pause", handler.Pause)
	router.POST("/sources/:id/resume", handler.Resume)
	router.POST("/sources/:id/revoke", handler.Revoke)
	router.POST("/sources/:id/transcribe", handler.Transcribe)
	router.POST("/sources/:id/extract-documents", handler.ExtractDocuments)
	router.PATCH("/sources/extractions/:id", handler.UpdateExtraction)
	router.POST("/sources/extractions/:id/archive", handler.ArchiveExtraction)
	router.DELETE("/sources/extractions/:id", handler.DeleteExtraction)

	tests := []struct{ method, path, body string }{
		{http.MethodGet, "/sources/sync-jobs?sourceId=" + bobID.String(), ""},
		{http.MethodGet, "/sources/audit-logs?sourceId=" + bobID.String(), ""},
		{http.MethodGet, "/sources/" + bobID.String() + "/health", ""},
		{http.MethodPatch, "/sources/" + bobID.String(), `{"enabled":false,"ownerIdentity":"bob"}`},
		{http.MethodPost, "/sources/" + bobID.String() + "/sync", `{"items":[],"ownerIdentity":"bob"}`},
		{http.MethodPost, "/sources/" + bobID.String() + "/reindex", ""},
		{http.MethodPost, "/sources/" + bobID.String() + "/pause", ""},
		{http.MethodPost, "/sources/" + bobID.String() + "/resume", ""},
		{http.MethodPost, "/sources/" + bobID.String() + "/revoke", ""},
		{http.MethodPost, "/sources/" + bobID.String() + "/transcribe", ""},
		{http.MethodPost, "/sources/" + bobID.String() + "/extract-documents", ""},
		{http.MethodPatch, "/sources/extractions/" + bobExtractionID.String(), `{"sourceId":"` + aliceID.String() + `","summary":"forged"}`},
		{http.MethodPost, "/sources/extractions/" + bobExtractionID.String() + "/archive", ""},
		{http.MethodDelete, "/sources/extractions/" + bobExtractionID.String(), ""},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := newSourceHTTPRequest(test.method, test.path, test.body)
			request.Header.Set("X-Owner-Identity", "bob")
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("cross-owner response = %d, want concealed 404: %s", response.Code, response.Body.String())
			}
		})
	}
	storedSource, err := repo.FindSource(bobID)
	if err != nil || !storedSource.Enabled || storedSource.Status != "active" {
		t.Fatalf("cross-owner mutation changed source: source=%#v err=%v", storedSource, err)
	}
	storedExtraction, err := repo.FindExtraction(bobExtractionID)
	if err != nil || storedExtraction.Archived || storedExtraction.Summary != "Bob private evidence" {
		t.Fatalf("cross-owner mutation changed extraction: extraction=%#v err=%v", storedExtraction, err)
	}
}

func TestSourceHTTPDoesNotFallbackToUnscopedLegacyLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sourceID, extractionID := uuid.New(), uuid.New()
	repo := newFakeSourceRepo(testOwnedSource(sourceID, "alice"))
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: extractionID, SourceID: sourceID, RawItemID: uuid.New()}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	base := NewService(repo, nil)
	legacy := &serviceWithoutScopedSourceLookups{Service: base}
	handler := NewHandler(legacy)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.PATCH("/sources/:id", handler.UpdateSource)
	router.PATCH("/sources/extractions/:id", handler.UpdateExtraction)

	for _, test := range []struct{ path, body string }{
		{"/sources/" + sourceID.String(), `{"enabled":false}`},
		{"/sources/extractions/" + extractionID.String(), `{"summary":"attempt"}`},
	} {
		request := httptest.NewRequest(http.MethodPatch, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("legacy unscoped lookup response = %d, want 503: %s", response.Code, response.Body.String())
		}
	}
	if legacy.globalSourceReads != 0 {
		t.Fatalf("legacy service fallback made %d unscoped source reads, want 0", legacy.globalSourceReads)
	}
	storedSource, _ := repo.FindSource(sourceID)
	if !storedSource.Enabled {
		t.Fatalf("legacy source update changed stored source: %#v", storedSource)
	}
}

func testOwnedSource(id uuid.UUID, owner string) *models.ConnectedSource {
	return &models.ConnectedSource{
		ID: id, OwnerIdentity: owner, ConnectorKey: "email",
		Name: "Project mailbox", Category: "email", Enabled: true,
		LocalOnly: true, Status: "active", DefaultProjectKey: "project-1",
	}
}

func newSourceHTTPRequest(method, path, body string) *http.Request {
	if body == "" {
		return httptest.NewRequest(method, path, nil)
	}
	return httptest.NewRequest(method, path, strings.NewReader(body))
}

type serviceWithoutScopedSourceLookups struct {
	Service
	globalSourceReads int
}

func (s *serviceWithoutScopedSourceLookups) Sources(includeDisabled bool) ([]models.ConnectedSource, error) {
	s.globalSourceReads++
	return s.Service.Sources(includeDisabled)
}

func testSourceAuthorization(owner string) DestructiveEffectAuthorization {
	return DestructiveEffectAuthorization{
		OwnerIdentity: owner, ActorIdentity: owner,
		IdempotencyKey: "source-effect-1", TaskID: "task-1",
		ApprovalSourceID:      "approval-1",
		ApprovalBindingDigest: strings.Repeat("a", 64),
	}
}

func configuredSourceEffectService(
	base Service,
	authorizer FinalEffectAuthorizer,
	stop func() safety.EmergencyStopDecision,
) *service {
	configured, ok := base.(*service)
	if !ok {
		panic("source service does not expose controlled final effects")
	}
	configured.WithDestructiveEffectAuthorization(authorizer, stop)
	return configured
}

func authorizedSourceEffectService(base Service) *service {
	return configuredSourceEffectService(
		base,
		&recordingSourceAuthorizer{},
		clearSourceEmergencyStop,
	)
}

func clearSourceEmergencyStop() safety.EmergencyStopDecision {
	return safety.EmergencyStopDecision{Source: "test"}
}

type recordingSourceAuthorizer struct {
	calls          int
	request        executionauth.Request
	consumer       string
	target         string
	err            error
	mutateReceipt  func(*executionauth.Receipt)
	afterAuthorize func(executionauth.Request)
}

func (a *recordingSourceAuthorizer) AuthorizeAndConsume(
	_ context.Context,
	request executionauth.Request,
	consumer string,
	target string,
) (executionauth.Receipt, error) {
	a.calls++
	a.request = request
	a.consumer = consumer
	a.target = target
	if a.err != nil {
		return executionauth.Receipt{}, a.err
	}
	if a.afterAuthorize != nil {
		a.afterAuthorize(request)
	}
	receipt := executionauth.Receipt{
		ID:                uuid.New(),
		ContractVersion:   executionauth.ContractVersion,
		OwnerIdentity:     request.OwnerIdentity,
		IdempotencyKey:    request.IdempotencyKey,
		ActorIdentity:     request.ActorIdentity,
		ActorKind:         request.ActorKind,
		TaskID:            request.TaskID,
		Action:            request.Action,
		Stage:             request.Stage,
		ResourceType:      request.ResourceType,
		ResourceID:        request.ResourceID,
		ProjectKey:        request.ProjectKey,
		ApprovalSourceID:  request.ApprovalSourceID,
		EffectDigest:      request.EffectDigest,
		Outcome:           executionauth.OutcomeAuthorized,
		RequestDigest:     strings.Repeat("d", 64),
		DecisionDigest:    strings.Repeat("e", 64),
		RequiredAuthority: request.RequiredAuthority,
		RequestedAutonomy: request.RequestedAutonomy,
		Risk:              request.Risk,
		Reversible:        request.Reversible,
		Evidence: executionauth.DecisionEvidence{
			Approval: executionauth.ApprovalEvidence{
				SourceID:       request.ApprovalSourceID,
				DecisionID:     "approval-decision-1",
				DecisionDigest: strings.Repeat("f", 64),
				ApprovedBy:     request.OwnerIdentity,
				ApprovedAt:     time.Now().UTC().Add(-time.Minute),
				ExpiresAt:      time.Now().UTC().Add(time.Minute),
			},
		},
	}
	if a.mutateReceipt != nil {
		a.mutateReceipt(&receipt)
	}
	return receipt, nil
}

func (a *recordingSourceAuthorizer) AuthorizeAndConsumeInTransaction(
	ctx context.Context,
	_ *gorm.DB,
	request executionauth.Request,
	consumer string,
	target string,
) (executionauth.Receipt, executionauth.AuthorizationPostCommitProjection, error) {
	receipt, err := a.AuthorizeAndConsume(ctx, request, consumer, target)
	return receipt, nil, err
}
