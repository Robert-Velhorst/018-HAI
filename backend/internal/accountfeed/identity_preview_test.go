package accountfeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

func previewFixture(t *testing.T, count int) (*Registry, *MemoryRegistryRepository, *operations.MemoryRepository, Feed, []GenericItem, string) {
	t.Helper()
	root := t.TempDir()
	items := make([]GenericItem, count)
	for i := range items {
		items[i] = GenericItem{ExternalID: fmt.Sprintf("item-%d", i), Title: fmt.Sprintf("Review %d", i), ItemType: "email", Provider: "gmail"}
	}
	data, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feed.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, ledger := NewMemoryRegistryRepository(), operations.NewMemoryRepository()
	reg := persistenceRegistry(t, store, ledger, root)
	feed, err := reg.RegisterContext(t.Context(), persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	return reg, store, ledger, feed, items, root
}

func previewLedgerSnapshot(t *testing.T, ledger *operations.MemoryRepository) ([]models.Operation, map[uuid.UUID][]models.OperationEvent) {
	t.Helper()
	ops, err := ledger.List(operations.Filter{OwnerUserID: "owner-a", WorkspaceID: "workspace-a", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	events := make(map[uuid.UUID][]models.OperationEvent)
	for _, op := range ops {
		events[op.ID], err = ledger.ListEvents(op.ID, 1000)
		if err != nil {
			t.Fatal(err)
		}
	}
	return ops, events
}

func TestIdentityPreviewReportsAllStatesWithoutMutatingSourcesOrLedger(t *testing.T) {
	reg, store, ledger, feed, items, _ := previewFixture(t, 4)
	states := []string{"unseen", "canonical", "historical", "coexisting"}
	for i, item := range items {
		in, err := feed.ToOperationInput(item.ToFeedItem())
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{in.DedupeKey, in.LegacyDedupeKey} {
			if (key == in.DedupeKey && (i == 1 || i == 3)) || (key == in.LegacyDedupeKey && (i == 2 || i == 3)) {
				seed := in
				seed.DedupeKey, seed.LegacyDedupeKey = key, ""
				if _, err := operations.NewService(ledger).Ingest(seed); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
	ops, events := previewLedgerSnapshot(t, ledger)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("source", 7200))
	reg.now = func() time.Time { return at }
	got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FeedID != feed.ID.String() || got.Scope != "current_local_feed_active_operations" || got.HistoricalInventoryComplete || got.Truncated ||
		got.ItemsObserved != 4 || got.ItemsInspected != 4 || got.Counts != (IdentityPreviewCounts{1, 1, 1, 1}) || !got.ObservedAt.Equal(at) || got.ObservedAt.Location() != time.UTC {
		t.Fatalf("incorrect scope/freshness: %+v", got)
	}
	for i, item := range got.Items {
		if item.State != states[i] || item.ExternalID != items[i].ExternalID {
			t.Fatalf("state %d: %+v", i, item)
		}
		if (item.CanonicalOperationID != "") != (i == 1 || i == 3) || (item.HistoricalOperationID != "") != (i == 2 || i == 3) {
			t.Fatalf("wrong record links: %+v", item)
		}
	}
	persistenceUnchanged(t, store, feed, before, audit)
	afterOps, afterEvents := previewLedgerSnapshot(t, ledger)
	if !reflect.DeepEqual(ops, afterOps) || !reflect.DeepEqual(events, afterEvents) {
		t.Fatal("inspection modified operations or audit")
	}
}

func TestIdentityPreviewBoundedAndEmptyScope(t *testing.T) {
	for _, count := range []int{0, 100, 103} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reg, _, _, feed, _, _ := previewFixture(t, count)
			got, err := reg.PreviewSourceIdentity(nil, persistenceScope(feed), feed.ID)
			if err != nil || got.ItemsObserved != count || got.ItemsInspected != min(count, 100) || len(got.Items) != min(count, 100) ||
				got.Counts.Unseen != min(count, 100) || got.Truncated != (count > 100) || got.Items == nil {
				t.Fatalf("invalid bounded result: %+v err=%v", got, err)
			}
		})
	}
}

func TestIdentityPreviewNeverContactsHTTPProvider(t *testing.T) {
	reg, store, _, _, _, _ := previewFixture(t, 1)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	reg.opts.AllowHTTP = true
	feed := persistenceSeed()
	feed.SourceType, feed.Path, feed.URL = SourceHTTPJSONFeed, "", server.URL+"/feed.json"
	feed, err := reg.RegisterContext(t.Context(), feed)
	if err != nil {
		t.Fatal(err)
	}
	before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
	got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
	if !errors.Is(err, ErrIdentityPreviewUnsupported) || calls.Load() != 0 || !reflect.DeepEqual(got, IdentityPreview{}) {
		t.Fatalf("provider access: %+v / %v / %d", got, err, calls.Load())
	}
	persistenceUnchanged(t, store, feed, before, audit)
}

func TestIdentityPreviewRejectsWrongScopeBusyAndUnreadableSources(t *testing.T) {
	for _, reason := range []string{"owner", "workspace", "invalid_scope", "missing", "malformed", "busy", "nil_ledger", "canceled"} {
		t.Run(reason, func(t *testing.T) {
			reg, store, _, feed, _, root := previewFixture(t, 1)
			scope := persistenceScope(feed)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ErrFeedInvalid
			switch reason {
			case "owner":
				scope.OwnerUserID = "other-owner"
				want = ErrFeedNotFound
			case "workspace":
				scope.WorkspaceID = "other-workspace"
				want = ErrFeedNotFound
			case "invalid_scope":
				scope.OwnerUserID = ""
			case "missing":
				// Change only this test's registered path, not a shared or live file.
				store.mu.Lock()
				row := store.feeds[feed.ID]
				row.Feed.Path = "absent.json"
				store.feeds[feed.ID] = row
				store.mu.Unlock()
			case "malformed":
				if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(`{"items":[`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "busy":
				if _, err := store.BeginSync(ctx, scope, feed.ID, uuid.New(), reg.now()); err != nil {
					t.Fatal(err)
				}
				want = ErrFeedSyncBusy
			case "nil_ledger":
				reg.ops = nil
				want = ErrFeedStorageUnavailable
			case "canceled":
				cancel()
				want = context.Canceled
			}
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			got, err := reg.PreviewSourceIdentity(ctx, scope, feed.ID)
			if !errors.Is(err, want) || !reflect.DeepEqual(got, IdentityPreview{}) {
				t.Fatalf("%s: %+v / %v", reason, got, err)
			}
			if err != nil && strings.Contains(err.Error(), root) {
				t.Fatal("error exposed private source root")
			}
			persistenceUnchanged(t, store, feed, before, audit)
		})
	}
}

type previewCancelLookup struct {
	*operations.MemoryRepository
	cancel context.CancelFunc
	calls  int
}

func (r *previewCancelLookup) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	r.calls++
	op, found, err := r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
	r.cancel()
	return op, found, err
}

func TestIdentityPreviewCancellationDuringLookupReturnsNoPartialResult(t *testing.T) {
	reg, store, ledger, feed, _, _ := previewFixture(t, 3)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &previewCancelLookup{MemoryRepository: ledger, cancel: cancel}
	reg.ops = operations.NewService(probe)
	before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
	got, err := reg.PreviewSourceIdentity(ctx, persistenceScope(feed), feed.ID)
	if !errors.Is(err, context.Canceled) || probe.calls != 1 || !reflect.DeepEqual(got, IdentityPreview{}) {
		t.Fatalf("partial cancellation: %+v / %v / %d", got, err, probe.calls)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	ops, _ := previewLedgerSnapshot(t, ledger)
	if len(ops) != 0 {
		t.Fatal("inspection created operations")
	}
}

func TestIdentityPreviewLabelsAreBoundedAndSecretRedacted(t *testing.T) {
	if got := boundedIdentityLabel(strings.Repeat("x", 300), 256); len(got) != 259 || !strings.HasSuffix(got, "...") {
		t.Fatalf("unbounded label: %d", len(got))
	}
	if got := boundedIdentityLabel("password=must-not-be-shown", 160); strings.Contains(got, "must-not-be-shown") {
		t.Fatalf("unredacted label: %q", got)
	}
}

type previewRegistryGetProbe struct {
	RegistryRepository
	calls    int
	scopes   []FeedScope
	ids      []uuid.UUID
	afterGet func(context.Context, FeedRecord, int) (FeedRecord, error)
}

func (r *previewRegistryGetProbe) Get(ctx context.Context, scope FeedScope, id uuid.UUID) (FeedRecord, error) {
	r.calls++
	r.scopes = append(r.scopes, scope)
	r.ids = append(r.ids, id)
	row, err := r.RegistryRepository.Get(ctx, scope, id)
	if err != nil || r.afterGet == nil {
		return row, err
	}
	return r.afterGet(ctx, row, r.calls)
}

type previewLookupHookRepository struct {
	*operations.MemoryRepository
	calls int
	hook  func(context.Context, string) error
}

func (r *previewLookupHookRepository) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	r.calls++
	if r.hook != nil {
		if err := r.hook(ctx, key); err != nil {
			return nil, false, err
		}
	}
	return r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
}

func previewSeedCanonicalItem(t *testing.T, ledger *operations.MemoryRepository, feed Feed, item GenericItem) {
	t.Helper()
	in, err := feed.ToOperationInput(item.ToFeedItem())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.NewService(ledger).Ingest(in); err != nil {
		t.Fatal(err)
	}
}

func previewLedgerUnchanged(t *testing.T, ledger *operations.MemoryRepository, before []models.Operation, events map[uuid.UUID][]models.OperationEvent) {
	t.Helper()
	after, afterEvents := previewLedgerSnapshot(t, ledger)
	if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(afterEvents, events) {
		t.Fatal("inspection modified operations or audit")
	}
}

func previewRegistryReads(t *testing.T, probe *previewRegistryGetProbe, feed Feed, count int) {
	t.Helper()
	if probe.calls != count {
		t.Fatalf("registry Get calls=%d, want %d", probe.calls, count)
	}
	for i := range probe.scopes {
		if probe.scopes[i] != persistenceScope(feed) || probe.ids[i] != feed.ID {
			t.Fatalf("registry observation %d lost scope or identity: %+v / %s", i, probe.scopes[i], probe.ids[i])
		}
	}
}

func TestIdentityPreviewFetchCancellationPreservesContextError(t *testing.T) {
	reg, store, ledger, feed, items, _ := previewFixture(t, 2)
	previewSeedCanonicalItem(t, ledger, feed, items[0])
	before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
	ops, events := previewLedgerSnapshot(t, ledger)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := &previewRegistryGetProbe{RegistryRepository: store}
	registry.afterGet = func(_ context.Context, row FeedRecord, _ int) (FeedRecord, error) {
		// The initial context check and scoped Get succeed; fetching sees cancellation.
		cancel()
		return row, nil
	}
	lookup := &previewLookupHookRepository{MemoryRepository: ledger}
	reg.repo, reg.ops = registry, operations.NewService(lookup)
	got, err := reg.PreviewSourceIdentity(ctx, persistenceScope(feed), feed.ID)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrFeedInvalid) || !reflect.DeepEqual(got, IdentityPreview{}) {
		t.Fatalf("fetch cancellation was reclassified or returned a report: %+v / %v", got, err)
	}
	previewRegistryReads(t, registry, feed, 1)
	if lookup.calls != 0 {
		t.Fatalf("canceled fetch reached ledger: %d lookups", lookup.calls)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	previewLedgerUnchanged(t, ledger, ops, events)
}

func TestIdentityPreviewLaterItemLookupFailureDiscardsAccumulatedReport(t *testing.T) {
	for _, boundary := range []string{"canonical", "historical"} {
		t.Run(boundary, func(t *testing.T) {
			reg, store, ledger, feed, items, _ := previewFixture(t, 3)
			previewSeedCanonicalItem(t, ledger, feed, items[0])
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			ops, events := previewLedgerSnapshot(t, ledger)
			in, err := feed.ToOperationInput(items[1].ToFeedItem())
			if err != nil {
				t.Fatal(err)
			}
			key, wantCalls := in.DedupeKey, 3
			if boundary == "historical" {
				key, wantCalls = in.LegacyDedupeKey, 4
			}
			injected := errors.New("later item lookup unavailable")
			lookup := &previewLookupHookRepository{MemoryRepository: ledger}
			lookup.hook = func(_ context.Context, gotKey string) error {
				if gotKey == key {
					return injected
				}
				return nil
			}
			registry := &previewRegistryGetProbe{RegistryRepository: store}
			reg.repo, reg.ops = registry, operations.NewService(lookup)
			got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
			if !errors.Is(err, injected) || !reflect.DeepEqual(got, IdentityPreview{}) || lookup.calls != wantCalls {
				t.Fatalf("later %s failure retained earlier items/counts: %+v / %v / %d", boundary, got, err, lookup.calls)
			}
			previewRegistryReads(t, registry, feed, 1)
			persistenceUnchanged(t, store, feed, before, audit)
			previewLedgerUnchanged(t, ledger, ops, events)
		})
	}
}

func TestIdentityPreviewRejectsInconsistentRegistryFeed(t *testing.T) {
	for _, observation := range []int{1, 2} {
		for _, field := range []string{"owner", "workspace", "id"} {
			t.Run(fmt.Sprintf("get_%d/%s", observation, field), func(t *testing.T) {
				reg, store, ledger, feed, items, _ := previewFixture(t, 2)
				previewSeedCanonicalItem(t, ledger, feed, items[0])
				before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
				ops, events := previewLedgerSnapshot(t, ledger)
				registry := &previewRegistryGetProbe{RegistryRepository: store}
				registry.afterGet = func(_ context.Context, row FeedRecord, call int) (FeedRecord, error) {
					if call == observation {
						switch field {
						case "owner":
							row.Feed.OwnerUserID = "other-owner"
						case "workspace":
							row.Feed.WorkspaceID = "other-workspace"
						case "id":
							row.Feed.ID = uuid.New()
						}
					}
					return row, nil
				}
				lookup := &previewLookupHookRepository{MemoryRepository: ledger}
				reg.repo, reg.ops = registry, operations.NewService(lookup)
				want, wantLookups := ErrFeedStorageUnavailable, 0
				if observation == 2 {
					want, wantLookups = ErrIdentityPreviewChanged, 4
				}
				got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
				if !errors.Is(err, want) || !reflect.DeepEqual(got, IdentityPreview{}) || lookup.calls != wantLookups {
					t.Fatalf("inconsistent registry result was not rejected: %+v / %v / %d", got, err, lookup.calls)
				}
				previewRegistryReads(t, registry, feed, observation)
				persistenceUnchanged(t, store, feed, before, audit)
				previewLedgerUnchanged(t, ledger, ops, events)
			})
		}
	}
}

func TestIdentityPreviewRegistryReobservationRejectsActorChanges(t *testing.T) {
	changes := []string{
		"running_sync", "completed_sync", "name", "enabled", "operation_type", "last_items_read",
		"last_attempt_added", "last_attempt_changed", "last_attempt_removed",
		"last_success_added", "last_success_changed", "last_success_removed",
	}
	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			reg, store, ledger, feed, items, _ := previewFixture(t, 2)
			previewSeedCanonicalItem(t, ledger, feed, items[0])
			at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			if strings.HasSuffix(change, "_changed") || strings.HasSuffix(change, "_removed") {
				store.mu.Lock()
				row := store.feeds[feed.ID]
				if strings.HasPrefix(change, "last_attempt") {
					row.LastAttemptAt = &at
				} else {
					row.LastSuccessAt = &at
				}
				store.feeds[feed.ID] = row
				store.mu.Unlock()
			}
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			ops, events := previewLedgerSnapshot(t, ledger)
			var actorRecord FeedRecord
			var actorAudit []AuditEvent
			lookup := &previewLookupHookRepository{MemoryRepository: ledger}
			lookup.hook = func(_ context.Context, _ string) error {
				if lookup.calls != 3 {
					return nil
				}
				// Interleave an independent actor after the first preview item is complete.
				scope, actorCtx := persistenceScope(feed), t.Context()
				switch change {
				case "running_sync", "completed_sync":
					token := uuid.New()
					if _, err := store.BeginSync(actorCtx, scope, feed.ID, token, at); err != nil {
						t.Fatal(err)
					}
					if change == "completed_sync" {
						outcome := SyncOutcome{ItemsRead: 2, Event: newFeedAudit(feed.ID, "synced", "explicit actor sync", at.Add(time.Second))}
						if err := store.FinishSync(actorCtx, scope, feed.ID, token, outcome); err != nil {
							t.Fatal(err)
						}
					}
				case "name", "enabled", "operation_type":
					name, enabled, operationType := "actor renamed feed", false, "actor_review"
					var patch FeedPatch
					switch change {
					case "name":
						patch.Name = &name
					case "enabled":
						patch.Enabled = &enabled
					case "operation_type":
						patch.OperationType = &operationType
					}
					if _, err := store.Patch(actorCtx, scope, feed.ID, patch, newFeedAudit(feed.ID, "updated", "explicit actor patch", at)); err != nil {
						t.Fatal(err)
					}
				default:
					// Actor-only fixture mutations isolate each observation comparison.
					store.mu.Lock()
					row := copyFeedRecord(store.feeds[feed.ID])
					changedAt := at.Add(time.Second)
					switch change {
					case "last_items_read":
						row.LastItemsRead++
					case "last_attempt_added", "last_attempt_changed":
						row.LastAttemptAt = &changedAt
					case "last_attempt_removed":
						row.LastAttemptAt = nil
					case "last_success_added", "last_success_changed":
						row.LastSuccessAt = &changedAt
					case "last_success_removed":
						row.LastSuccessAt = nil
					}
					store.feeds[feed.ID] = row
					store.mu.Unlock()
				}
				actorRecord, actorAudit = persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
				return nil
			}
			registry := &previewRegistryGetProbe{RegistryRepository: store}
			reg.repo, reg.ops = registry, operations.NewService(lookup)
			want := ErrIdentityPreviewChanged
			if change == "running_sync" {
				want = ErrFeedSyncBusy
			}
			got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
			if !errors.Is(err, want) || !reflect.DeepEqual(got, IdentityPreview{}) || lookup.calls != 4 {
				t.Fatalf("actor change returned wrong error or partial report: %+v / %v / %d", got, err, lookup.calls)
			}
			previewRegistryReads(t, registry, feed, 2)
			wantActorEvents := 0
			switch change {
			case "running_sync", "name", "enabled", "operation_type":
				wantActorEvents = 1
			case "completed_sync":
				wantActorEvents = 2
			}
			if reflect.DeepEqual(actorRecord, before) || len(actorAudit) != len(audit)+wantActorEvents {
				t.Fatalf("actor interleaving did not produce expected state/audit: before=%+v actor=%+v events=%d", before, actorRecord, len(actorAudit)-len(audit))
			}
			persistenceUnchanged(t, store, feed, actorRecord, actorAudit)
			previewLedgerUnchanged(t, ledger, ops, events)
		})
	}
}

func TestIdentityPreviewSecondGetFailureDiscardsReport(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reg, store, ledger, feed, items, _ := previewFixture(t, count)
			if count > 0 {
				previewSeedCanonicalItem(t, ledger, feed, items[0])
			}
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			ops, events := previewLedgerSnapshot(t, ledger)
			injected := fmt.Errorf("%w: second observation unavailable", ErrFeedStorageUnavailable)
			registry := &previewRegistryGetProbe{RegistryRepository: store}
			registry.afterGet = func(_ context.Context, row FeedRecord, call int) (FeedRecord, error) {
				if call == 2 {
					return FeedRecord{}, injected
				}
				return row, nil
			}
			lookup := &previewLookupHookRepository{MemoryRepository: ledger}
			reg.repo, reg.ops = registry, operations.NewService(lookup)
			got, err := reg.PreviewSourceIdentity(t.Context(), persistenceScope(feed), feed.ID)
			if !errors.Is(err, injected) || !reflect.DeepEqual(got, IdentityPreview{}) || lookup.calls != 2*count {
				t.Fatalf("second Get failure returned complete/empty success: %+v / %v / %d", got, err, lookup.calls)
			}
			previewRegistryReads(t, registry, feed, 2)
			persistenceUnchanged(t, store, feed, before, audit)
			previewLedgerUnchanged(t, ledger, ops, events)
		})
	}
}
