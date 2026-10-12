package accountfeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"automation-hub-backend/internal/operations"
)

func TestTrelloCardProjectKeySurvivesGenericFeedSync(t *testing.T) {
	root := t.TempDir()
	items := []GenericItem{
		{
			ExternalID: "card-1", Title: "Prepare hearing bundle", Content: "Collect evidence",
			ItemType: string(ItemCard), Provider: string(ProviderTrello), ProjectKey: "vivare-dispute",
		},
		{
			ExternalID: "card-2", Title: "Review client quote", Content: "Confirm scope",
			ItemType: string(ItemCard), Provider: string(ProviderTrello),
		},
	}
	body, err := json.Marshal(GenericFeed{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feed.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	ops := operations.NewService(operations.NewMemoryRepository())
	registry := NewRegistry(ops, nil, FetchOptions{FeedsRoot: root})
	feed := persistenceSeed()
	feed.ProjectKey = "default-project"
	registered, err := registry.Register(feed)
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.SyncContext(t.Context(), feedScope(registered), registered.ID)
	if err != nil || !report.Recorded || report.OperationsCreated != 2 || len(report.Errors) != 0 {
		t.Fatalf("sync did not ingest Trello cards: report=%+v err=%v", report, err)
	}

	rows, err := ops.List(operations.Filter{OwnerUserID: registered.OwnerUserID, WorkspaceID: registered.WorkspaceID})
	if err != nil || len(rows) != 2 {
		t.Fatalf("operations missing: rows=%+v err=%v", rows, err)
	}
	projects := make(map[string]string, len(rows))
	for _, row := range rows {
		projects[row.SourceExternalID] = row.ProjectKey
	}
	if projects["card-1"] != "vivare-dispute" || projects["card-2"] != "default-project" {
		t.Fatalf("per-card project mapping lost or fallback changed: %+v", projects)
	}
}
