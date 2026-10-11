package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestTrelloCredentialOwnerBindingIsExactAndRequired(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	if !trelloConfigured() || !trelloCredentialsBelongToOwner("alice") {
		t.Fatal("configured owner should be able to use the shared Trello token")
	}
	for _, owner := range []string{"bob", " Alice", "alice ", ""} {
		if trelloCredentialsBelongToOwner(owner) {
			t.Fatalf("owner %q unexpectedly matched the configured Trello identity", owner)
		}
	}

	t.Setenv(trelloOwnerIdentityEnv, " ")
	if trelloConfigured() {
		t.Fatal("Trello must be unavailable without an explicit account owner")
	}
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, "not-a-member-id")
	if trelloConfigured() {
		t.Fatal("Trello must be unavailable without a valid expected provider member ID")
	}
}

func TestTrelloCredentialOwnerBindingBlocksOtherHAIUsers(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	t.Setenv(trelloBaseURLEnv, "https://api.trello.com")

	service := NewService(newFakeSourceRepo(), nil)
	_, err := service.CreateSource(CreateSourceRequest{
		OwnerIdentity: "bob",
		ConnectorKey:  trelloConnectorKey,
		Name:          "Bob's board",
		Enabled:       true,
		LocalOnly:     false,
		SyncFrequency: "manual",
		SyncTarget:    "abc123XY",
	})
	if err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("CreateSource error = %v, want shared-credential owner restriction", err)
	}
}

func TestFetchTrelloSourceRejectsUnassignedOwnerBeforeProviderRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = "bob"
	_, _, err := fetchTrelloSource(t.Context(), source)
	if err == nil || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("fetch error = %v, want credential owner mismatch", err)
	}
	if requests != 0 {
		t.Fatalf("provider requests = %d, want none for an unassigned HAI owner", requests)
	}
}

func TestFetchTrelloSourceRejectsTokenForDifferentProviderMemberBeforeBoardReads(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/1/tokens/") {
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000002","permissions":[{"modelType":"board","read":true,"write":false}]}`))
			return
		}
		http.Error(w, "unexpected board read", http.StatusInternalServerError)
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	_, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err == nil || !strings.Contains(err.Error(), "different account") {
		t.Fatalf("fetchTrelloSource error = %v, want configured-member mismatch", err)
	}
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "/1/tokens/") {
		t.Fatalf("provider requests = %v, want token verification only before board access", requests)
	}
}

func TestTrelloConnectionHealthDoesNotReportAnotherOwnersCredentialsAsReady(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "bob", ConnectorKey: trelloConnectorKey,
		Name: "Board owned by Bob", Enabled: true, Status: "active", SyncTarget: "abc123XY",
	})
	health, err := NewService(repo, nil).(ConnectionHealthService).ConnectionHealth(sourceID)
	if err != nil {
		t.Fatalf("ConnectionHealth: %v", err)
	}
	if health.Configured || health.Authorized || health.Status != "configuration_required" || !strings.Contains(health.Reason, "different HAI owner") {
		t.Fatalf("health = %#v, want credentials unavailable to Bob", health)
	}
}

func TestTrelloConnectionHealthSurfacesLatestFailedSyncAfterEarlierSuccess(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	lastSuccess := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	source := &models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
		Name: "Board", Enabled: true, Status: "active", SyncTarget: "abc123XY", LastSyncedAt: &lastSuccess,
	}
	repo := newFakeSourceRepo(source)
	repo.jobs = []models.SourceSyncJob{
		{ID: uuid.New(), SourceID: sourceID, Status: "completed", StartedAt: lastSuccess, CreatedAt: lastSuccess},
		{ID: uuid.New(), SourceID: sourceID, Status: "failed", Message: "provider rejected the token", StartedAt: lastSuccess.Add(time.Hour), CreatedAt: lastSuccess.Add(time.Hour)},
	}

	health, err := NewService(repo, nil).(ConnectionHealthService).ConnectionHealth(sourceID)
	if err != nil {
		t.Fatalf("ConnectionHealth: %v", err)
	}
	if health.Status != "sync_failed" || health.Authorized || health.LastSyncedAt == nil || !strings.Contains(health.Reason, "latest Trello read-only sync failed") {
		t.Fatalf("health = %#v, want latest failure and prior success kept distinct", health)
	}
}

func TestTrelloSourceBoardTargetCannotBeRedirected(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
		Name: "Board A", Category: "project_board", Enabled: true,
		LocalOnly: false, Status: "active", SyncTarget: "abc123XY",
	})
	service := NewService(repo, nil)
	otherBoard := "https://trello.com/b/def456GH/another-board"
	if _, err := service.UpdateSource(sourceID, UpdateSourceRequest{SyncTarget: &otherBoard}); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("board redirect error = %v, want immutable target rejection", err)
	}
	if repo.sources[sourceID].SyncTarget != "abc123XY" {
		t.Fatalf("stored board target changed after rejected update: %q", repo.sources[sourceID].SyncTarget)
	}

	sameBoard := "https://trello.com/b/abc123XY/renamed-board"
	updated, err := service.UpdateSource(sourceID, UpdateSourceRequest{SyncTarget: &sameBoard})
	if err != nil {
		t.Fatalf("same-board canonical update: %v", err)
	}
	if updated.SyncTarget != "abc123XY" {
		t.Fatalf("same-board target = %q, want canonical board ID", updated.SyncTarget)
	}
}

func TestTrelloSourceCannotBeChangedToLocalOnly(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
		Name: "Board", Category: "project_board", Enabled: true,
		LocalOnly: false, Status: "active", SyncTarget: "abc123XY",
	})
	localOnly := true
	if _, err := NewService(repo, nil).UpdateSource(sourceID, UpdateSourceRequest{LocalOnly: &localOnly}); err == nil || !strings.Contains(err.Error(), "remote read-only") {
		t.Fatalf("UpdateSource error = %v, want remote-only rejection", err)
	}
	if repo.sources[sourceID].LocalOnly {
		t.Fatal("rejected update changed the Trello source to local-only")
	}
}

func TestTrelloConnectionHealthUsesRecentSuccessfulSyncEvidence(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	t.Setenv("TRELLO_WEBHOOK_CALLBACK_URL", "")
	t.Setenv(trelloAPISecretEnv, "")
	for _, test := range []struct {
		name       string
		age        time.Duration
		wantStatus string
		wantAuth   bool
	}{
		{name: "recent complete sync", age: time.Minute, wantStatus: "polling_operational", wantAuth: true},
		{name: "old complete sync", age: maxSchedulerInterval + time.Minute, wantStatus: "previously_verified"},
	} {
		t.Run(test.name, func(t *testing.T) {
			lastSync := time.Now().UTC().Add(-test.age)
			sourceID := uuid.New()
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
				Name: "Board", Enabled: true, Status: "active", SyncTarget: "abc123XY", LastSyncedAt: &lastSync,
			})
			repo.jobs = []models.SourceSyncJob{{ID: uuid.New(), SourceID: sourceID, Status: "completed", CreatedAt: lastSync}}
			health, err := NewService(repo, nil).(ConnectionHealthService).ConnectionHealth(sourceID)
			if err != nil {
				t.Fatalf("ConnectionHealth: %v", err)
			}
			if health.Status != test.wantStatus || health.Authorized != test.wantAuth {
				t.Fatalf("health = %#v, want status %q and authorized=%v", health, test.wantStatus, test.wantAuth)
			}
			if test.wantAuth && (!strings.Contains(health.PollingReason, "API polling is operational") || !strings.Contains(health.PollingReason, "not webhook registration or delivery")) {
				t.Fatalf("health reason = %q, want the point-in-time verification boundary", health.Reason)
			}
			if health.WebhookStatus != "unconfigured" {
				t.Fatalf("webhook status = %q, want unconfigured independent of API sync", health.WebhookStatus)
			}
		})
	}
}

func TestTrelloConnectionHealthSurfacesNonSuccessfulLatestSyncStates(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	lastSuccess := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		jobStatus  string
		wantStatus string
		wantReason string
	}{
		{name: "partial failure", jobStatus: "partial_failure", wantStatus: "sync_partial_failure", wantReason: "only part"},
		{name: "running", jobStatus: "running", wantStatus: "sync_running", wantReason: "currently running"},
		{name: "queued", jobStatus: "queued", wantStatus: "sync_queued", wantReason: "is queued"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceID := uuid.New()
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
				Name: "Board", Enabled: true, Status: "active", SyncTarget: "abc123XY", LastSyncedAt: &lastSuccess,
			})
			repo.jobs = []models.SourceSyncJob{{ID: uuid.New(), SourceID: sourceID, Status: test.jobStatus, CreatedAt: lastSuccess.Add(time.Hour)}}
			health, err := NewService(repo, nil).(ConnectionHealthService).ConnectionHealth(sourceID)
			if err != nil {
				t.Fatalf("ConnectionHealth: %v", err)
			}
			if health.Status != test.wantStatus || !strings.Contains(health.Reason, test.wantReason) || !strings.Contains(health.Reason, lastSuccess.Format(time.RFC3339)) {
				t.Fatalf("health = %#v, want status %q, reason containing %q and last successful timestamp", health, test.wantStatus, test.wantReason)
			}
		})
	}
}
