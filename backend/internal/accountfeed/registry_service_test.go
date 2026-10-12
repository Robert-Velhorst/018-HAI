package accountfeed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	feedsDir := t.TempDir()
	ops := operations.NewService(operations.NewMemoryRepository())
	reg := NewRegistry(ops, privacyfilter.NewService(), FetchOptions{FeedsRoot: feedsDir})
	return reg, feedsDir
}

const genericFeed = `{
  "cursor": "next-42",
  "items": [
    {"externalId":"m1","title":"Invoice","content":"Please review the invoice","itemType":"email","provider":"gmail","accountLabel":"primary"},
    {"externalId":"i1","title":"Bug","content":"Fix the crash","itemType":"issue","provider":"github","accountLabel":"repo"}
  ]
}`

func TestRegisterValidatesProvider(t *testing.T) {
	reg, _ := newTestRegistry(t)
	_, err := reg.Register(Feed{Name: "bad", Provider: "not_a_provider", SourceType: SourceLocalJSONFile, Path: "x.json", OwnerUserID: "u"})
	if err == nil {
		t.Fatalf("invalid provider must be rejected")
	}
	feed, err := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile, Path: "feed.json", OwnerUserID: "u"})
	if err != nil {
		t.Fatalf("valid feed must register: %v", err)
	}
	if feed.ID.String() == "" {
		t.Fatalf("registered feed must get an id")
	}
}

func TestSyncGenericFeedIntoLedger(t *testing.T) {
	reg, dir := newTestRegistry(t)
	if err := os.WriteFile(filepath.Join(dir, "feed.json"), []byte(genericFeed), 0o600); err != nil {
		t.Fatal(err)
	}
	feed, _ := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile, Path: "feed.json", OwnerUserID: "u", WorkspaceID: "local", Enabled: true})

	rep, ok := reg.Sync(context.Background(), feed.ID)
	if !ok {
		t.Fatalf("feed must sync")
	}
	if rep.ItemsRead != 2 || rep.OperationsCreated != 2 {
		t.Fatalf("expected 2 items -> 2 operations, got read=%d created=%d errs=%v", rep.ItemsRead, rep.OperationsCreated, rep.Errors)
	}
	if rep.Cursor != "next-42" {
		t.Fatalf("cursor must be surfaced, got %q", rep.Cursor)
	}
	// Re-sync must be idempotent (dedupe), creating no new operations.
	rep2, _ := reg.Sync(context.Background(), feed.ID)
	if rep2.OperationsCreated != 0 {
		t.Fatalf("re-sync must not duplicate, created %d", rep2.OperationsCreated)
	}
	// Audit trail recorded.
	if len(reg.Audit(feed.ID)) < 2 {
		t.Fatalf("sync must record an audit trail")
	}
}

func TestSyncSkipsOverlappingRunForSameFeed(t *testing.T) {
	reg, _ := newTestRegistry(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	releaseServer := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(genericFeed))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseServer)
	reg.opts.AllowHTTP = true
	feed, err := reg.Register(Feed{Name: "slow inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceHTTPJSONFeed, URL: server.URL, OwnerUserID: "u", WorkspaceID: "local", Enabled: true})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	firstDone := make(chan SyncReport, 1)
	go func() {
		report, _ := reg.Sync(context.Background(), feed.ID)
		firstDone <- report
	}()
	<-started

	secondDone := make(chan SyncReport, 1)
	go func() {
		report, _ := reg.Sync(context.Background(), feed.ID)
		secondDone <- report
	}()
	select {
	case report := <-secondDone:
		if !strings.Contains(strings.Join(report.Errors, " "), "already in progress") {
			t.Fatalf("overlapping sync error = %v, want in-progress rejection", report.Errors)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("overlapping sync blocked instead of being rejected")
	}

	releaseServer()
	if report := <-firstDone; len(report.Errors) != 0 {
		t.Fatalf("first sync errors = %v", report.Errors)
	}
}

func TestGenericItemValidationRules(t *testing.T) {
	base := GenericItem{ExternalID: "x", Title: "t", ItemType: "email", Provider: "gmail"}
	if err := base.Validate(0, 0); err != nil {
		t.Fatalf("valid item should pass: %v", err)
	}
	// Missing provider/itemType.
	if err := (GenericItem{ExternalID: "x", Title: "t", ItemType: "email"}).Validate(0, 0); err == nil {
		t.Fatalf("missing provider must fail")
	}
	if err := (GenericItem{ExternalID: "x", Title: "t", Provider: "gmail"}).Validate(0, 0); err == nil {
		t.Fatalf("missing itemType must fail")
	}
	// Secret in sourceUri.
	bad := base
	bad.SourceURI = "https://x/y?token=sk-secret"
	if err := bad.Validate(0, 0); err == nil {
		t.Fatalf("sourceUri with a secret must be rejected")
	}
}

func TestHTTPFeedDisabledByDefault(t *testing.T) {
	reg, _ := newTestRegistry(t)
	feed, err := reg.Register(Feed{Name: "remote", Provider: string(ProviderGenericJSONFeed), SourceType: SourceHTTPJSONFeed, URL: "http://localhost:9/x", OwnerUserID: "u", Enabled: true})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	rep, _ := reg.Sync(context.Background(), feed.ID)
	if len(rep.Errors) == 0 {
		t.Fatalf("HTTP feed must fail to sync when HTTP is disabled")
	}
}

func TestBridgesAndPermissionsAreTruthful(t *testing.T) {
	// API providers without credentials are credentials_required, never connected.
	b, _ := Bridge(ProviderGmail)
	if b.ConnectionStatus() != ConnCredentialsRequired {
		t.Fatalf("gmail must be credentials_required without a token, got %s", b.ConnectionStatus())
	}
	if !b.ReadOnly {
		t.Fatalf("gmail bridge must be read-only")
	}
	// Local providers are available.
	lf, _ := Bridge(ProviderGenericJSONFeed)
	if lf.ConnectionStatus() != ConnAvailable {
		t.Fatalf("generic feed must be available")
	}
	// Permission registry never allows writes.
	pr := NewPermissionRegistry()
	if pr.WriteAllowed(ProviderGmail) {
		t.Fatalf("no bridge may allow writes")
	}
	perm, _ := pr.Permission(ProviderGmail)
	if perm.Granted {
		t.Fatalf("gmail must not be granted without a real credential")
	}
}

func TestCredentialPresenceIsUnverifiedNotConnected(t *testing.T) {
	t.Setenv("GITHUB_SOURCE_TOKEN", "ghp_dummy_token_value")
	b, _ := Bridge(ProviderGitHub)
	if b.ConnectionStatus() != ConnCredentialsPresentUnverified {
		t.Fatalf("a present credential must be credentials_present_unverified (not connected), got %s", b.ConnectionStatus())
	}
}
