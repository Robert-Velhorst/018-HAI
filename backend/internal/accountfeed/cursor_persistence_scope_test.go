package accountfeed

import (
	"testing"

	"automation-hub-backend/internal/operations"
)

func TestLocalJSONCursorIsNotPersistedAsHTTPCheckpoint(t *testing.T) {
	store := NewMemoryRegistryRepository()
	reg := persistenceRegistry(t, store, operations.NewMemoryRepository(), persistenceFixture(t))
	feed, err := reg.RegisterContext(t.Context(), persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}

	report, err := reg.SyncContext(t.Context(), persistenceScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.Cursor != "next-42" {
		t.Fatalf("local feed sync report=%+v err=%v", report, err)
	}
	stored := persistenceRecord(t, store, feed)
	if stored.Feed.SyncCursor != "" {
		t.Fatalf("local feed cursor was persisted as an HTTP checkpoint: %q", stored.Feed.SyncCursor)
	}
}
