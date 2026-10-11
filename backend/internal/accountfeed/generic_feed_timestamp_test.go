package accountfeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
)

func TestGenericFeedPreservesReceiptTimestampIntoLedger(t *testing.T) {
	root := t.TempDir()
	body := `{"items":[{"externalId":"email-one","title":"Evidence request","content":"Please prepare documents","itemType":"email","provider":"gmail","receivedAt":"2026-09-30T23:05:06.123456+02:00"}]}`
	if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := operations.NewService(operations.NewMemoryRepository())
	registry := NewRegistry(ops, nil, FetchOptions{FeedsRoot: root})
	feed, err := registry.Register(Feed{Name: "history", Provider: "generic_json_feed", SourceType: SourceLocalJSONFile, Path: "feed.json", OwnerUserID: "owner", WorkspaceID: "local", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.OperationsCreated != 1 || len(report.Errors) != 0 {
		t.Fatalf("timestamp import failed: %+v %v", report, err)
	}
	rows, err := ops.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger import absent: %d %v", len(rows), err)
	}
	expected := time.Date(2026, 9, 30, 21, 5, 6, 123456000, time.UTC)
	if rows[0].SourceReceivedAt == nil || !rows[0].SourceReceivedAt.Equal(expected) || rows[0].SourceReceivedAt.Location() != time.UTC {
		t.Fatalf("source receipt timestamp lost: %v", rows[0].SourceReceivedAt)
	}
	var evidence struct {
		ReceivedAt string `json:"receivedAt"`
	}
	if err := json.Unmarshal([]byte(rows[0].EvidenceJSON), &evidence); err != nil || evidence.ReceivedAt != "2026-09-30T23:05:06.123456+02:00" {
		t.Fatalf("original timestamp evidence changed: %+v %v", evidence, err)
	}
}

func TestGenericFeedRejectsAmbiguousReceiptTimestamp(t *testing.T) {
	for _, value := range []string{"", "2026-09-30", "2026-09-30T23:05:06", "2026-99-30T23:05:06Z", "yesterday"} {
		t.Run(value, func(t *testing.T) {
			item := GenericItem{ExternalID: "one", Title: "one", ItemType: "email", Provider: "gmail", ReceivedAt: &value}
			body, err := json.Marshal(GenericFeed{Cursor: "not-acknowledged", Items: []GenericItem{item}})
			if err != nil {
				t.Fatal(err)
			}
			if parsed, err := ParseGenericFeed(body, 0, 0); err == nil || len(parsed.Items) != 0 || parsed.Cursor != "" {
				t.Fatalf("ambiguous timestamp admitted: %+v %v", parsed, err)
			}
		})
	}
	item := GenericItem{ExternalID: "one", Title: "one", ItemType: "email", Provider: "gmail"}
	if err := item.Validate(0, 0); err != nil || item.ToFeedItem().ReceivedAt != nil {
		t.Fatalf("optional missing timestamp became invented evidence: %v", err)
	}
}
