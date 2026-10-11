package accountfeed

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

type disableAfterFeedListRepository struct {
	RegistryRepository
	feed Feed
}

func (r *disableAfterFeedListRepository) List(ctx context.Context, scope FeedScope) ([]FeedRecord, error) {
	rows, err := r.RegistryRepository.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	disabled := false
	if _, err := r.RegistryRepository.Patch(ctx, scope, r.feed.ID, FeedPatch{Enabled: &disabled}, newFeedAudit(r.feed.ID, "updated", "disabled before claim", time.Now())); err != nil {
		return nil, err
	}
	return rows, nil
}

func TestBulkSyncRespectsDisableCommittedAfterList(t *testing.T) {
	store := NewMemoryRegistryRepository()
	ledger := operations.NewMemoryRepository()
	decorated := &disableAfterFeedListRepository{RegistryRepository: store}
	registry := persistenceRegistry(t, decorated, ledger, persistenceFixture(t))
	feed, err := registry.RegisterContext(t.Context(), persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	decorated.feed = feed
	reports, err := registry.SyncDueContext(t.Context(), feedScope(feed))
	if err != nil || len(reports) != 0 {
		t.Fatalf("disabled feed imported from stale list: %+v %v", reports, err)
	}
	row := persistenceRecord(t, store, feed)
	if row.SyncToken != nil || row.LastAttemptAt != nil || row.Feed.Enabled {
		t.Fatalf("disabled feed claimed: %+v", row)
	}
	audits := persistenceAudit(t, store, feed)
	if len(audits) != 2 || audits[0].EventType != "updated" {
		t.Fatalf("unstarted bulk run advertised audit: %+v", audits)
	}
	rows, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("disabled run recorded operations: %+v %v", rows, err)
	}
	// Disabled canonical sources cannot be reactivated by a one-off import.
	report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
	if !errors.Is(err, ErrFeedDisabled) || report.Recorded || report.OperationsCreated != 0 {
		t.Fatalf("manual disabled-feed import bypassed revocation: %+v %v", report, err)
	}
	persistenceUnchanged(t, store, feed, row, audits)
}

func TestEnabledClaimChecksCurrentConfigurationBeforeAudit(t *testing.T) {
	store := NewMemoryRegistryRepository()
	feed := persistenceSeed()
	feed.Enabled = false
	if _, err := store.Register(t.Context(), feed, newFeedAudit(feed.ID, "registered", "disabled feed", time.Now())); err != nil {
		t.Fatal(err)
	}
	before := persistenceRecord(t, store, feed)
	audits := persistenceAudit(t, store, feed)
	if _, err := store.BeginEnabledSync(t.Context(), feedScope(feed), feed.ID, uuid.New(), time.Now()); !errors.Is(err, ErrFeedDisabled) {
		t.Fatalf("disabled claim accepted: %v", err)
	}
	persistenceUnchanged(t, store, feed, before, audits)
}
