package accountfeed

import (
	"context"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

type cancelDuringFeedLookupRepository struct {
	*operations.MemoryRepository
	cancel context.CancelFunc
}

func (r *cancelDuringFeedLookupRepository) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	op, found, err := r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
	r.cancel()
	return op, found, err
}

func TestFeedCancellationStopsIntakeButRecordsOwnedAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ledger := &cancelDuringFeedLookupRepository{MemoryRepository: operations.NewMemoryRepository(), cancel: cancel}
	store := NewMemoryRegistryRepository()
	registry := persistenceRegistry(t, store, ledger, persistenceFixture(t))
	feed, err := registry.RegisterContext(ctx, persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.SyncContext(ctx, feedScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.Cursor != "" || report.OperationsCreated != 0 || report.OperationsRefresh != 0 || len(report.Errors) == 0 {
		t.Fatalf("cancelled intake advertised success or lost cleanup: %+v %v", report, err)
	}
	rows, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("cancelled request created work: %+v %v", rows, err)
	}
	stored := persistenceRecord(t, store, feed)
	if stored.SyncToken != nil || stored.LastSuccessAt != nil || stored.LastAttemptAt == nil {
		t.Fatalf("cancelled attempt state incorrect: %+v", stored)
	}
	audit := persistenceAudit(t, store, feed)
	if len(audit) != 3 || audit[0].EventType != "sync_failed" || audit[1].EventType != "sync_started" {
		t.Fatalf("cancelled attempt lacks truthful audit: %+v", audit)
	}
}
