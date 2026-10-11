//go:build live

// Live acceptance test for the Trello connector. Runs only under `-tags live`
// against the REAL Trello API, using least-privilege read-only credentials:
//
//	TRELLO_API_KEY, TRELLO_READ_TOKEN, TRELLO_LIVE_BOARD
//
// Normal `go test ./...` never compiles or runs this. It exists so the
// connector's status in docs/completion-matrix.md can say "live-tested" on the
// strength of an actual credentialed run rather than a mock.
package source

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func liveBoard(t *testing.T) string {
	t.Helper()
	if strings.TrimSpace(os.Getenv(trelloAPIKeyEnv)) == "" || strings.TrimSpace(os.Getenv(trelloReadTokenEnv)) == "" {
		t.Skip("TRELLO_API_KEY / TRELLO_READ_TOKEN not set; skipping live Trello test")
	}
	board := strings.TrimSpace(os.Getenv("TRELLO_LIVE_BOARD"))
	if board == "" {
		t.Skip("TRELLO_LIVE_BOARD not set; skipping live Trello test")
	}
	return board
}

// TestLiveTrelloSyncAgainstRealBoard performs a full sync against a real board
// and asserts the contract the review asked to see: real cards ingested, source
// provenance retained, an audit trail written, and the cursor advanced.
func TestLiveTrelloSyncAgainstRealBoard(t *testing.T) {
	board := liveBoard(t)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: trelloConnectorKey, Name: "Live Trello board",
		Category: "project_board", Enabled: true, Status: "active",
		SyncFrequency: "manual", SyncTarget: board, DefaultProjectKey: "018-HAI",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	result, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("live Trello sync failed: %v", err)
	}
	if result.Job.Status != "completed" {
		t.Fatalf("job status = %q (%s), want completed", result.Job.Status, result.Job.Message)
	}
	if result.Job.ItemsSeen == 0 {
		t.Fatal("no cards ingested from the live board")
	}
	t.Logf("live sync: seen=%d added=%d updated=%d failed=%d cursor=%q",
		result.Job.ItemsSeen, result.Job.ItemsAdded, result.Job.ItemsUpdated, result.Job.ItemsFailed, result.Job.CursorAfter)

	// Provenance: every extraction must link back to its Trello card.
	for _, extraction := range result.Extractions {
		if !strings.Contains(extraction.SourceURI, "trello.com/c/") {
			t.Fatalf("extraction %q has no Trello card provenance (SourceURI=%q)", extraction.SourceLabel, extraction.SourceURI)
		}
	}
	// Cursor must be a real RFC3339 activity timestamp, not a placeholder.
	if _, ok := parseTrelloTime(result.Job.CursorAfter); !ok {
		t.Fatalf("cursor %q is not a parseable activity timestamp", result.Job.CursorAfter)
	}
	if !repo.hasAudit("source.synced") {
		t.Fatal("expected an audit record for the live sync")
	}
}

// TestLiveTrelloIncrementalSyncSkipsUnchanged proves the cursor genuinely makes
// the second sync incremental against the live API: nothing changed on the
// board between runs, so no card should be re-ingested.
func TestLiveTrelloIncrementalSyncSkipsUnchanged(t *testing.T) {
	board := liveBoard(t)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: trelloConnectorKey, Name: "Live Trello board",
		Category: "project_board", Enabled: true, Status: "active",
		SyncFrequency: "manual", SyncTarget: board, DefaultProjectKey: "018-HAI",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	first, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("first live sync: %v", err)
	}
	if first.Job.ItemsSeen == 0 {
		t.Fatal("first sync ingested nothing; cannot assess incrementality")
	}

	second, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("second live sync: %v", err)
	}
	t.Logf("incremental: first=%d second=%d", first.Job.ItemsSeen, second.Job.ItemsSeen)
	if second.Job.ItemsSeen != 0 {
		t.Fatalf("second sync ingested %d card(s); with no board changes the cursor should skip them all", second.Job.ItemsSeen)
	}
}

// TestLiveTrelloTokenIsReadOnly asserts at the credential level that the token
// cannot write. This is the least-privilege guarantee, verified against Trello
// rather than assumed.
func TestLiveTrelloTokenIsReadOnly(t *testing.T) {
	liveBoard(t)
	base, err := trelloBaseURL()
	if err != nil {
		t.Fatalf("base url: %v", err)
	}
	key := strings.TrimSpace(os.Getenv(trelloAPIKeyEnv))
	token := strings.TrimSpace(os.Getenv(trelloReadTokenEnv))

	var info struct {
		Permissions []struct {
			ModelType string `json:"modelType"`
			Read      bool   `json:"read"`
			Write     bool   `json:"write"`
		} `json:"permissions"`
		DateExpires string `json:"dateExpires"`
	}
	if err := trelloGetJSON(context.Background(), base, key, token, "/1/tokens/"+token, nil, &info); err != nil {
		t.Fatalf("inspect token: %v", err)
	}
	if len(info.Permissions) == 0 {
		t.Fatal("token reported no permissions")
	}
	for _, p := range info.Permissions {
		if p.Write {
			t.Fatalf("token has WRITE permission on %s; the connector requires a read-only token", p.ModelType)
		}
		if !p.Read {
			t.Fatalf("token lacks read permission on %s", p.ModelType)
		}
	}
	if expiry, err := time.Parse(time.RFC3339, info.DateExpires); err == nil {
		t.Logf("token is read-only on %d model(s), expires %s", len(info.Permissions), expiry.Format("2006-01-02"))
	}
}
