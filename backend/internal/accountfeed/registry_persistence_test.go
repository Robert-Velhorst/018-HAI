package accountfeed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/google/uuid"
)

const persistenceFeedJSON = `{"cursor":"next-42","items":[
  {"externalId":"m1","title":"Invoice","content":"Review the invoice","itemType":"email","provider":"gmail"},
  {"externalId":"i1","title":"Bug","content":"Fix the crash","itemType":"issue","provider":"github"}
]}`

func persistenceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(persistenceFeedJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func persistenceRegistry(t *testing.T, repo RegistryRepository, ledger operations.Repository, root string) *Registry {
	t.Helper()
	var service *operations.Service
	if ledger != nil {
		service = operations.NewService(ledger)
	}
	reg, err := NewRegistryWithRepository(repo, service, privacyfilter.NewService(), FetchOptions{FeedsRoot: root})
	if err != nil {
		t.Fatalf("construct registry: %v", err)
	}
	return reg
}

func persistenceSeed() Feed {
	return Feed{
		ID: uuid.New(), Name: "seed inbox", Provider: string(ProviderGenericJSONFeed),
		AccountLabel: "primary", SourceType: SourceLocalJSONFile, Path: "feed.json",
		OwnerUserID: "owner-a", WorkspaceID: "workspace-a", ProjectKey: "project-a",
		OperationType: "review_source_item", Enabled: true,
	}
}

func persistenceScope(feed Feed) FeedScope {
	return FeedScope{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}
}

func persistenceRecord(t *testing.T, repo RegistryRepository, feed Feed) FeedRecord {
	t.Helper()
	row, err := repo.Get(context.Background(), persistenceScope(feed), feed.ID)
	if err != nil {
		t.Fatalf("read stored feed: %v", err)
	}
	return row
}

func persistenceAudit(t *testing.T, repo RegistryRepository, feed Feed) []AuditEvent {
	t.Helper()
	events, err := repo.Audit(context.Background(), persistenceScope(feed), feed.ID)
	if err != nil {
		t.Fatalf("read stored audit: %v", err)
	}
	return events
}

func persistenceUnchanged(t *testing.T, repo RegistryRepository, feed Feed, before FeedRecord, audit []AuditEvent) {
	t.Helper()
	if after := persistenceRecord(t, repo, feed); !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected action changed stored state: before=%+v after=%+v", before, after)
	}
	if after := persistenceAudit(t, repo, feed); !reflect.DeepEqual(after, audit) {
		t.Fatalf("rejected action changed audit: before=%+v after=%+v", audit, after)
	}
}

func persistenceEvent(feed Feed, kind string, at time.Time) AuditEvent {
	return AuditEvent{ID: uuid.NewString(), FeedID: feed.ID.String(), EventType: kind, Message: "synthetic repository outcome", CreatedAt: at}
}

func TestRegistryConstructorRejectsNilRepository(t *testing.T) {
	reg, err := NewRegistryWithRepository(nil, nil, nil, FetchOptions{})
	if !errors.Is(err, ErrFeedStorageUnavailable) || reg != nil {
		t.Fatalf("nil repository must not fall back to memory: registry=%v err=%v", reg, err)
	}
}

func TestPostgresRegistryRepositoryRejectsUnreadyStore(t *testing.T) {
	// These exercise nil guards only. Do not call NewPostgresRegistry here:
	// its default database/schema probes require separate integration acceptance.
	for _, tc := range []struct {
		name string
		repo *GormRegistryRepository
	}{
		{"nil_receiver", nil},
		{"constructor_nil_database", NewGormRegistryRepository(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			feed := persistenceSeed()
			scope := persistenceScope(feed)
			at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			event := persistenceEvent(feed, "registered", at)
			token := uuid.New()
			if err := tc.repo.ready(); !errors.Is(err, ErrFeedStorageUnavailable) {
				t.Fatalf("unready PostgreSQL repository advertised readiness: %v", err)
			}
			if rows, err := tc.repo.List(ctx, scope); !errors.Is(err, ErrFeedStorageUnavailable) || len(rows) != 0 {
				t.Fatalf("unready list: %+v err=%v", rows, err)
			}
			if row, err := tc.repo.Get(ctx, scope, feed.ID); !errors.Is(err, ErrFeedStorageUnavailable) || !reflect.DeepEqual(row, FeedRecord{}) {
				t.Fatalf("unready get: %+v err=%v", row, err)
			}
			if got, err := tc.repo.Register(ctx, feed, event); !errors.Is(err, ErrFeedStorageUnavailable) || !reflect.DeepEqual(got, Feed{}) {
				t.Fatalf("unready register: %+v err=%v", got, err)
			}
			name := "uncommitted patch"
			if got, err := tc.repo.Patch(ctx, scope, feed.ID, FeedPatch{Name: &name}, event); !errors.Is(err, ErrFeedStorageUnavailable) || !reflect.DeepEqual(got, Feed{}) {
				t.Fatalf("unready patch: %+v err=%v", got, err)
			}
			if events, err := tc.repo.Audit(ctx, scope, feed.ID); !errors.Is(err, ErrFeedStorageUnavailable) || len(events) != 0 {
				t.Fatalf("unready audit: %+v err=%v", events, err)
			}
			if got, err := tc.repo.BeginSync(ctx, scope, feed.ID, token, at); !errors.Is(err, ErrFeedStorageUnavailable) || !reflect.DeepEqual(got, Feed{}) {
				t.Fatalf("unready begin sync: %+v err=%v", got, err)
			}
			if err := tc.repo.FinishSync(ctx, scope, feed.ID, token, SyncOutcome{Event: event}); !errors.Is(err, ErrFeedStorageUnavailable) {
				t.Fatalf("unready finish sync: %v", err)
			}
		})
	}
}

// Reusing a memory repository models registry reconstruction only, not a
// process restart, database durability, or production transaction acceptance.
func TestRegistrySharedMemoryReconstructionPreservesState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRegistryRepository()
	ledger := operations.NewMemoryRepository()
	root := persistenceFixture(t)
	first := persistenceRegistry(t, store, ledger, root)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	first.now = func() time.Time { return at }
	feed, err := first.RegisterContext(ctx, persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	scope := persistenceScope(feed)
	report, err := first.SyncContext(ctx, scope, feed.ID)
	if err != nil || !report.Recorded || report.Cursor != "next-42" || report.ItemsRead != 2 || report.OperationsCreated != 2 || len(report.Errors) != 0 {
		t.Fatalf("initial sync: report=%+v err=%v", report, err)
	}
	enabled, name, operationType := false, "operator inbox", "operator_review"
	patched, err := first.PatchContext(ctx, scope, feed.ID, FeedPatch{Enabled: &enabled, Name: &name, OperationType: &operationType})
	if err != nil {
		t.Fatal(err)
	}
	before := persistenceRecord(t, store, feed)
	audit := persistenceAudit(t, store, feed)
	if len(audit) != 4 || audit[0].EventType != "updated" || audit[1].EventType != "synced" || audit[2].EventType != "sync_started" || audit[3].EventType != "registered" {
		t.Fatalf("expected complete newest-first lifecycle audit: %+v", audit)
	}
	second := persistenceRegistry(t, store, ledger, root)
	got, err := second.GetContext(ctx, scope, feed.ID)
	if err != nil || !reflect.DeepEqual(got, patched) {
		t.Fatalf("reconstructed config: got=%+v want=%+v err=%v", got, patched, err)
	}
	health, err := second.ListContext(ctx, scope)
	if err != nil || len(health) != 1 {
		t.Fatalf("reconstructed health: %+v err=%v", health, err)
	}
	if !reflect.DeepEqual(health[0].Feed, patched) || health[0].LastItemsRead != 2 ||
		health[0].LastSyncedAt == nil || !health[0].LastSyncedAt.Equal(at) ||
		health[0].LastAttemptAt == nil || !health[0].LastAttemptAt.Equal(at) {
		t.Fatalf("reconstruction lost disabled config or freshness: %+v", health[0])
	}
	gotAudit, err := second.AuditContext(ctx, scope, feed.ID)
	if err != nil || !reflect.DeepEqual(gotAudit, audit) {
		t.Fatalf("reconstructed audit: %+v err=%v", gotAudit, err)
	}
	due, err := second.SyncDueContext(ctx, scope)
	if err != nil || len(due) != 0 {
		t.Fatalf("disabled feed was scheduled after reconstruction: %+v err=%v", due, err)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	enabled = true
	updated, err := second.PatchContext(ctx, scope, feed.ID, FeedPatch{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	got, err = first.GetContext(ctx, scope, feed.ID)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("first registry served stale process-local config: %+v err=%v", got, err)
	}
}

func TestRegistrySeedReregistrationPreservesOperatorPatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRegistryRepository()
	ledger := operations.NewMemoryRepository()
	root := persistenceFixture(t)
	first := persistenceRegistry(t, store, ledger, root)
	seed := persistenceSeed()
	feed, err := first.RegisterContext(ctx, seed)
	if err != nil {
		t.Fatal(err)
	}
	enabled, name, operationType := false, "operator-selected name", "operator_selected_type"
	patched, err := first.PatchContext(ctx, persistenceScope(feed), feed.ID, FeedPatch{Enabled: &enabled, Name: &name, OperationType: &operationType})
	if err != nil {
		t.Fatal(err)
	}
	before := persistenceRecord(t, store, feed)
	audit := persistenceAudit(t, store, feed)
	second := persistenceRegistry(t, store, ledger, root)
	// Also vary immutable seed config so idempotent registration cannot silently
	// restore stale defaults outside the mutable operator fields.
	seed.Path, seed.AccountLabel, seed.ProjectKey = "stale.json", "stale-account", "stale-project"
	for i := 0; i < 2; i++ {
		got, err := second.RegisterContext(ctx, seed)
		if err != nil || !reflect.DeepEqual(got, patched) {
			t.Fatalf("seed overwrote operator config: got=%+v want=%+v err=%v", got, patched, err)
		}
		persistenceUnchanged(t, store, feed, before, audit)
	}
}

func TestRegistryRepositoryScopePrivacy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRegistryRepository()
	ledger := operations.NewMemoryRepository()
	root := persistenceFixture(t)
	reg := persistenceRegistry(t, store, ledger, root)
	feed, err := reg.RegisterContext(ctx, persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []FeedScope{
		{OwnerUserID: "owner-b", WorkspaceID: feed.WorkspaceID},
		{OwnerUserID: feed.OwnerUserID, WorkspaceID: "workspace-b"},
		{OwnerUserID: "owner-b", WorkspaceID: "workspace-b"},
	} {
		t.Run(scope.OwnerUserID+"/"+scope.WorkspaceID, func(t *testing.T) {
			before := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			missing := uuid.New()
			for _, id := range []uuid.UUID{feed.ID, missing} {
				got, err := reg.GetContext(ctx, scope, id)
				if !errors.Is(err, ErrFeedNotFound) || !reflect.DeepEqual(got, Feed{}) {
					t.Fatalf("lookup exposed foreign/missing feed: %+v err=%v", got, err)
				}
				name := "unauthorized rename"
				got, err = reg.PatchContext(ctx, scope, id, FeedPatch{Name: &name})
				if !errors.Is(err, ErrFeedNotFound) || !reflect.DeepEqual(got, Feed{}) {
					t.Fatalf("patch exposed foreign/missing feed: %+v err=%v", got, err)
				}
				events, err := reg.AuditContext(ctx, scope, id)
				if !errors.Is(err, ErrFeedNotFound) || len(events) != 0 {
					t.Fatalf("audit exposed foreign/missing feed: %+v err=%v", events, err)
				}
				report, err := reg.SyncContext(ctx, scope, id)
				if !errors.Is(err, ErrFeedNotFound) || report.Recorded || report.Cursor != "" || report.ItemsRead != 0 {
					t.Fatalf("sync accepted foreign/missing feed: %+v err=%v", report, err)
				}
				row, err := store.Get(ctx, scope, id)
				if !errors.Is(err, ErrFeedNotFound) || !reflect.DeepEqual(row, FeedRecord{}) {
					t.Fatalf("repository lookup exposed foreign/missing feed: %+v err=%v", row, err)
				}
				if events, err := store.Audit(ctx, scope, id); !errors.Is(err, ErrFeedNotFound) || len(events) != 0 {
					t.Fatalf("repository audit exposed foreign/missing feed: %+v err=%v", events, err)
				}
				if _, err := store.BeginSync(ctx, scope, id, uuid.New(), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrFeedNotFound) {
					t.Fatalf("repository began foreign/missing sync: %v", err)
				}
			}
			foreign := feed
			foreign.OwnerUserID, foreign.WorkspaceID = scope.OwnerUserID, scope.WorkspaceID
			if got, err := reg.RegisterContext(ctx, foreign); !errors.Is(err, ErrFeedNotFound) || !reflect.DeepEqual(got, Feed{}) {
				t.Fatalf("same-id foreign registration must fail closed: %+v err=%v", got, err)
			}
			if rows, err := reg.ListContext(ctx, scope); err != nil || len(rows) != 0 {
				t.Fatalf("list exposed foreign feed: %+v err=%v", rows, err)
			}
			if rows, err := store.List(ctx, scope); err != nil || len(rows) != 0 {
				t.Fatalf("repository list exposed foreign feed: %+v err=%v", rows, err)
			}
			if reports, err := reg.SyncDueContext(ctx, scope); err != nil || len(reports) != 0 {
				t.Fatalf("scheduled foreign feed: %+v err=%v", reports, err)
			}
			persistenceUnchanged(t, store, feed, before, audit)
		})
	}
	// A scoped due run must select an owned feed even when other scopes contain
	// enabled feeds, not merely pass the empty-foreign-list case above.
	other := persistenceSeed()
	other.OwnerUserID = "owner-b"
	if _, err := reg.RegisterContext(ctx, other); err != nil {
		t.Fatal(err)
	}
	beforeOther := persistenceRecord(t, store, other)
	auditOther := persistenceAudit(t, store, other)
	reports, err := reg.SyncDueContext(ctx, persistenceScope(feed))
	if err != nil || len(reports) != 1 || reports[0].FeedID != feed.ID.String() || !reports[0].Recorded {
		t.Fatalf("due run was not scoped to the owned feed: %+v err=%v", reports, err)
	}
	persistenceUnchanged(t, store, other, beforeOther, auditOther)
	if rows, err := ledger.List(operations.Filter{OwnerUserID: other.OwnerUserID, WorkspaceID: other.WorkspaceID}); err != nil || len(rows) != 0 {
		t.Fatalf("scope rejection ingested foreign operations: %+v err=%v", rows, err)
	}
}

func TestRegistrySharedRepositoryBusyAndStaleTokenFence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRegistryRepository()
	ledger := operations.NewMemoryRepository()
	root := persistenceFixture(t)
	first := persistenceRegistry(t, store, ledger, root)
	feed, err := first.RegisterContext(ctx, persistenceSeed())
	if err != nil {
		t.Fatal(err)
	}
	scope := persistenceScope(feed)
	token := uuid.New()
	started := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if got, err := store.BeginSync(ctx, scope, feed.ID, token, started); err != nil || !reflect.DeepEqual(got, feed) {
		t.Fatalf("begin sync: %+v err=%v", got, err)
	}
	before := persistenceRecord(t, store, feed)
	audit := persistenceAudit(t, store, feed)
	if before.SyncToken == nil || *before.SyncToken != token || before.SyncStartedAt == nil || !before.SyncStartedAt.Equal(started) ||
		before.LastAttemptAt == nil || !before.LastAttemptAt.Equal(started) || before.LastSuccessAt != nil ||
		len(audit) != 2 || audit[0].EventType != "sync_started" || !audit[0].CreatedAt.Equal(started) {
		t.Fatalf("begin did not record token, attempt, and started audit together: row=%+v audit=%+v", before, audit)
	}
	second := persistenceRegistry(t, store, ledger, root)
	future := started.AddDate(100, 0, 0)
	for _, reg := range []*Registry{first, second} {
		reg.now = func() time.Time { return future }
		health, err := reg.ListContext(ctx, scope)
		if err != nil || len(health) != 1 || health[0].LastAttemptAt == nil || !health[0].LastAttemptAt.Equal(started) ||
			health[0].LastSyncedAt != nil || health[0].SyncStartedAt == nil || !health[0].SyncStartedAt.Equal(started) ||
			health[0].SyncState != "running_or_interrupted" {
			t.Fatalf("held claim was not observable before fetch: %+v err=%v", health, err)
		}
		report, err := reg.SyncContext(ctx, scope, feed.ID)
		if !errors.Is(err, ErrFeedSyncBusy) || report.Recorded || report.Cursor != "" || report.ItemsRead != 0 || report.OperationsCreated != 0 {
			t.Fatalf("registry stole an interrupted token: %+v err=%v", report, err)
		}
		name := "rename during sync"
		if _, err := reg.PatchContext(ctx, scope, feed.ID, FeedPatch{Name: &name}); !errors.Is(err, ErrFeedSyncBusy) {
			t.Fatalf("busy feed accepted patch: %v", err)
		}
		due, err := reg.SyncDueContext(ctx, scope)
		if err != nil || len(due) != 1 || due[0].Recorded || due[0].Cursor != "" || due[0].ItemsRead != 0 ||
			due[0].OperationsCreated != 0 || due[0].OperationsRefresh != 0 || len(due[0].Errors) == 0 {
			t.Fatalf("scheduler bypassed held claim: %+v err=%v", due, err)
		}
		persistenceUnchanged(t, store, feed, before, audit)
	}
	if _, err := store.BeginSync(ctx, scope, feed.ID, uuid.New(), future); !errors.Is(err, ErrFeedSyncBusy) {
		t.Fatalf("old token expired automatically: %v", err)
	}
	outcome := SyncOutcome{ItemsRead: 2, Event: persistenceEvent(feed, "synced", future)}
	if err := store.FinishSync(ctx, scope, feed.ID, uuid.New(), outcome); !errors.Is(err, ErrFeedSyncBusy) {
		t.Fatalf("wrong finish token was accepted: %v", err)
	}
	for _, foreign := range []FeedScope{
		{OwnerUserID: "owner-b", WorkspaceID: feed.WorkspaceID},
		{OwnerUserID: feed.OwnerUserID, WorkspaceID: "workspace-b"},
	} {
		if err := store.FinishSync(ctx, foreign, feed.ID, token, outcome); !errors.Is(err, ErrFeedNotFound) {
			t.Fatalf("matching token bypassed owner/workspace scope: %v", err)
		}
	}
	persistenceUnchanged(t, store, feed, before, audit)
	if rows, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}); err != nil || len(rows) != 0 {
		t.Fatalf("busy sync performed intake: %+v err=%v", rows, err)
	}
	if err := store.FinishSync(ctx, scope, feed.ID, token, outcome); err != nil {
		t.Fatal(err)
	}
	finished := persistenceRecord(t, store, feed)
	audit = persistenceAudit(t, store, feed)
	if finished.SyncToken != nil || finished.SyncStartedAt != nil || finished.LastItemsRead != 2 ||
		finished.LastSuccessAt == nil || !finished.LastSuccessAt.Equal(future) ||
		finished.LastAttemptAt == nil || !finished.LastAttemptAt.Equal(started) || len(audit) != 3 || audit[0] != outcome.Event {
		t.Fatalf("matching finish did not atomically publish outcome: row=%+v audit=%+v", finished, audit)
	}
	if err := store.FinishSync(ctx, scope, feed.ID, token, outcome); !errors.Is(err, ErrFeedSyncBusy) {
		t.Fatalf("completed token appended another outcome: %v", err)
	}
	persistenceUnchanged(t, store, feed, finished, audit)
	for _, kind := range []string{"sync_partial", "sync_failed"} {
		future = future.Add(time.Minute)
		active := uuid.New()
		if _, err := store.BeginSync(ctx, scope, feed.ID, active, future); err != nil {
			t.Fatal(err)
		}
		before = persistenceRecord(t, store, feed)
		audit = persistenceAudit(t, store, feed)
		if err := store.FinishSync(ctx, scope, feed.ID, token, outcome); !errors.Is(err, ErrFeedSyncBusy) {
			t.Fatalf("stale token cleared a newer attempt: %v", err)
		}
		persistenceUnchanged(t, store, feed, before, audit)
		failed := SyncOutcome{ItemsRead: 1, Event: persistenceEvent(feed, kind, future)}
		if err := store.FinishSync(ctx, scope, feed.ID, active, failed); err != nil {
			t.Fatal(err)
		}
		row := persistenceRecord(t, store, feed)
		events := persistenceAudit(t, store, feed)
		if row.SyncToken != nil || row.SyncStartedAt != nil || row.LastItemsRead != 1 ||
			row.LastSuccessAt == nil || !row.LastSuccessAt.Equal(*finished.LastSuccessAt) ||
			row.LastAttemptAt == nil || !row.LastAttemptAt.Equal(future) || len(events) != len(audit)+1 || events[0] != failed.Event {
			t.Fatalf("failed finish lost previous success or outcome: row=%+v audit=%+v", row, events)
		}
	}
}

type persistenceFailingRepository struct {
	RegistryRepository
	failMethod  string
	failure     error
	finishCalls int
	lastOutcome SyncOutcome
}

var _ RegistryRepository = (*persistenceFailingRepository)(nil)

func (r *persistenceFailingRepository) Register(ctx context.Context, feed Feed, event AuditEvent) (Feed, error) {
	if r.failMethod == "Register" {
		return Feed{}, r.failure
	}
	return r.RegistryRepository.Register(ctx, feed, event)
}

func (r *persistenceFailingRepository) Patch(ctx context.Context, scope FeedScope, id uuid.UUID, patch FeedPatch, event AuditEvent) (Feed, error) {
	if r.failMethod == "Patch" {
		return Feed{}, r.failure
	}
	return r.RegistryRepository.Patch(ctx, scope, id, patch, event)
}

func (r *persistenceFailingRepository) List(ctx context.Context, scope FeedScope) ([]FeedRecord, error) {
	if r.failMethod == "List" {
		return nil, r.failure
	}
	return r.RegistryRepository.List(ctx, scope)
}

func (r *persistenceFailingRepository) Get(ctx context.Context, scope FeedScope, id uuid.UUID) (FeedRecord, error) {
	if r.failMethod == "Get" {
		return FeedRecord{}, r.failure
	}
	return r.RegistryRepository.Get(ctx, scope, id)
}

func (r *persistenceFailingRepository) Audit(ctx context.Context, scope FeedScope, id uuid.UUID) ([]AuditEvent, error) {
	if r.failMethod == "Audit" {
		return nil, r.failure
	}
	return r.RegistryRepository.Audit(ctx, scope, id)
}

func (r *persistenceFailingRepository) BeginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	if r.failMethod == "BeginSync" {
		return Feed{}, r.failure
	}
	return r.RegistryRepository.BeginSync(ctx, scope, id, token, at)
}

func (r *persistenceFailingRepository) BeginEnabledSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	if r.failMethod == "BeginSync" {
		return Feed{}, r.failure
	}
	return r.RegistryRepository.BeginEnabledSync(ctx, scope, id, token, at)
}

func (r *persistenceFailingRepository) FinishSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, outcome SyncOutcome) error {
	r.finishCalls++
	r.lastOutcome = outcome
	if r.failMethod == "FinishSync" {
		return r.failure
	}
	return r.RegistryRepository.FinishSync(ctx, scope, id, token, outcome)
}

func TestRegistryStorageFailuresSurfaceWithoutFalseProgress(t *testing.T) {
	for _, tc := range []struct{ name, method string }{
		{"register", "Register"}, {"patch", "Patch"}, {"list", "List"},
		{"get", "Get"}, {"audit", "Audit"}, {"begin_sync", "BeginSync"},
		{"due_list", "List"}, {"due_begin", "BeginSync"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryRegistryRepository()
			failing := &persistenceFailingRepository{RegistryRepository: store, failure: errors.New("synthetic registry storage failure")}
			ledger := operations.NewMemoryRepository()
			reg := persistenceRegistry(t, failing, ledger, persistenceFixture(t))
			feed, err := reg.RegisterContext(ctx, persistenceSeed())
			if err != nil {
				t.Fatal(err)
			}
			scope := persistenceScope(feed)
			before := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			failing.failMethod = tc.method
			var callErr error
			switch tc.name {
			case "register":
				seed := persistenceSeed()
				var got Feed
				got, callErr = reg.RegisterContext(ctx, seed)
				if !reflect.DeepEqual(got, Feed{}) {
					t.Fatalf("failed register returned a feed: %+v", got)
				}
				if _, err := store.Get(ctx, persistenceScope(seed), seed.ID); !errors.Is(err, ErrFeedNotFound) {
					t.Fatalf("failed register stored config: %v", err)
				}
				if events, err := store.Audit(ctx, persistenceScope(seed), seed.ID); !errors.Is(err, ErrFeedNotFound) || len(events) != 0 {
					t.Fatalf("failed register stored audit: %+v err=%v", events, err)
				}
			case "patch":
				name := "uncommitted name"
				var got Feed
				got, callErr = reg.PatchContext(ctx, scope, feed.ID, FeedPatch{Name: &name})
				if !reflect.DeepEqual(got, Feed{}) {
					t.Fatalf("failed patch returned changed config: %+v", got)
				}
			case "list":
				var rows []FeedHealth
				rows, callErr = reg.ListContext(ctx, scope)
				if len(rows) != 0 {
					t.Fatalf("failed list used cached state: %+v", rows)
				}
			case "get":
				var got Feed
				got, callErr = reg.GetContext(ctx, scope, feed.ID)
				if !reflect.DeepEqual(got, Feed{}) {
					t.Fatalf("failed get used cached state: %+v", got)
				}
			case "audit":
				var events []AuditEvent
				events, callErr = reg.AuditContext(ctx, scope, feed.ID)
				if len(events) != 0 {
					t.Fatalf("failed audit used cached history: %+v", events)
				}
			case "begin_sync":
				var report SyncReport
				report, callErr = reg.SyncContext(ctx, scope, feed.ID)
				if report.Recorded || report.Cursor != "" || report.ItemsRead != 0 || report.OperationsCreated != 0 || report.OperationsRefresh != 0 {
					t.Fatalf("unstarted sync advertised progress: %+v", report)
				}
			case "due_list":
				var reports []SyncReport
				reports, callErr = reg.SyncDueContext(ctx, scope)
				if len(reports) != 0 {
					t.Fatalf("failed scheduler used cached feeds: %+v", reports)
				}
			case "due_begin":
				var reports []SyncReport
				reports, callErr = reg.SyncDueContext(ctx, scope)
				if len(reports) != 1 || reports[0].Recorded || reports[0].Cursor != "" || reports[0].ItemsRead != 0 ||
					reports[0].OperationsCreated != 0 || reports[0].OperationsRefresh != 0 {
					t.Fatalf("scheduler advertised an unstarted sync: %+v", reports)
				}
			}
			if callErr == nil || callErr.Error() == "" {
				t.Fatalf("%s failure was swallowed: %v", tc.method, callErr)
			}
			persistenceUnchanged(t, store, feed, before, audit)
			if failing.finishCalls != 0 {
				t.Fatalf("failed pre-intake action attempted finish: %d", failing.finishCalls)
			}
			if rows, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}); err != nil || len(rows) != 0 {
				t.Fatalf("failed pre-intake action created operations: %+v err=%v", rows, err)
			}
		})
	}
}

type persistenceIntakeRepository struct {
	*operations.MemoryRepository
	rejectTitle string
}

func (r *persistenceIntakeRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if r.rejectTitle != "" && op.Title == r.rejectTitle {
		return nil, errors.New("synthetic intake failure")
	}
	return r.MemoryRepository.CreateWithEvent(op, event)
}

func (r *persistenceIntakeRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.rejectTitle != "" && op.Title == r.rejectTitle {
		return nil, errors.New("synthetic intake failure")
	}
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func TestRegistryRecordedOutcomeDoesNotImplySuccessfulSync(t *testing.T) {
	for _, tc := range []struct {
		name, payload, rejectTitle, eventType string
		itemsRead, created                    int
	}{
		{"invalid_envelope", `{"cursor":"must-not-advance"}`, "", "sync_failed", 0, 0},
		{"partial_intake", persistenceFeedJSON, "Bug", "sync_partial", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryRegistryRepository()
			ledger := &persistenceIntakeRepository{MemoryRepository: operations.NewMemoryRepository(), rejectTitle: tc.rejectTitle}
			root := persistenceFixture(t)
			if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(tc.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			reg := persistenceRegistry(t, store, ledger, root)
			feed, err := reg.RegisterContext(ctx, persistenceSeed())
			if err != nil {
				t.Fatal(err)
			}
			report, err := reg.SyncContext(ctx, persistenceScope(feed), feed.ID)
			if err != nil || !report.Recorded || report.Cursor != "" || len(report.Errors) == 0 ||
				report.ItemsRead != tc.itemsRead || report.OperationsCreated != tc.created {
				t.Fatalf("recorded unsuccessful outcome was misreported: %+v err=%v", report, err)
			}
			row := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			if row.SyncToken != nil || row.SyncStartedAt != nil || row.LastAttemptAt == nil || row.LastSuccessAt != nil ||
				row.LastItemsRead != tc.itemsRead || len(audit) != 3 || audit[0].EventType != tc.eventType || audit[1].EventType != "sync_started" {
				t.Fatalf("recorded unsuccessful outcome advertised success or lost audit: row=%+v audit=%+v", row, audit)
			}
		})
	}
}

func TestRegistryFinishFailureRetainsIntakeAndBlocksRetry(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "all_items_ingested"
		if partial {
			name = "partial_intake"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryRegistryRepository()
			failing := &persistenceFailingRepository{RegistryRepository: store, failure: errors.New("synthetic finish storage failure")}
			ledger := &persistenceIntakeRepository{MemoryRepository: operations.NewMemoryRepository()}
			wantCreated, wantEvent := 2, "synced"
			if partial {
				ledger.rejectTitle = "Bug"
				wantCreated, wantEvent = 1, "sync_partial"
			}
			root := persistenceFixture(t)
			reg := persistenceRegistry(t, failing, ledger, root)
			at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			reg.now = func() time.Time { return at }
			feed, err := reg.RegisterContext(ctx, persistenceSeed())
			if err != nil {
				t.Fatal(err)
			}
			scope := persistenceScope(feed)
			failing.failMethod = "FinishSync"
			report, err := reg.SyncContext(ctx, scope, feed.ID)
			if !errors.Is(err, ErrFeedStorageUnavailable) || report.Recorded || report.Cursor != "" ||
				report.ItemsRead != 2 || report.OperationsCreated != wantCreated || report.OperationsRefresh != 0 || len(report.Errors) == 0 {
				t.Fatalf("uncommitted finish advertised success or hid intake: %+v err=%v", report, err)
			}
			if failing.finishCalls != 1 || failing.lastOutcome.ItemsRead != 2 || failing.lastOutcome.Event.EventType != wantEvent {
				t.Fatalf("finish did not receive truthful attempted outcome: calls=%d outcome=%+v", failing.finishCalls, failing.lastOutcome)
			}
			before := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			if before.SyncToken == nil || before.SyncStartedAt == nil || !before.SyncStartedAt.Equal(at) ||
				before.LastAttemptAt == nil || !before.LastAttemptAt.Equal(at) || before.LastSuccessAt != nil || before.LastItemsRead != 0 ||
				len(audit) != 2 || audit[0].EventType != "sync_started" || audit[1].EventType != "registered" {
				t.Fatalf("failed finish committed health/audit or released token: row=%+v audit=%+v", before, audit)
			}
			stored, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
			if err != nil || len(stored) != wantCreated {
				t.Fatalf("finish failure rolled back committed intake: %+v err=%v", stored, err)
			}
			for _, op := range stored {
				events, err := ledger.ListEvents(op.ID, 0)
				if err != nil || len(events) != 2 || !observationWiringCreationAudit(events, 1) {
					t.Fatalf("committed operation lost its creation audit: %+v err=%v", events, err)
				}
			}
			// Restoring storage and reconstructing a registry cannot authorize a
			// second observation while the original finish remains uncommitted.
			failing.failMethod, ledger.rejectTitle = "", ""
			second := persistenceRegistry(t, failing, ledger, root)
			second.now = func() time.Time { return at.AddDate(100, 0, 0) }
			for _, registry := range []*Registry{reg, second} {
				retry, err := registry.SyncContext(ctx, scope, feed.ID)
				if !errors.Is(err, ErrFeedSyncBusy) || retry.Recorded || retry.Cursor != "" || retry.ItemsRead != 0 || retry.OperationsCreated != 0 || retry.OperationsRefresh != 0 {
					t.Fatalf("finish failure permitted retry intake: %+v err=%v", retry, err)
				}
				persistenceUnchanged(t, store, feed, before, audit)
			}
			if failing.finishCalls != 1 {
				t.Fatalf("blocked retry wrote a replacement finish: %d", failing.finishCalls)
			}
			after, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
			if err != nil || len(after) != len(stored) {
				t.Fatalf("blocked retry changed committed intake count: %+v err=%v", after, err)
			}
			byID := make(map[uuid.UUID]models.Operation, len(stored))
			for _, op := range stored {
				byID[op.ID] = op
			}
			for _, op := range after {
				if !reflect.DeepEqual(op, byID[op.ID]) {
					t.Fatalf("blocked retry changed committed operation: before=%+v after=%+v", byID[op.ID], op)
				}
			}
		})
	}
}
