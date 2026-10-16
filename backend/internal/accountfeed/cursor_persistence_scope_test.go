package accountfeed

import (
	"testing"

	"automation-hub-backend/internal/operations"
)

func TestLocalJSONCursorIsNotPersistedAsHTTPCheckpoint(t *testing.T) {
	store := NewMemoryRegistryRepository()
	reg := persistenceRegistry(t, store, operations.NewMemoryRepository(), persistenceFixture(t))
	seed := persistenceSeed()
	seed.SyncCursor = "legacy-local-checkpoint"
	feed, err := reg.RegisterContext(t.Context(), seed)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reg.SyncContext(t.Context(), persistenceScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.Cursor != "next-42" {
		t.Fatalf("local feed sync report=%+v err=%v", report, err)
	}
	stored := persistenceRecord(t, store, feed)
	if stored.Feed.SyncCursor != "" {
		t.Fatalf("successful local sync did not clear its legacy checkpoint: %q", stored.Feed.SyncCursor)
	}
}
