package source

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestManualSyncRejectsRepeatedIdempotencyHeadersBeforeQueueing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, repeated := range []string{"manual-sync-header-key-01", "manual-sync-header-key-02"} {
		t.Run(repeated, func(t *testing.T) {
			sourceID := uuid.New()
			repo := &manualSyncSubmitTestRepo{
				fakeSourceRepo: newFakeSourceRepo(&models.ConnectedSource{
					ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "trello", Name: "Trello",
					Enabled: true, Status: "active", DefaultProjectKey: "project",
				}),
				jobs: map[string]models.SourceSyncJob{}, durable: map[uuid.UUID]models.DurableJob{},
			}
			svc := NewService(repo, nil).(*service)
			svc.setManualSyncWorkerReady(true)
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
			router.POST("/sources/:id/sync-jobs", NewHandler(svc).SubmitManualSync)
			request := httptest.NewRequest(http.MethodPost, "/sources/"+sourceID.String()+"/sync-jobs", strings.NewReader(`{}`))
			request.Header.Add("Idempotency-Key", "manual-sync-header-key-01")
			request.Header.Add("Idempotency-Key", repeated)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || len(repo.jobs) != 0 || len(repo.durable) != 0 {
				t.Fatalf("ambiguous sync headers: status=%d history=%d queue=%d; want 400 and no work", response.Code, len(repo.jobs), len(repo.durable))
			}
		})
	}
}

func TestExtractionCorrectionRejectsRepeatedBindingHeadersBeforeSaving(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, header := range []string{"Idempotency-Key", "If-Match"} {
		for _, same := range []bool{true, false} {
			name := header + "/different"
			if same {
				name = header + "/identical"
			}
			t.Run(name, func(t *testing.T) {
				repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
				router.PATCH("/sources/extractions/:id", NewHandler(svc).UpdateExtraction)
				request := httptest.NewRequest(http.MethodPatch, "/sources/extractions/"+extraction.ID.String(), strings.NewReader(`{"summary":"new summary"}`))
				request.Header.Set("Idempotency-Key", strings.Repeat("c", 24))
				request.Header.Set("If-Match", extraction.UpdatedAt.UTC().Format(time.RFC3339Nano))
				repeated := request.Header.Get(header)
				if !same {
					repeated = "different-header-value"
				}
				request.Header.Add(header, repeated)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != http.StatusBadRequest || len(repo.corrections) != 0 || len(repo.correctionJobs) != 0 || repo.extractions[extraction.ID].Summary != "before" {
					t.Fatalf("ambiguous correction headers: status=%d intents=%d jobs=%d; want 400 with no saved work", response.Code, len(repo.corrections), len(repo.correctionJobs))
				}
			})
		}
	}
}
