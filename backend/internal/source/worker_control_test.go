package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerControlSyncIsReadOnlyAndIncremental(t *testing.T) {
	var cursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/integrations/hai/feed" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer vwc_hai_test-credential" {
			t.Fatalf("authorization header was not the configured connector key")
		}
		cursor = request.URL.Query().Get("cursor")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nextCursor":"cursor-2",
			"items":[{
				"externalId":"worker-control-event:2",
				"title":"Commitment update: Prepare report",
				"content":"Status: open\nEvent: reminder_scheduled",
				"sourceUri":"worker-control://commitments/1/events/2",
				"itemType":"worker_commitment_event",
				"projectKey":"worker-control:1",
				"metadata":"source=worker_control;read_only=true;sensitive=true;review_required=true"
			}]
		}`))
	}))
	defer server.Close()

	t.Setenv("HAI_WORKER_CONTROL_ENABLED", "true")
	t.Setenv("HAI_WORKER_CONTROL_BASE_URL", server.URL)
	t.Setenv("HAI_WORKER_CONTROL_CONNECTOR_TOKEN", "vwc_hai_test-credential")
	t.Setenv("HAI_WORKER_CONTROL_SYNC_LIMIT", "2")

	items, nextCursor, err := fetchWorkerControlSource(t.Context(), "cursor-1", "operations")
	if err != nil {
		t.Fatalf("fetchWorkerControlSource: %v", err)
	}
	if cursor != "cursor-1" || nextCursor != "cursor-2" || len(items) != 1 {
		t.Fatalf("cursor=%q next=%q items=%d", cursor, nextCursor, len(items))
	}
	if strings.Contains(items[0].Content, "test-credential") || items[0].ItemType != "worker_commitment_event" {
		t.Fatalf("unsafe worker-control item: %#v", items[0])
	}
}

func TestWorkerControlConfigurationFailsClosed(t *testing.T) {
	t.Setenv("HAI_WORKER_CONTROL_ENABLED", "true")
	t.Setenv("HAI_WORKER_CONTROL_BASE_URL", "http://dashboard.example.test")
	t.Setenv("HAI_WORKER_CONTROL_CONNECTOR_TOKEN", "vwc_hai_not-exposed")
	if _, err := workerControlConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("configuration error = %v, want HTTPS rejection", err)
	}
	t.Setenv("HAI_WORKER_CONTROL_BASE_URL", "https://dashboard.example.test?token=bad")
	if _, err := workerControlConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("configuration error = %v, want URL credential rejection", err)
	}
}

func TestWorkerControlRejectsUnsafeProvenanceURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items":[{
				"externalId":"worker-control-event:2",
				"title":"Commitment update",
				"content":"Status: open",
				"sourceUri":"https://dashboard.example.test/commitments/1?token=must-not-persist",
				"itemType":"worker_commitment_event",
				"metadata":"source=worker_control;read_only=true"
			}]
		}`))
	}))
	defer server.Close()

	t.Setenv("HAI_WORKER_CONTROL_ENABLED", "true")
	t.Setenv("HAI_WORKER_CONTROL_BASE_URL", server.URL)
	t.Setenv("HAI_WORKER_CONTROL_CONNECTOR_TOKEN", "vwc_hai_test-credential")

	if _, _, err := fetchWorkerControlSource(t.Context(), "", "operations"); err == nil || !strings.Contains(err.Error(), "unsafe provenance URI") {
		t.Fatalf("fetchWorkerControlSource error = %v, want unsafe provenance rejection", err)
	}
}
