//go:build live

// Live acceptance test for the Trello connector. Runs only under `-tags live`
// against the REAL Trello API, using least-privilege read-only credentials:
//
//	TRELLO_API_KEY, TRELLO_READ_TOKEN, TRELLO_ACCOUNT_OWNER_IDENTITY,
//	TRELLO_ACCOUNT_MEMBER_ID, TRELLO_LIVE_BOARD
//
// Normal `go test ./...` never compiles or runs this. It exists so the
// connector's status in docs/completion-matrix.md can say "live-tested" on the
// strength of an actual credentialed run rather than a mock.
package source

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func liveBoard(t *testing.T) (string, string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(trelloAPIKeyEnv)) == "" || strings.TrimSpace(os.Getenv(trelloReadTokenEnv)) == "" {
		t.Skip("TRELLO_API_KEY / TRELLO_READ_TOKEN not set; skipping live Trello test")
	}
	ownerIdentity := strings.TrimSpace(os.Getenv(trelloOwnerIdentityEnv))
	if _, valid := normalizedTrelloMongoID(os.Getenv(trelloAccountMemberIDEnv)); ownerIdentity == "" || !valid {
		t.Skip(trelloOwnerIdentityEnv + " and a valid " + trelloAccountMemberIDEnv + " are required; skipping live Trello test")
	}
	board := strings.TrimSpace(os.Getenv("TRELLO_LIVE_BOARD"))
	if board == "" {
		t.Skip("TRELLO_LIVE_BOARD not set; skipping live Trello test")
	}
	return board, ownerIdentity
}

func liveTrelloSource(t *testing.T) *models.ConnectedSource {
	t.Helper()
	board, ownerIdentity := liveBoard(t)
	return &models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: ownerIdentity, ConnectorKey: trelloConnectorKey,
		Name: "Live Trello board", Category: "project_board", Enabled: true,
		Status: "active", SyncFrequency: "manual", SyncTarget: board,
		DefaultProjectKey: "018-HAI",
	}
}

// TestLiveTrelloSyncAgainstRealBoard performs a full sync against a real board
// and asserts the contract the review asked to see: real cards ingested, source
// provenance retained, an audit trail written, and the cursor advanced.
func TestLiveTrelloSyncAgainstRealBoard(t *testing.T) {
	source := liveTrelloSource(t)
	sourceID := source.ID
	repo := newFakeSourceRepo(source)
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

// TestLiveTrelloIncrementalSyncSkipsUnchanged proves the cursor replays its
// high-water cards idempotently: stable cards may be seen again, but persisted
// source and extraction counts and content must remain unchanged.
func TestLiveTrelloIncrementalSyncSkipsUnchanged(t *testing.T) {
	source := liveTrelloSource(t)
	sourceID := source.ID
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})
	type rawState struct {
		Title, Content, Metadata, SourceURI, ContentHash, ProjectKey, ItemType string
	}
	type extractionState struct {
		Text, Summary, Entities, Dates, Tasks, Decisions, FollowUps  string
		SourceURI, SourceLabel, ContentHash, ContentType, ProjectKey string
		Sensitive, Uncertain, Archived                               bool
	}
	type persistedSnapshot struct {
		rawCount, extractionCount int
		rawItems                  map[string]rawState
		extractions               map[string]extractionState
	}
	snapshot := func() (persistedSnapshot, error) {
		rawItems, err := repo.FindRawItems(sourceID)
		if err != nil {
			return persistedSnapshot{}, err
		}
		rawByID := make(map[string]rawState, len(rawItems))
		for _, item := range rawItems {
			rawByID[item.ExternalID] = rawState{
				Title: item.Title, Content: item.Content, Metadata: item.Metadata,
				SourceURI: item.SourceURI, ContentHash: item.ContentHash,
				ProjectKey: item.ProjectKey, ItemType: item.ItemType,
			}
		}
		extractions, err := repo.FindExtractionsForSources([]uuid.UUID{sourceID}, "", true)
		if err != nil {
			return persistedSnapshot{}, err
		}
		extractionsByRawItem := make(map[string]extractionState, len(extractions))
		for _, extraction := range extractions {
			extractionsByRawItem[extraction.RawItemID.String()] = extractionState{
				Text: extraction.Text, Summary: extraction.Summary, Entities: extraction.Entities,
				Dates: extraction.Dates, Tasks: extraction.Tasks, Decisions: extraction.Decisions,
				FollowUps: extraction.FollowUps, SourceURI: extraction.SourceURI,
				SourceLabel: extraction.SourceLabel, ContentHash: extraction.ContentHash,
				ContentType: extraction.ContentType, ProjectKey: extraction.ProjectKey,
				Sensitive: extraction.Sensitive, Uncertain: extraction.Uncertain, Archived: extraction.Archived,
			}
		}
		return persistedSnapshot{
			rawCount: len(rawItems), rawItems: rawByID,
			extractionCount: len(extractions), extractions: extractionsByRawItem,
		}, nil
	}

	first, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("first live sync: %v", err)
	}
	if first.Job.ItemsSeen == 0 {
		t.Fatal("first sync ingested nothing; cannot assess incrementality")
	}
	before, err := snapshot()
	if err != nil {
		t.Fatalf("snapshot first sync: %v", err)
	}

	second, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("second live sync: %v", err)
	}
	t.Logf("incremental: first=%d second=%d added=%d updated=%d", first.Job.ItemsSeen, second.Job.ItemsSeen, second.Job.ItemsAdded, second.Job.ItemsUpdated)
	if second.Job.ItemsAdded != 0 || second.Job.ItemsUpdated != second.Job.ItemsSeen || second.Job.ItemsFailed != 0 {
		t.Fatalf("second sync counts: seen=%d added=%d updated=%d failed=%d; want replay-only updates with no additions or failures", second.Job.ItemsSeen, second.Job.ItemsAdded, second.Job.ItemsUpdated, second.Job.ItemsFailed)
	}
	if second.Job.CursorAfter != first.Job.CursorAfter {
		t.Fatalf("cursor moved from %q to %q although the persisted snapshot should be stable", first.Job.CursorAfter, second.Job.CursorAfter)
	}
	after, err := snapshot()
	if err != nil {
		t.Fatalf("snapshot second sync: %v", err)
	}
	if after.rawCount != before.rawCount || !reflect.DeepEqual(after.rawItems, before.rawItems) {
		t.Fatalf("persisted raw item count/content changed across boundary replay: before=%d after=%d", before.rawCount, after.rawCount)
	}
	if after.extractionCount != before.extractionCount || !reflect.DeepEqual(after.extractions, before.extractions) {
		t.Fatalf("persisted extraction count/content changed across boundary replay: before=%d after=%d", before.extractionCount, after.extractionCount)
	}
}

// TestLiveTrelloTokenIsReadOnly asserts at the credential level that the token
// cannot write. This is the least-privilege guarantee, verified against Trello
// rather than assumed.
func TestLiveTrelloTokenIsReadOnly(t *testing.T) {
	_, _ = liveBoard(t)
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
	if err := trelloGetJSON(base, key, token, "/1/tokens/"+token, nil, &info); err != nil {
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
