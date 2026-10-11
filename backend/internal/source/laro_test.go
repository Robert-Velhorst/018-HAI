package source

import (
	"automation-hub-backend/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLAROSyncUsesBearerCredentialAndIncrementalCursor(t *testing.T) {
	var receivedCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/laro/api/integrations/hai/feed" {
			t.Fatalf("unexpected LARO request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer laro_hai_test-credential" || request.Header.Get("User-Agent") != laroConnectorUserAgent {
			t.Fatalf("unexpected LARO request headers: %#v", request.Header)
		}
		if request.URL.Query().Get("limit") != "2" {
			t.Fatalf("limit = %q, want 2", request.URL.Query().Get("limit"))
		}
		receivedCursor = request.URL.Query().Get("cursor")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nextCursor":"cursor-2",
			"items":[{
				"externalId":"laro-analysis:analysis-1",
				"title":"Decision: notice.pdf",
				"content":"Legal analysis summary. A response is due on 14 August 2026.",
				"sourceUri":"laro://cases/case-1/evidence/evidence-1",
				"itemType":"laro_legal_analysis",
				"projectKey":"laro:case-1",
				"metadata":"source=laro;read_only=true;sensitive=true;review_required=true"
			}]
		}`))
	}))
	defer server.Close()

	t.Setenv("HAI_LARO_ENABLED", "true")
	t.Setenv("HAI_LARO_BASE_URL", server.URL+"/laro")
	t.Setenv("HAI_LARO_CONNECTOR_TOKEN", "laro_hai_test-credential")
	t.Setenv("HAI_LARO_SYNC_LIMIT", "2")

	repo := newFakeSourceRepo()
	service := NewService(repo, &fakeSourceMemoryService{})
	source, err := service.CreateSource(CreateSourceRequest{
		OwnerIdentity:     "owner@example.test",
		ConnectorKey:      laroConnectorKey,
		Name:              "LARO cases",
		Enabled:           true,
		LocalOnly:         false,
		SyncFrequency:     "15m",
		DefaultProjectKey: "legal",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	source.Cursor = "cursor-1"
	if _, err := repo.UpdateSource(source); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}

	result, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if receivedCursor != "cursor-1" || result.Job.CursorAfter != "cursor-2" || result.Job.ItemsSeen != 1 {
		t.Fatalf("sync result = %#v, received cursor = %q", result.Job, receivedCursor)
	}
	for _, extraction := range repo.extractions {
		if !extraction.Sensitive || extraction.SourceURI != "laro://cases/case-1/evidence/evidence-1" {
			t.Fatalf("LARO extraction = %#v, want sensitive source-linked record", extraction)
		}
	}
	for _, item := range repo.rawItems {
		if strings.Contains(item.Content, "test-credential") || strings.Contains(item.Metadata, "test-credential") || strings.Contains(item.SourceURI, "test-credential") {
			t.Fatalf("LARO bearer credential leaked into stored source record: %#v", item)
		}
	}
}

func TestLAROConfigurationFailsClosed(t *testing.T) {
	t.Setenv("HAI_LARO_ENABLED", "true")
	t.Setenv("HAI_LARO_BASE_URL", "http://laro.example.test/laro")
	t.Setenv("HAI_LARO_CONNECTOR_TOKEN", "laro_hai_not-exposed")
	if _, err := laroConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("config error = %v, want HTTPS rejection", err)
	}
	t.Setenv("HAI_LARO_BASE_URL", "https://laro.example.test/laro?token=bad")
	if _, err := laroConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("config error = %v, want URL credential/query rejection", err)
	}
	t.Setenv("HAI_LARO_BASE_URL", "https://laro.example.test/laro")
	t.Setenv("HAI_LARO_CONNECTOR_TOKEN", "")
	if _, err := laroConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "missing or invalid") {
		t.Fatalf("config error = %v, want token rejection", err)
	}
}

func TestLAROSyncRecoversRejectedLegacyCursorAndClearsIt(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Query().Get("cursor") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"Cursor is invalid"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"nextCursor":""}`))
	}))
	defer server.Close()

	t.Setenv("HAI_LARO_ENABLED", "true")
	t.Setenv("HAI_LARO_BASE_URL", server.URL)
	t.Setenv("HAI_LARO_CONNECTOR_TOKEN", "laro_hai_test-credential")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "owner@example.test", ConnectorKey: laroConnectorKey,
		Name: "LARO cases", Category: "legal_case", Enabled: true, Status: "active",
		SyncFrequency: "15m", Cursor: "2026-08-09T02:22:10Z:0",
	})
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if requests != 2 || result.Job.Status != "completed" || result.Job.CursorAfter != "" {
		t.Fatalf("requests=%d result=%#v, want one cursor retry and a cleared cursor", requests, result.Job)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil || stored.Cursor != "" || stored.LastSyncedAt == nil {
		t.Fatalf("stored source=%#v err=%v, want successful cursor reset", stored, err)
	}
	if !repo.hasAudit("source.laro_cursor_reset") {
		t.Fatal("expected an auditable LARO cursor reset")
	}
}
