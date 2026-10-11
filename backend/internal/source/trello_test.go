package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// trelloTestServer returns an httptest server that serves a small read-only
// board and records every request method and whether credentials were sent.
func trelloTestServer(t *testing.T, cardsJSON string) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	methods := &[]string{}
	cardQueries := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		if r.URL.Query().Get("key") == "" || r.URL.Query().Get("token") == "" {
			http.Error(w, "missing credentials", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Doing"},{"id":"list-2","name":"Done"}]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			*cardQueries = append(*cardQueries, r.URL.RawQuery)
			_, _ = w.Write([]byte(cardsJSON))
		default: // board metadata
			_, _ = w.Write([]byte(`{"id":"board-1","name":"Client Delivery","shortUrl":"https://trello.com/b/abc123"}`))
		}
	}))
	return server, methods, cardQueries
}

func newTrelloSource(id uuid.UUID, target, cursor string) *models.ConnectedSource {
	return &models.ConnectedSource{
		ID:                id,
		ConnectorKey:      trelloConnectorKey,
		Name:              "Delivery board",
		Category:          "project_board",
		Enabled:           true,
		Status:            "active",
		SyncFrequency:     "manual",
		SyncTarget:        target,
		DefaultProjectKey: "018-HAI",
		Cursor:            cursor,
	}
}

func TestSyncTrelloImportsCardsWithProvenanceAndCursor(t *testing.T) {
	cards := `[
		{"id":"card-1","name":"Prepare client quote","desc":"Draft the quote","shortUrl":"https://trello.com/c/card-1","due":"2026-07-20T09:00:00Z","dateLastActivity":"2026-07-10T12:00:00Z","idList":"list-1","labels":[{"name":"priority","color":"red"}],"actions":[{"id":"comment-2","type":"commentCard","date":"2026-07-10T11:00:00Z","idMemberCreator":"member-1","data":{"text":"Attach the signed proposal."},"memberCreator":{"fullName":"Robert"}},{"id":"comment-1","type":"commentCard","date":"2026-07-09T10:00:00Z","idMemberCreator":"member-2","data":{"text":"Please verify the amount."},"memberCreator":{"username":"reviewer"}}],"checklists":[{"id":"checklist-1","name":"Delivery","pos":1,"checkItems":[{"id":"item-2","name":"Send quote","state":"incomplete","pos":2},{"id":"item-1","name":"Verify amount","state":"complete","pos":1}]}],"attachments":[{"id":"attachment-1","name":"proposal.pdf","url":"https://example.test/proposal.pdf","mimeType":"application/pdf","bytes":2048,"isUpload":true}]},
		{"id":"card-2","name":"Archived idea","dateLastActivity":"2026-07-05T08:00:00Z","idList":"list-2","closed":true}
	]`
	server, methods, cardQueries := trelloTestServer(t, cards)
	defer server.Close()

	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// Closed card must be skipped; only the open card is ingested.
	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1 (closed card skipped)", result.Job.ItemsSeen)
	}
	if result.Job.CursorAfter != "2026-07-10T12:00:00Z" {
		t.Fatalf("CursorAfter = %q, want 2026-07-10T12:00:00Z", result.Job.CursorAfter)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	if !strings.Contains(result.Extractions[0].SourceURI, "trello.com/c/card-1") {
		t.Fatalf("extraction provenance = %q, want card shortUrl", result.Extractions[0].SourceURI)
	}
	if result.Extractions[0].ProjectKey != "018-HAI" {
		t.Fatalf("project key = %q, want default", result.Extractions[0].ProjectKey)
	}
	for _, expected := range []string{"Comments (2)", "Please verify the amount.", "Attach the signed proposal.", "Checklists (1)", "[x] Verify amount", "[ ] Send quote", "Attachments (1; metadata only)", "proposal.pdf", "https://example.test/proposal.pdf"} {
		if !strings.Contains(result.Extractions[0].Text, expected) {
			t.Fatalf("extraction text does not contain %q: %q", expected, result.Extractions[0].Text)
		}
	}
	if strings.Index(result.Extractions[0].Text, "Please verify the amount.") > strings.Index(result.Extractions[0].Text, "Attach the signed proposal.") {
		t.Fatalf("comments were not normalized into chronological order: %q", result.Extractions[0].Text)
	}
	if len(*cardQueries) != 1 {
		t.Fatalf("card queries = %d, want 1", len(*cardQueries))
	}
	for _, expected := range []string{"actions=commentCard", "actions_limit=300", "action_fields=", "action_memberCreator=true", "attachments=true", "checklists=all"} {
		if !strings.Contains((*cardQueries)[0], expected) {
			t.Fatalf("card query %q does not request %q", (*cardQueries)[0], expected)
		}
	}
	if !repo.hasAudit("source.synced") {
		t.Fatalf("expected trello sync audit record")
	}
	// Read-only guarantee: every request the adapter made must be a GET.
	for _, method := range *methods {
		if method != http.MethodGet {
			t.Fatalf("trello adapter issued a %s request; the connector must be read-only", method)
		}
	}
}

func TestSyncContextCancelsTrelloWithoutExposingCredentials(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	t.Setenv(trelloAPIKeyEnv, "test-key-secret")
	t.Setenv(trelloReadTokenEnv, "test-read-token-secret")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", "cursor-before"))
	service := NewService(repo, &fakeSourceMemoryService{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.SyncContext(ctx, sourceID, ImportRequest{Mode: ModeIncrementalSync})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Trello sync did not reach the read-only endpoint")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Trello cancellation error = %v, want context canceled", err)
		}
		if strings.Contains(err.Error(), "test-key-secret") || strings.Contains(err.Error(), "test-read-token-secret") {
			t.Fatalf("Trello cancellation leaked credentials: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Trello sync ignored caller cancellation")
	}
	if len(repo.jobs) != 1 || repo.jobs[0].Status != "failed" || repo.jobs[0].CursorAfter != "cursor-before" {
		t.Fatalf("canceled Trello job = %#v", repo.jobs)
	}
}

func TestSyncTrelloPreservesComplexCardAcceptanceShape(t *testing.T) {
	actions := make([]map[string]any, 0, 30)
	for i := 0; i < 30; i++ {
		actions = append(actions, map[string]any{
			"id": fmt.Sprintf("comment-%02d", i+1), "type": "commentCard",
			"date":          fmt.Sprintf("2026-07-%02dT10:00:00Z", i%28+1),
			"data":          map[string]any{"text": fmt.Sprintf("Operational comment %02d", i+1)},
			"memberCreator": map[string]any{"fullName": "Robert"},
		})
	}
	attachments := []map[string]any{
		{"id": "a-1", "name": "requirements.txt", "url": "https://example.test/requirements.txt", "mimeType": "text/plain", "bytes": 120},
		{"id": "a-2", "name": "evidence.pdf", "url": "https://example.test/evidence.pdf", "mimeType": "application/pdf", "bytes": 2048},
		{"id": "a-3", "name": "walkthrough.png", "url": "https://example.test/walkthrough.png", "mimeType": "image/png", "bytes": 4096},
		{"id": "a-4", "name": "demo.mp4", "url": "https://example.test/demo.mp4", "mimeType": "video/mp4", "bytes": 8192},
		{"id": "a-5", "name": "Drive evidence", "url": "https://drive.google.com/file/d/example/view", "mimeType": "text/html"},
	}
	cards, err := json.Marshal([]map[string]any{{
		"id": "complex-card", "name": "HAI operational card", "desc": "Build the real engine.",
		"shortUrl": "https://trello.com/c/complex", "dateLastActivity": "2026-07-30T12:00:00Z",
		"idList": "list-1", "actions": actions, "attachments": attachments,
		"checklists": []map[string]any{{"id": "check-1", "name": "Acceptance", "pos": 1, "checkItems": []map[string]any{{"id": "item-1", "name": "Verify source links", "state": "incomplete", "pos": 1}}}},
	}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server, methods, _ := trelloTestServer(t, string(cards))
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	text := result.Extractions[0].Text
	for _, expected := range []string{"Comments (30)", "Operational comment 01", "Operational comment 30", "Checklists (1)", "Attachments (5; metadata only)", "requirements.txt", "evidence.pdf", "walkthrough.png", "demo.mp4", "https://drive.google.com/file/d/example/view"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("complex card extraction missing %q", expected)
		}
	}
	rawItems, err := repo.FindRawItems(sourceID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("FindRawItems: items=%#v err=%v", rawItems, err)
	}
	if !strings.Contains(rawItems[0].Metadata, "comments=30") || !strings.Contains(rawItems[0].Metadata, "attachments=5") || !strings.Contains(rawItems[0].Metadata, "attachmentContentFetched=false") {
		t.Fatalf("complex card metadata = %q", rawItems[0].Metadata)
	}
	for _, method := range *methods {
		if method != http.MethodGet {
			t.Fatalf("complex Trello intake issued %s, want GET only", method)
		}
	}
}

func TestSyncTrelloIncrementalSkipsUnchangedCards(t *testing.T) {
	cards := `[
		{"id":"card-old","name":"Stale card","shortUrl":"https://trello.com/c/old","dateLastActivity":"2026-07-01T00:00:00Z","idList":"list-1"},
		{"id":"card-new","name":"Fresh activity","shortUrl":"https://trello.com/c/new","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}
	]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()

	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")

	sourceID := uuid.New()
	// Cursor sits between the two cards' activity timestamps.
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", "2026-07-05T00:00:00Z"))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1 (only the card changed after the cursor)", result.Job.ItemsSeen)
	}
	if len(result.Extractions) != 1 || !strings.Contains(result.Extractions[0].SourceURI, "/c/new") {
		t.Fatalf("expected only the freshly-updated card, got %#v", result.Extractions)
	}
	if result.Job.CursorAfter != "2026-07-12T00:00:00Z" {
		t.Fatalf("CursorAfter = %q, want advance to newest activity", result.Job.CursorAfter)
	}
}

func TestSyncTrelloRequiresCredentials(t *testing.T) {
	server, _, _ := trelloTestServer(t, `[]`)
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	t.Setenv(trelloAPIKeyEnv, "")
	t.Setenv(trelloReadTokenEnv, "")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want a not-configured credential error", err)
	}
}

func TestSyncTrelloRejectsUnallowlistedHost(t *testing.T) {
	t.Setenv(trelloBaseURLEnv, "https://trello.example.net")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("error = %v, want allowlist rejection", err)
	}
}

func TestTrelloBoardID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"board123ABC", "board123ABC", false},
		{"https://trello.com/b/abc123XY/client-board", "abc123XY", false},
		{"https://user:pass@trello.com/b/abc123XY", "", true},
		{"", "", true},
		{"has spaces", "", true},
	}
	for _, tc := range cases {
		got, err := trelloBoardID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("trelloBoardID(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("trelloBoardID(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}
