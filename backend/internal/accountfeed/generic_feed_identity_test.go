package accountfeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/operations"
)

func TestGenericFeedKeepsDistinctProviderAndAccountItems(t *testing.T) {
	root := t.TempDir()
	items := []GenericItem{
		{ExternalID: "same", Title: "Gmail primary", Content: "identical", ItemType: "email", Provider: "gmail", AccountLabel: "primary"},
		{ExternalID: "same", Title: "Gmail other", Content: "identical", ItemType: "email", Provider: "gmail", AccountLabel: "other"},
		{ExternalID: "same", Title: "Github primary", Content: "identical", ItemType: "issue", Provider: "github", AccountLabel: "primary"},
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
	feed, err := registry.Register(persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
		if err != nil || len(report.Errors) != 0 || !report.Recorded || report.ItemsRead != 3 || (attempt == 0 && report.OperationsCreated != 3) || (attempt == 1 && report.OperationsCreated != 0) {
			t.Fatalf("source identities merged or replay duplicated: %+v %v", report, err)
		}
	}
	rows, err := ops.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if err != nil || len(rows) != 3 {
		t.Fatalf("source operations absent: %+v %v", rows, err)
	}
	byURI := make(map[string]string)
	for _, row := range rows {
		byURI[row.SourceURI] = row.Title
	}
	want := map[string]string{"gmail:primary:same": "Gmail primary", "gmail:other:same": "Gmail other", "github:primary:same": "Github primary"}
	if !reflect.DeepEqual(byURI, want) {
		t.Fatalf("source provenance changed: %+v", byURI)
	}
}

func TestGenericFeedIdentityAndSemanticRevisionAreUnambiguous(t *testing.T) {
	feed := persistenceSeed()
	base := GenericItem{ExternalID: "one", Title: "original", Content: "body", ItemType: "email", Provider: "gmail", AccountLabel: "primary"}
	first, err := feed.ToOperationInput(base.ToFeedItem())
	if err != nil {
		t.Fatal(err)
	}
	variants := []GenericItem{base, base, base, base, base}
	variants[0].Title = "changed title"
	variants[1].ItemType = "issue"
	at := time.Now().UTC().Format(time.RFC3339Nano)
	variants[2].ReceivedAt = &at
	variants[3].Provider = "github"
	variants[4].AccountLabel = "other"
	for _, variant := range variants {
		got, err := feed.ToOperationInput(variant.ToFeedItem())
		if err != nil || got.DedupeKey == first.DedupeKey {
			t.Fatalf("distinct source/semantics reused identity: %+v %v", got, err)
		}
	}
	// The historical pipe-joined encoding conflates these field boundaries.
	if idempotency.StructuredFeedItemDedupeKey("gmail", "a|b", "c", "r") == idempotency.StructuredFeedItemDedupeKey("gmail", "a", "b|c", "r") {
		t.Fatal("structured account/external-id boundaries collided")
	}
	base.AccountLabel = ""
	feed.AccountLabel = ""
	unnamed, _ := feed.ToOperationInput(base.ToFeedItem())
	other := persistenceSeed()
	other.AccountLabel = ""
	otherInput, _ := other.ToOperationInput(base.ToFeedItem())
	if unnamed.DedupeKey == otherInput.DedupeKey {
		t.Fatal("unlabelled feeds collided")
	}
}

func TestHistoricalGenericFeedCollisionIsPreservedForReview(t *testing.T) {
	root := t.TempDir()
	item := GenericItem{ExternalID: "same", Title: "current", Content: "unchanged", ItemType: "email", Provider: "gmail", AccountLabel: "primary"}
	body, err := json.Marshal(GenericFeed{Cursor: "must-not-advance", Items: []GenericItem{item}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feed.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	ops := operations.NewService(operations.NewMemoryRepository())
	registry := NewRegistry(ops, nil, FetchOptions{FeedsRoot: root})
	feed, err := registry.Register(persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	legacyItem := item.ToFeedItem()
	legacyItem.Provider, legacyItem.AccountLabel = "", ""
	legacyItem.RawJSON = `{"externalId":"same","title":"historical","content":"unchanged","provider":"gmail","itemType":"email","accountLabel":"primary"}`
	legacyInput, err := feed.ToOperationInput(legacyItem)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the actual historical encoding explicitly; both current readers
	// must use v2 even when an item omits its provider override.
	legacyInput.DedupeKey = legacyInput.LegacyDedupeKey
	legacyInput.LegacyDedupeKey = ""
	legacy, err := ops.Ingest(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := ops.Events(legacy.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.OperationsCreated != 0 || report.OperationsRefresh != 0 || report.Cursor != "" || len(report.Errors) != 1 {
		t.Fatalf("legacy row overwritten/duplicated: %+v %v", report, err)
	}
	stored, err := ops.Get(feed.OwnerUserID, feed.WorkspaceID, legacy.Operation.ID)
	if err != nil || !reflect.DeepEqual(*stored, legacy.Operation) {
		t.Fatalf("historical operation changed: %+v %v", stored, err)
	}
	eventsAfter, err := ops.Events(legacy.Operation.ID)
	if err != nil || !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatalf("historical audit changed: %+v %v", eventsAfter, err)
	}
	rows, err := ops.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("legacy replay created extra work: %+v %v", rows, err)
	}
}

func TestFeedIdentityDoesNotDowngradeForOptionalProvider(t *testing.T) {
	feed := persistenceSeed()
	feed.Provider, feed.AccountLabel = "gmail", "primary"
	base := FeedItem{ExternalID: "same", Title: "original", Body: "body", OperationType: "review_source_item"}
	explicit := base
	explicit.Provider, explicit.AccountLabel = feed.Provider, feed.AccountLabel
	implicitInput, err := feed.ToOperationInput(base)
	if err != nil {
		t.Fatal(err)
	}
	explicitInput, err := feed.ToOperationInput(explicit)
	if err != nil || implicitInput.DedupeKey != explicitInput.DedupeKey || implicitInput.LegacyDedupeKey != explicitInput.LegacyDedupeKey || implicitInput.LegacyDedupeKey == "" {
		t.Fatalf("optional override changed identity contract: %+v %+v %v", implicitInput, explicitInput, err)
	}
	ops := operations.NewService(operations.NewMemoryRepository())
	first, err := ops.IngestContext(t.Context(), explicitInput)
	if err != nil || !first.Created {
		t.Fatalf("initial intake failed: %+v %v", first, err)
	}
	replay, err := ops.IngestContext(t.Context(), implicitInput)
	if err != nil || replay.Created || replay.Operation.ID != first.Operation.ID {
		t.Fatalf("provider omission duplicated active work: %+v %v", replay, err)
	}
	base.AccountLabel = "other"
	other, err := feed.ToOperationInput(base)
	if err != nil || other.DedupeKey == implicitInput.DedupeKey || other.SourceURI != "gmail:other:same" {
		t.Fatalf("implicit provider lost item account: %+v %v", other, err)
	}
	base.AccountLabel = ""
	base.Title = "updated title"
	revision, err := feed.ToOperationInput(base)
	if err != nil || revision.DedupeKey == implicitInput.DedupeKey {
		t.Fatalf("legacy reader omitted semantic revision: %+v %v", revision, err)
	}
}

func TestFeedIdentityUsesTimestampInstantNotOffset(t *testing.T) {
	feed := persistenceSeed()
	at := time.Date(2026, 10, 1, 14, 15, 0, 42, time.FixedZone("offset", 2*60*60))
	base := FeedItem{ExternalID: "same", Title: "original", Body: "body", ReceivedAt: &at}
	first, err := feed.ToOperationInput(base)
	if err != nil {
		t.Fatal(err)
	}
	utc := at.UTC()
	base.ReceivedAt = &utc
	replay, err := feed.ToOperationInput(base)
	if err != nil || first.DedupeKey != replay.DedupeKey || first.SourceReceivedAt == nil || first.SourceReceivedAt.Location() != time.UTC {
		t.Fatalf("same timestamp instant changed identity: %+v %+v %v", first, replay, err)
	}
}
