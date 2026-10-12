package accountfeed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func testFeed(path string) Feed {
	return Feed{
		ID:           uuid.New(),
		Name:         "inbox",
		Provider:     "local",
		AccountLabel: "primary",
		SourceType:   SourceLocalJSONFile,
		Path:         path,
		OwnerUserID:  "user-1",
		WorkspaceID:  "local",
		Enabled:      true,
	}
}

func TestLocalFileReaderReadsAndValidates(t *testing.T) {
	dir := t.TempDir()
	body := `[
	  {"externalId":"a1","title":"Review invoice","body":"Please review","operationType":"review_invoice"},
	  {"externalId":"a2","title":"Draft reply","body":"Reply to client"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "feed.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewLocalFileReader(testFeed("feed.json"), dir)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	items, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].RawJSON == "" {
		t.Fatalf("raw JSON must be preserved for evidence")
	}
}

func TestLocalFileReaderRejectsUnsafePath(t *testing.T) {
	if _, err := NewLocalFileReader(testFeed("../escape.json"), t.TempDir()); err == nil {
		t.Fatalf("path traversal must be rejected")
	}
}

func TestLocalFileReaderRejectsInvalidItem(t *testing.T) {
	dir := t.TempDir()
	// Missing title on the second item.
	body := `[{"externalId":"a1","title":"ok"},{"externalId":"a2"}]`
	if err := os.WriteFile(filepath.Join(dir, "feed.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ := NewLocalFileReader(testFeed("feed.json"), dir)
	if _, err := r.Read(context.Background()); err == nil {
		t.Fatalf("invalid item must be rejected")
	}
}

func TestLocalFileReaderRejectsOversizedFeedBeforeParsingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oversized.json"), []byte(strings.Repeat("x", maxFeedBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewLocalFileReader(testFeed("oversized.json"), dir)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	_, err = r.Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("oversized reader error=%v", err)
	}
}

func TestToOperationInputIsDeterministic(t *testing.T) {
	f := testFeed("feed.json")
	it := FeedItem{ExternalID: "a1", Title: "Review", Body: "content", Metadata: map[string]any{"z": 1, "a": 2}}
	in1, err := f.ToOperationInput(it)
	if err != nil {
		t.Fatalf("to input: %v", err)
	}
	in2, _ := f.ToOperationInput(it)
	if in1.DedupeKey != in2.DedupeKey || in1.SourceRevisionHash != in2.SourceRevisionHash {
		t.Fatalf("same item must produce a stable dedupe key + revision hash")
	}
	// A changed body must change the revision hash (stale approvals invalidated).
	it2 := it
	it2.Body = "different"
	in3, _ := f.ToOperationInput(it2)
	if in3.SourceRevisionHash == in1.SourceRevisionHash {
		t.Fatalf("changed body must change the revision hash")
	}
}
