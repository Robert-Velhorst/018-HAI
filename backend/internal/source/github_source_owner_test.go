package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestGitHubDeploymentTokenNeverUsesHTTPAndUnboundOwnerStaysUnauthenticated(t *testing.T) {
	var authorization []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/example/project" {
			_, _ = w.Write([]byte(`{"full_name":"example/project","html_url":"https://github.com/example/project"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	t.Setenv("GITHUB_SOURCE_TOKEN", "private-token")
	t.Setenv(githubSourceTokenOwnerIdentityEnv, "alice")
	t.Setenv("GITHUB_SOURCE_API_BASE_URL", server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())

	source := &models.ConnectedSource{OwnerIdentity: "bob", SyncTarget: "example/project"}
	if _, _, err := fetchGitHubSource(t.Context(), source); err != nil {
		t.Fatalf("unauthenticated public GitHub sync: %v", err)
	}
	if _, _, err := fetchGitHubSource(t.Context(), &models.ConnectedSource{OwnerIdentity: "alice", SyncTarget: "example/project"}); err == nil || !strings.Contains(err.Error(), "require an HTTPS") {
		t.Fatalf("bound deployment token over HTTP error = %v, want HTTPS restriction", err)
	}
	if len(authorization) != 5 {
		t.Fatalf("GitHub request count = %d, want only 5 unauthenticated public requests", len(authorization))
	}
	for i, header := range authorization {
		if header != "" {
			t.Fatalf("unauthenticated request %d carried authorization", i)
		}
	}
}

func TestGitHubManualItemsCannotForgeSourceEvidence(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "github", Name: "HAI repository",
		Category: "github", Enabled: true, Status: "active", SyncTarget: "example/project",
	})
	service := NewService(repo, nil)
	_, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{{
			ExternalID: "github:commit:" + commit,
			ItemType:   "github_commit",
			Title:      "Fabricated commit",
			Content:    "A fabricated record must never count as source-grounded quality evidence.",
			SourceURI:  "https://github.com/example/project/commit/" + commit,
			Metadata:   `{"source":"github","repository":"example/project","kind":"commit","fetched_at":"2026-09-24T12:00:00Z"}`,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "caller-supplied items are not accepted") {
		t.Fatalf("manual GitHub sync error = %v, want API-only evidence restriction", err)
	}
	if len(repo.jobs) != 0 || len(repo.rawItems) != 0 || len(repo.extractions) != 0 {
		t.Fatalf("rejected manual GitHub sync persisted evidence: jobs=%d raw=%d extractions=%d", len(repo.jobs), len(repo.rawItems), len(repo.extractions))
	}
}

func TestGitHubSourceTokenOwnerDoesNotTrimOrCaseFoldIdentity(t *testing.T) {
	t.Setenv("GITHUB_SOURCE_TOKEN", "private-token")
	t.Setenv(githubSourceTokenOwnerIdentityEnv, "alice")
	if got := githubSourceTokenForOwner("alice"); got != "private-token" {
		t.Fatal("configured GitHub owner did not receive its deployment token")
	}
	for _, owner := range []string{"bob", " Alice", "alice ", ""} {
		if got := githubSourceTokenForOwner(owner); got != "" {
			t.Fatalf("owner %q received a deployment token", owner)
		}
	}
	t.Setenv(githubSourceTokenOwnerIdentityEnv, "")
	if got := githubSourceTokenForOwner("alice"); got != "" {
		t.Fatal("deployment token was sent without an explicit owner binding")
	}
}
