package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestFetchAutofeverUsesBearerTokenAndCursor(t *testing.T) {
	var authorization, cursor, requestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		cursor = r.URL.Query().Get("cursor")
		requestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nextCursor":"cursor-2","items":[{"externalId":"autofever:listing:1","title":"Volvo V60 — shortlisted","content":"Reviewed Autofever vehicle with data confidence 92 percent.","sourceUri":"https://cars.example/1","itemType":"autofever_vehicle_review","projectKey":"028-Autofever","metadata":"connector=autofever;read_only=true"}]}`))
	}))
	defer server.Close()

	t.Setenv(autofeverTokenEnv, "afhai_test-token")
	t.Setenv(autofeverOwnerEnv, "robert")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	source := &models.ConnectedSource{
		ID: uuid.New(), ConnectorKey: autofeverConnectorKey, SyncTarget: server.URL + "/api/integrations/hai/feed",
		OwnerIdentity: "robert", DefaultProjectKey: "028-Autofever", Cursor: "cursor-1",
	}
	parsedTarget, err := url.Parse(source.SyncTarget)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", sourceHTTPURLAddress(parsedTarget))
	items, nextCursor, err := fetchAutofever(context.Background(), source)
	if err != nil {
		t.Fatalf("fetchAutofever: %v", err)
	}
	if authorization != "Bearer afhai_test-token" {
		t.Fatalf("Authorization = %q", authorization)
	}
	if cursor != "cursor-1" || nextCursor != "cursor-2" {
		t.Fatalf("cursor request=%q response=%q", cursor, nextCursor)
	}
	if !strings.HasPrefix(requestID, "hai-autofever-") {
		t.Fatalf("X-Request-ID = %q", requestID)
	}
	if len(items) != 1 || items[0].ExternalID != "autofever:listing:1" || items[0].ProjectKey != "028-Autofever" {
		t.Fatalf("items = %#v", items)
	}
}

func TestFetchAutofeverFailsClosedWithoutToken(t *testing.T) {
	t.Setenv(autofeverTokenEnv, "")
	t.Setenv(autofeverOwnerEnv, "robert")
	_, _, err := fetchAutofever(context.Background(), &models.ConnectedSource{OwnerIdentity: "robert", SyncTarget: "https://autofever.example/api/integrations/hai/feed"})
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("error = %v, want missing credentials", err)
	}
}

func TestAutofeverTargetCannotRedirectTokenToAnotherPath(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "autofever.example")
	_, err := validateAutofeverTarget("https://autofever.example/not-the-feed")
	if err == nil || !strings.Contains(err.Error(), "/api/integrations/hai/feed") {
		t.Fatalf("error = %v, want fixed feed path rejection", err)
	}
}

func TestAutofeverCredentialsAreBoundToOneHAIOwner(t *testing.T) {
	t.Setenv(autofeverTokenEnv, "afhai_test-token")
	t.Setenv(autofeverOwnerEnv, "robert")
	if !autofeverCredentialsBelongToOwner("robert") {
		t.Fatal("configured owner should be authorized")
	}
	if autofeverCredentialsBelongToOwner("alice") || autofeverCredentialsBelongToOwner(" robert") {
		t.Fatal("shared token must not be available to another or non-canonical owner identity")
	}
}

func TestFetchAutofeverHonorsCanceledContext(t *testing.T) {
	t.Setenv(autofeverTokenEnv, "afhai_test-token")
	t.Setenv(autofeverOwnerEnv, "robert")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := fetchAutofever(ctx, &models.ConnectedSource{
		OwnerIdentity: "robert", SyncTarget: "https://autofever.example/api/integrations/hai/feed",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchAutofever error = %v, want context cancellation", err)
	}
}

func TestCreateAutofeverSourceRejectsOtherOwner(t *testing.T) {
	t.Setenv(autofeverTokenEnv, "afhai_test-token")
	t.Setenv(autofeverOwnerEnv, "robert")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "autofever.example")
	service := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{})
	_, err := service.CreateSource(CreateSourceRequest{
		OwnerIdentity: "alice", ConnectorKey: autofeverConnectorKey, Name: "Autofever",
		Category: "vehicle_intelligence", SyncTarget: "https://autofever.example/api/integrations/hai/feed",
		Enabled: true, SyncFrequency: "manual",
	})
	if err == nil || !strings.Contains(err.Error(), "configured HAI account owner") {
		t.Fatalf("CreateSource error = %v, want owner restriction", err)
	}
}

func TestAutofeverTargetRequiresHTTPSOutsideLocalDevelopment(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "autofever.example,host.docker.internal")
	if _, err := validateAutofeverTarget("http://autofever.example/api/integrations/hai/feed"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("remote HTTP target error = %v, want HTTPS requirement", err)
	}
	if _, err := validateAutofeverTarget("https://autofever.example/api/integrations/hai/feed?token=wrong-place"); err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("query target error = %v, want query rejection", err)
	}
	if _, err := validateAutofeverTarget("http://host.docker.internal/api/integrations/hai/feed"); err != nil {
		t.Fatalf("local Docker host target: %v", err)
	}
}

func TestAutofeverConnectorReportsConfigurationTruthfully(t *testing.T) {
	t.Setenv(autofeverTokenEnv, "")
	t.Setenv(autofeverOwnerEnv, "")
	connectors, err := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{}).Connectors()
	if err != nil {
		t.Fatalf("Connectors: %v", err)
	}
	for _, connector := range connectors {
		if connector.ConnectorKey == autofeverConnectorKey {
			if connector.AdapterStatus != AdapterConfigurationRequired || !strings.Contains(connector.StatusReason, autofeverTokenEnv) || !strings.Contains(connector.StatusReason, autofeverOwnerEnv) {
				t.Fatalf("connector = %#v", connector)
			}
			return
		}
	}
	t.Fatal("Autofever connector missing from catalog")
}
