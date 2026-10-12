package accountfeed

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func sourceConfigurationVersionFixture(t *testing.T) (*Registry, *operations.MemoryRepository, Feed) {
	t.Helper()
	ledger := operations.NewMemoryRepository()
	registry, err := NewRegistryWithRepository(NewMemoryRegistryRepository(), operations.NewService(ledger), nil, FetchOptions{})
	if err != nil || registry == nil {
		t.Fatalf("construct canonical memory registry: %v", err)
	}
	return registry, ledger, Feed{
		ID:   uuid.MustParse("00000000-0000-4000-8000-000000000814"),
		Name: "canonical A", Provider: string(ProviderGenericJSONFeed), AccountLabel: "primary",
		SourceType: SourceLocalJSONFile, Path: "version-fixture.json",
		OwnerUserID: "version-owner", WorkspaceID: "version-workspace",
		ProjectKey: "version-project", OperationType: "review_source_item", Enabled: true,
	}
}

func sourceConfigurationVersionStored(t *testing.T, registry *Registry, want Feed) {
	t.Helper()
	got, err := registry.GetContext(context.Background(), feedScope(want), want.ID)
	if err != nil || got != want {
		t.Fatalf("canonical feed not persisted exactly: got=%+v want=%+v err=%v", got, want, err)
	}
}

func sourceConfigurationVersionObserve(t *testing.T, ledger *operations.MemoryRepository, feed Feed) operations.SourceObservation {
	t.Helper()
	start := feed.SourceObservationStart()
	got, err := ledger.BeginSourceObservation(context.Background(), start)
	if err != nil || got.ID == uuid.Nil || got.OwnerUserID != feed.OwnerUserID || got.WorkspaceID != feed.WorkspaceID ||
		got.OriginID != feed.ID || got.ConfigDigest != start.ConfigDigest || got.ConfigEpoch <= 0 || got.Generation <= 0 {
		t.Fatalf("canonical feed cannot mint matching observation: %+v / %v", got, err)
	}
	return got
}

func TestRegistryConfigVersionRegistrationIgnoresCallerVersion(t *testing.T) {
	for _, supplied := range []int64{-1, 0, 1, 73, math.MaxInt64} {
		t.Run(strconv.FormatInt(supplied, 10), func(t *testing.T) {
			registry, ledger, input := sourceConfigurationVersionFixture(t)
			input.ConfigVersion = supplied
			registered, err := registry.RegisterContext(context.Background(), input)
			want := input
			want.ConfigVersion = 1
			if err != nil || registered != want {
				t.Fatalf("caller version became canonical: got=%+v want=%+v err=%v", registered, want, err)
			}
			sourceConfigurationVersionStored(t, registry, registered)
			observed := sourceConfigurationVersionObserve(t, ledger, registered)
			if observed.ConfigEpoch != 1 || observed.Generation != 1 {
				t.Fatalf("fresh registered origin has wrong epoch/generation: %+v", observed)
			}
			if supplied != 1 {
				got, err := ledger.BeginSourceObservation(context.Background(), input.SourceObservationStart())
				if !errors.Is(err, operations.ErrSourceHeadSuperseded) || got != (operations.SourceObservation{}) {
					t.Fatalf("caller-supplied version reminted canonical authority: %+v / %v", got, err)
				}
			}
		})
	}
}

func TestFeedConfigVersionUsesPublicJSONName(t *testing.T) {
	registry, _, input := sourceConfigurationVersionFixture(t)
	feed, err := registry.RegisterContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	versionJSON, exists := fields["configVersion"]
	if !exists || string(versionJSON) != "1" {
		t.Fatalf("missing canonical public configVersion: %s", encoded)
	}
	if _, exists := fields["ConfigVersion"]; exists {
		t.Fatal("config version leaked a Go field name instead of the public JSON name")
	}
	var decoded Feed
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != feed {
		t.Fatalf("feed configVersion did not round-trip: %+v / %v", decoded, err)
	}
}

func TestApplyFeedPatchConfigVersionChangesExactlyOnce(t *testing.T) {
	_, _, feed := sourceConfigurationVersionFixture(t)
	feed.ConfigVersion = 11
	name, normalizedName, operationType, enabled := "canonical B", "  canonical A  ", "review_other_item", false
	for _, tc := range []struct {
		name    string
		patch   FeedPatch
		changed bool
	}{
		{"empty", FeedPatch{}, false},
		{"same_values", FeedPatch{Name: &feed.Name, OperationType: &feed.OperationType, Enabled: &feed.Enabled}, false},
		{"normalized_name_noop", FeedPatch{Name: &normalizedName}, false},
		{"name", FeedPatch{Name: &name}, true},
		{"operation_type", FeedPatch{OperationType: &operationType}, true},
		{"enabled", FeedPatch{Enabled: &enabled}, true},
		{"three_fields_one_increment", FeedPatch{Name: &name, OperationType: &operationType, Enabled: &enabled}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyFeedPatch(feed, tc.patch)
			want := feed
			if tc.patch.Name != nil {
				want.Name = strings.TrimSpace(*tc.patch.Name)
			}
			if tc.patch.OperationType != nil {
				want.OperationType = *tc.patch.OperationType
			}
			if tc.patch.Enabled != nil {
				want.Enabled = *tc.patch.Enabled
			}
			if tc.changed {
				want.ConfigVersion++
			}
			if err != nil || got != want {
				t.Fatalf("patch changed feed/version incorrectly: got=%+v want=%+v err=%v", got, want, err)
			}
			withoutVersion := got
			withoutVersion.ConfigVersion = feed.ConfigVersion
			if (withoutVersion != feed) != tc.changed || (got.SourceObservationStart() != feed.SourceObservationStart()) != tc.changed {
				t.Fatalf("semantic patch/digest disagrees with version change: before=%+v after=%+v", feed, got)
			}
		})
	}
}

func TestRegistryConfigVersionPatchPersistsChangeAndNoop(t *testing.T) {
	registry, ledger, input := sourceConfigurationVersionFixture(t)
	feed, err := registry.RegisterContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	name, operationType := "canonical B", "review_other_item"
	changed, err := registry.PatchContext(context.Background(), feedScope(feed), feed.ID, FeedPatch{Name: &name, OperationType: &operationType})
	if err != nil || changed.ConfigVersion != 2 || changed.Name != name || changed.OperationType != operationType {
		t.Fatalf("multi-field patch did not persist one increment: %+v / %v", changed, err)
	}
	sourceConfigurationVersionStored(t, registry, changed)
	first := sourceConfigurationVersionObserve(t, ledger, changed)
	for _, patch := range []FeedPatch{
		{},
		{Name: &changed.Name, OperationType: &changed.OperationType, Enabled: &changed.Enabled},
	} {
		unchanged, err := registry.PatchContext(context.Background(), feedScope(changed), changed.ID, patch)
		if err != nil || unchanged != changed {
			t.Fatalf("no-op changed canonical feed/version: %+v / %v", unchanged, err)
		}
		sourceConfigurationVersionStored(t, registry, unchanged)
		next := sourceConfigurationVersionObserve(t, ledger, unchanged)
		if next.ConfigEpoch != first.ConfigEpoch || next.Generation != first.Generation+1 {
			t.Fatalf("no-op advanced authority epoch or lost tenant clock: before=%+v after=%+v", first, next)
		}
		first = next
	}
}

func TestRegistryConfigVersionABACannotMintFromOriginallyCachedFeed(t *testing.T) {
	registry, ledger, input := sourceConfigurationVersionFixture(t)
	original, err := registry.RegisterContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	first := sourceConfigurationVersionObserve(t, ledger, original)
	nameB := "canonical B"
	feedB, err := registry.PatchContext(context.Background(), feedScope(original), original.ID, FeedPatch{Name: &nameB})
	if err != nil || feedB.ConfigVersion != 2 {
		t.Fatalf("patch A to B: %+v / %v", feedB, err)
	}
	middle := sourceConfigurationVersionObserve(t, ledger, feedB)
	restored, err := registry.PatchContext(context.Background(), feedScope(original), original.ID, FeedPatch{Name: &original.Name})
	if err != nil || restored.ConfigVersion != 3 {
		t.Fatalf("patch B to A did not retain version history: %+v / %v", restored, err)
	}
	withoutVersion := restored
	withoutVersion.ConfigVersion = original.ConfigVersion
	if withoutVersion != original || withoutVersion.SourceObservationStart() != original.SourceObservationStart() ||
		restored.SourceObservationStart() == original.SourceObservationStart() {
		t.Fatal("A-B-A fixture did not isolate configVersion as the stale-snapshot fence")
	}
	sourceConfigurationVersionStored(t, registry, restored)
	staleUnmanaged := original.SourceObservationStart()
	staleUnmanaged.RegistryManaged = false
	for _, start := range []operations.SourceObservationStart{
		original.SourceObservationStart(), feedB.SourceObservationStart(), staleUnmanaged,
	} {
		got, err := ledger.BeginSourceObservation(context.Background(), start)
		if !errors.Is(err, operations.ErrSourceHeadSuperseded) || got != (operations.SourceObservation{}) {
			t.Fatalf("cached or downgraded A-B-A snapshot minted authority: %+v / %v", got, err)
		}
		sourceConfigurationVersionStored(t, registry, restored)
	}
	fresh := sourceConfigurationVersionObserve(t, ledger, restored)
	if first.ConfigEpoch != 1 || middle.ConfigEpoch != 2 || fresh.ConfigEpoch != 3 ||
		middle.Generation != first.Generation+1 || fresh.Generation != middle.Generation+1 {
		t.Fatalf("refused cached starts changed epoch/clock, or current A is not usable: first=%+v B=%+v current=%+v", first, middle, fresh)
	}
}

func TestRegistryConfigVersionDuplicateRegisterPreservesCanonicalVersion(t *testing.T) {
	registry, ledger, input := sourceConfigurationVersionFixture(t)
	registered, err := registry.RegisterContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	name := "canonical B"
	current, err := registry.PatchContext(context.Background(), feedScope(registered), registered.ID, FeedPatch{Name: &name})
	if err != nil || current.ConfigVersion != 2 {
		t.Fatalf("prepare versioned duplicate registration: %+v / %v", current, err)
	}
	before := sourceConfigurationVersionObserve(t, ledger, current)
	audit, err := registry.AuditContext(context.Background(), feedScope(current), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int64{0, 1, 99, math.MaxInt64} {
		cached := registered
		cached.ConfigVersion = version
		got, err := registry.RegisterContext(context.Background(), cached)
		if err != nil || got != current {
			t.Fatalf("duplicate registration reset or overwrote canonical version: %+v / %v want=%+v", got, err, current)
		}
		sourceConfigurationVersionStored(t, registry, current)
		afterAudit, err := registry.AuditContext(context.Background(), feedScope(current), current.ID)
		if err != nil || !reflect.DeepEqual(afterAudit, audit) {
			t.Fatalf("duplicate registration rewrote audit history: %+v / %v", afterAudit, err)
		}
		next := sourceConfigurationVersionObserve(t, ledger, current)
		if next.ConfigEpoch != before.ConfigEpoch || next.Generation != before.Generation+1 {
			t.Fatalf("duplicate registration reminted canonical configuration: before=%+v after=%+v", before, next)
		}
		before = next
	}
}

func TestMemoryRegistryConfigVersionRejectsLateAuthorityBinding(t *testing.T) {
	_, ledger, input := sourceConfigurationVersionFixture(t)
	store := NewMemoryRegistryRepository()
	unbound, err := NewRegistryWithRepository(store, nil, nil, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	feed, err := unbound.RegisterContext(context.Background(), input)
	if err != nil || feed.ConfigVersion != 1 {
		t.Fatalf("prepare populated unbound registry: %+v / %v", feed, err)
	}
	before := persistenceRecord(t, store, feed)
	audit := persistenceAudit(t, store, feed)
	service := operations.NewService(ledger)
	if err := store.BindSourceAuthority(service); !errors.Is(err, ErrFeedStorageUnavailable) {
		t.Fatalf("late binding did not refuse unenrolled rows: %v", err)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	if got, err := NewRegistryWithRepository(store, service, nil, FetchOptions{}); !errors.Is(err, ErrFeedStorageUnavailable) || got != nil {
		t.Fatalf("constructor exposed a populated unenrolled registry: %+v / %v", got, err)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	if err := store.BindSourceAuthority(nil); err != nil {
		t.Fatalf("nil read-only binding refused: %v", err)
	}
	readOnly, err := NewRegistryWithRepository(store, nil, nil, FetchOptions{})
	if err != nil || readOnly == nil {
		t.Fatalf("read-only reconstruction refused: %v", err)
	}
	sourceConfigurationVersionStored(t, readOnly, feed)
	persistenceUnchanged(t, store, feed, before, audit)
	store.mu.Lock()
	authority := store.authority
	store.mu.Unlock()
	if authority != nil {
		t.Fatal("refused binding or nil facade attached authority to unenrolled rows")
	}
	if got, err := ledger.BeginSourceObservation(context.Background(), feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) || got != (operations.SourceObservation{}) {
		t.Fatalf("refused binding minted canonical source authority: %+v / %v", got, err)
	}
}

func TestMemoryRegistryConfigVersionAuthorityReconstructionKeepsBoundLedger(t *testing.T) {
	registry, ledger, input := sourceConfigurationVersionFixture(t)
	feed, err := registry.RegisterContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	store := registry.repo.(*MemoryRegistryRepository)
	before := persistenceRecord(t, store, feed)
	audit := persistenceAudit(t, store, feed)
	first := sourceConfigurationVersionObserve(t, ledger, feed)
	readOnly, err := NewRegistryWithRepository(store, nil, nil, FetchOptions{})
	if err != nil || readOnly == nil {
		t.Fatalf("nil read-only reconstruction refused: %v", err)
	}
	sourceConfigurationVersionStored(t, readOnly, feed)
	reconstructed, err := NewRegistryWithRepository(store, operations.NewService(ledger), nil, FetchOptions{})
	if err != nil || reconstructed == nil {
		t.Fatalf("same-ledger reconstruction refused: %v", err)
	}
	sourceConfigurationVersionStored(t, reconstructed, feed)
	other := operations.NewMemoryRepository()
	if err := store.BindSourceAuthority(operations.NewService(other)); !errors.Is(err, ErrFeedStorageUnavailable) {
		t.Fatalf("different ledger rebound canonical authority: %v", err)
	}
	if got, err := NewRegistryWithRepository(store, operations.NewService(other), nil, FetchOptions{}); !errors.Is(err, ErrFeedStorageUnavailable) || got != nil {
		t.Fatalf("different-ledger constructor exposed a facade: %+v / %v", got, err)
	}
	persistenceUnchanged(t, store, feed, before, audit)
	next := sourceConfigurationVersionObserve(t, ledger, feed)
	if next.ConfigEpoch != first.ConfigEpoch || next.Generation != first.Generation+1 {
		t.Fatalf("reconstruction reminted or detached original authority: before=%+v after=%+v", first, next)
	}
	if got, err := other.BeginSourceObservation(context.Background(), feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) || got != (operations.SourceObservation{}) {
		t.Fatalf("rejected ledger gained canonical authority: %+v / %v", got, err)
	}
	name := "canonical after reconstruction"
	changed, err := reconstructed.PatchContext(context.Background(), feedScope(feed), feed.ID, FeedPatch{Name: &name})
	if err != nil || changed.ConfigVersion != 2 || changed.Name != name {
		t.Fatalf("healthy reconstructed mutation failed: %+v / %v", changed, err)
	}
	if got, err := ledger.BeginSourceObservation(context.Background(), feed.SourceObservationStart()); !errors.Is(err, operations.ErrSourceHeadSuperseded) || got != (operations.SourceObservation{}) {
		t.Fatalf("nil or rejected rebind detached mutation revocation: %+v / %v", got, err)
	}
	current := sourceConfigurationVersionObserve(t, ledger, changed)
	if current.ConfigEpoch != next.ConfigEpoch+1 || current.Generation != next.Generation+1 {
		t.Fatalf("mutation no longer uses original ledger: %+v", current)
	}
}

type sourceConfigurationVersionObservationCounter struct {
	*operations.MemoryRepository
	beginCalls int
}

func (r *sourceConfigurationVersionObservationCounter) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	r.beginCalls++
	return r.MemoryRepository.BeginSourceObservation(ctx, start)
}

type sourceConfigurationVersionCallerKey struct{}

type sourceConfigurationVersionCanceledBeginRepository struct {
	*MemoryRegistryRepository
	cancel             context.CancelFunc
	mode               string
	beginToken         uuid.UUID
	committedToken     uuid.UUID
	finishToken        uuid.UUID
	finishFeedID       uuid.UUID
	finishScope        FeedScope
	finishCalls        int
	finishContextValid bool
	finishOutcome      SyncOutcome
	beforeFinish       FeedRecord
	beforeFinishAudit  []AuditEvent
}

func (r *sourceConfigurationVersionCanceledBeginRepository) BeginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	r.beginToken, r.committedToken = token, token
	if r.mode == "other_token" {
		r.committedToken = uuid.MustParse("00000000-0000-4000-8000-000000000815")
		if r.committedToken == token {
			return Feed{}, errors.New("fixture generated token unexpectedly equals the other attempt")
		}
	}
	feed, err := r.MemoryRegistryRepository.BeginSync(context.Background(), scope, id, r.committedToken, at)
	if err != nil {
		return Feed{}, err
	}
	r.beforeFinish, err = r.MemoryRegistryRepository.Get(context.Background(), scope, id)
	if err != nil {
		return Feed{}, err
	}
	r.beforeFinishAudit, err = r.MemoryRegistryRepository.Audit(context.Background(), scope, id)
	if err != nil {
		return Feed{}, err
	}
	r.cancel()
	return feed, ctx.Err()
}

func (r *sourceConfigurationVersionCanceledBeginRepository) FinishSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, outcome SyncOutcome) error {
	r.finishCalls++
	r.finishToken, r.finishOutcome = token, outcome
	r.finishFeedID, r.finishScope = id, scope
	if ctx != nil {
		deadline, bounded := ctx.Deadline()
		remaining := time.Until(deadline)
		r.finishContextValid = ctx.Err() == nil && bounded && remaining > 0 && remaining <= 10*time.Second && ctx.Value(sourceConfigurationVersionCallerKey{}) == nil
	}
	if r.mode == "cleanup_failure" {
		return ErrFeedStorageUnavailable
	}
	return r.MemoryRegistryRepository.FinishSync(ctx, scope, id, token, outcome)
}

func TestRegistryConfigVersionCanceledBeginSettlesOnlyOwnedToken(t *testing.T) {
	for _, mode := range []string{"owned_token", "other_token", "cleanup_failure"} {
		t.Run(mode, func(t *testing.T) {
			_, ledger, input := sourceConfigurationVersionFixture(t)
			counter := &sourceConfigurationVersionObservationCounter{MemoryRepository: ledger}
			store := &sourceConfigurationVersionCanceledBeginRepository{MemoryRegistryRepository: NewMemoryRegistryRepository(), mode: mode}
			registry, err := NewRegistryWithRepository(store, operations.NewService(counter), nil, FetchOptions{})
			if err != nil {
				t.Fatal(err)
			}
			feed, err := registry.RegisterContext(context.Background(), input)
			if err != nil || feed.ConfigVersion != 1 {
				t.Fatalf("canonical bound cancellation fixture: %+v / %v", feed, err)
			}
			caller := context.WithValue(context.Background(), sourceConfigurationVersionCallerKey{}, "caller value must not enter cleanup")
			ctx, cancel := context.WithCancel(caller)
			defer cancel()
			store.cancel = cancel
			report, err := registry.SyncContext(ctx, feedScope(feed), feed.ID)
			if !errors.Is(err, context.Canceled) || report.FeedID != feed.ID.String() {
				t.Fatalf("original begin cancellation lost: %+v / %v", report, err)
			}
			if store.beginToken == uuid.Nil || store.finishCalls != 1 || store.finishToken != store.beginToken || store.finishFeedID != feed.ID || store.finishScope != feedScope(feed) || !store.finishContextValid {
				t.Fatalf("cleanup did not use exact token and fresh bounded authority: begin=%s finish=%s calls=%d valid=%t", store.beginToken, store.finishToken, store.finishCalls, store.finishContextValid)
			}
			if store.beforeFinish.SyncToken == nil || *store.beforeFinish.SyncToken != store.committedToken || store.beforeFinish.SyncStartedAt == nil {
				t.Fatalf("fixture never committed a real active attempt: %+v", store.beforeFinish)
			}
			if store.finishOutcome.ItemsRead != 0 || store.finishOutcome.Event.FeedID != feed.ID.String() || store.finishOutcome.Event.EventType != "sync_failed" || store.finishOutcome.Event.ID == "" || store.finishOutcome.Event.CreatedAt.IsZero() {
				t.Fatalf("cleanup recorded a read or the wrong attempt: %+v", store.finishOutcome)
			}
			if counter.beginCalls != 0 || report.ReadCompleted || report.ItemsRead != 0 || report.OperationsCreated != 0 || report.OperationsRefresh != 0 || report.Cursor != "" || report.PrivacyFlagged != 0 {
				t.Fatalf("cancelled begin reached observation/read/intake: calls=%d report=%+v", counter.beginCalls, report)
			}
			ops, err := ledger.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
			if err != nil || len(ops) != 0 {
				t.Fatalf("canceled begin wrote operations: %+v / %v", ops, err)
			}
			row := persistenceRecord(t, store, feed)
			audit := persistenceAudit(t, store, feed)
			if mode == "owned_token" {
				if !report.Recorded || row.SyncToken != nil || row.SyncStartedAt != nil || row.Feed != feed || row.LastSuccessAt != nil || row.LastItemsRead != 0 ||
					!reflect.DeepEqual(row.LastAttemptAt, store.beforeFinish.LastAttemptAt) {
					t.Fatalf("owned cancellation was not settled without changing config: %+v / %+v", row, report)
				}
				if len(audit) != len(store.beforeFinishAudit)+1 || !reflect.DeepEqual(audit[1:], store.beforeFinishAudit) || audit[0] != store.finishOutcome.Event {
					t.Fatalf("cleanup did not append exactly its failed-attempt audit: %+v", audit)
				}
			} else {
				if report.Recorded || row.SyncToken == nil || *row.SyncToken != store.committedToken || !reflect.DeepEqual(row, store.beforeFinish) || !reflect.DeepEqual(audit, store.beforeFinishAudit) {
					t.Fatalf("failed/unowned cleanup changed token, row or history: %+v / %+v", row, report)
				}
				if mode == "cleanup_failure" && !strings.Contains(strings.Join(report.Errors, " "), "operator review") {
					t.Fatalf("cleanup failure hid operator recovery requirement: %+v", report)
				}
			}
			fresh := sourceConfigurationVersionObserve(t, ledger, feed)
			if fresh.Generation != 1 || fresh.ConfigEpoch != 1 {
				t.Fatalf("canceled begin minted observation or altered canonical origin: %+v", fresh)
			}
		})
	}
}
