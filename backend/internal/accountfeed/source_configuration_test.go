package accountfeed

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

func bindTestSourceAuthority(repo RegistryRepository, service *operations.Service) error {
	binder, ok := repo.(interface {
		BindSourceAuthority(*operations.Service) error
	})
	if !ok {
		return ErrFeedStorageUnavailable
	}
	return binder.BindSourceAuthority(service)
}

func (r *persistenceFailingRepository) BindSourceAuthority(service *operations.Service) error {
	return bindTestSourceAuthority(r.RegistryRepository, service)
}

func (r *disableAfterFeedListRepository) BindSourceAuthority(service *operations.Service) error {
	return bindTestSourceAuthority(r.RegistryRepository, service)
}

func TestRegistryPatchImmediatelyRevokesObservationAndPreservesHistory(t *testing.T) {
	registry, service, repo, feed, path := newObservationWiringRegistry(t)
	observationWiringWrite(t, path, observationWiringBytes(t, "original", "source body", false))
	observationWiringSync(t, registry, feed, 1, 0)
	before := observationWiringOperations(t, service, feed)
	changedName := "changed canonical feed"
	changed, err := registry.PatchContext(t.Context(), feedScope(feed), feed.ID, FeedPatch{Name: &changedName})
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []operations.SourceObservationStart{feed.SourceObservationStart(), func() operations.SourceObservationStart {
		start := feed.SourceObservationStart()
		start.RegistryManaged = false
		return start
	}()} {
		if _, err := repo.BeginSourceObservation(t.Context(), stale); !errors.Is(err, operations.ErrSourceHeadSuperseded) {
			t.Fatalf("stale snapshot reminted canonical origin: %v", err)
		}
	}
	claim, err := service.ClaimNext(t.Context(), feed.OwnerUserID, feed.WorkspaceID, uuid.New(), time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("old head claimable before new sync: %+v / %v", claim, err)
	}
	after := observationWiringOperations(t, service, feed)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].Version != before[0].Version || after[0].EvidenceJSON != before[0].EvidenceJSON {
		t.Fatal("config revocation destroyed or rewrote operation history")
	}
	observationWiringSync(t, registry, changed, 0, 1)
}

func TestRegistryDisableDuringOwnedSyncRevokesAndRetainsSyncToken(t *testing.T) {
	registry, service, repo, feed, _ := newObservationWiringRegistry(t)
	store := registry.repo.(*MemoryRegistryRepository)
	token := uuid.New()
	if _, err := store.BeginSync(t.Context(), feedScope(feed), feed.ID, token, time.Now()); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := registry.PatchContext(t.Context(), feedScope(feed), feed.ID, FeedPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("disable refused while sync owned: %v", err)
	}
	row, err := store.Get(t.Context(), feedScope(feed), feed.ID)
	if err != nil || row.Feed.Enabled || row.SyncToken == nil || *row.SyncToken != token {
		t.Fatalf("disable erased active attempt or failed: %+v / %v", row, err)
	}
	if _, err := repo.BeginSourceObservation(t.Context(), feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) {
		t.Fatalf("disabled origin observed: %v", err)
	}
	if rows := observationWiringOperations(t, service, feed); len(rows) != 0 {
		t.Fatal("disabled sync created operations")
	}
}

func TestRegistryCanceledPatchLeavesCanonicalAuthorityUnchanged(t *testing.T) {
	registry, _, repo, feed, _ := newObservationWiringRegistry(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	disabled := false
	if _, err := registry.PatchContext(ctx, feedScope(feed), feed.ID, FeedPatch{Enabled: &disabled}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled config mutation accepted: %v", err)
	}
	if _, err := repo.BeginSourceObservation(t.Context(), feed.SourceObservationStart()); err != nil {
		t.Fatalf("rejected mutation revoked unchanged config: %v", err)
	}
}

func TestMemoryRegistryLockWaitIsCanceledWithoutConfigurationMutation(t *testing.T) {
	for _, path := range []string{"get", "register", "patch", "begin_sync"} {
		t.Run(path, func(t *testing.T) {
			store := NewMemoryRegistryRepository()
			feed, err := store.Register(t.Context(), persistenceSeed(), newFeedAudit(uuid.New(), "registered", "fixture", time.Now()))
			if err != nil {
				t.Fatal(err)
			}
			before := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store.mu.Lock()
			locked := true
			defer func() {
				if locked {
					store.mu.Unlock()
				}
			}()
			done := make(chan error, 1)
			go func() {
				var err error
				switch path {
				case "get":
					_, err = store.Get(ctx, feedScope(feed), feed.ID)
				case "register":
					_, err = store.Register(ctx, feed, newFeedAudit(feed.ID, "registered", "duplicate", time.Now()))
				case "patch":
					disabled := false
					_, err = store.Patch(ctx, feedScope(feed), feed.ID, FeedPatch{Enabled: &disabled}, newFeedAudit(feed.ID, "updated", "disable", time.Now()))
				case "begin_sync":
					_, err = store.BeginSync(ctx, feedScope(feed), feed.ID, uuid.New(), time.Now())
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("lock wait returned before cancellation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lock wait ignored cancellation: %v", err)
				}
			case <-time.After(time.Second):
				store.mu.Unlock()
				locked = false
				<-done
				t.Fatal("lock wait continued after cancellation")
			}
			store.mu.Unlock()
			locked = false
			persistenceUnchanged(t, store, feed, before, audit)
		})
	}
}
