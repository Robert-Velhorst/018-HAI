package accountfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

type observationWiringRepository struct {
	*operations.MemoryRepository
	starts                    []operations.SourceObservationStart
	observations              []operations.SourceObservation
	beginError                error
	afterBegin                func(operations.SourceObservation)
	lookups, creates, updates int
}

var _ operations.SourceObservationRepository = (*observationWiringRepository)(nil)
var _ operations.ContextIntakeRepository = (*observationWiringRepository)(nil)

func (r *observationWiringRepository) BeginSourceObservation(ctx context.Context, start operations.SourceObservationStart) (operations.SourceObservation, error) {
	r.starts = append(r.starts, start)
	if r.beginError != nil {
		return operations.SourceObservation{}, r.beginError
	}
	observation, err := r.MemoryRepository.BeginSourceObservation(ctx, start)
	if err != nil {
		return operations.SourceObservation{}, err
	}
	r.observations = append(r.observations, observation)
	if r.afterBegin != nil {
		r.afterBegin(observation)
	}
	return observation, nil
}

func (r *observationWiringRepository) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	r.lookups++
	return r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
}

func (r *observationWiringRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.creates++
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func (r *observationWiringRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.updates++
	return r.MemoryRepository.UpdateWithEventContext(ctx, op, event)
}

func newObservationWiringRegistry(t *testing.T) (*Registry, *operations.Service, *observationWiringRepository, Feed, string) {
	t.Helper()
	root := t.TempDir()
	repo := &observationWiringRepository{MemoryRepository: operations.NewMemoryRepository()}
	svc := operations.NewService(repo)
	registry := NewRegistry(svc, nil, FetchOptions{FeedsRoot: root})
	feed, err := registry.RegisterContext(t.Context(), Feed{
		Name: "observation feed", Provider: string(ProviderGenericJSONFeed),
		SourceType: SourceLocalJSONFile, Path: "feed.json", AccountLabel: "primary",
		OwnerUserID: " read-owner ", WorkspaceID: " \t", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry, svc, repo, feed, filepath.Join(root, feed.Path)
}

func observationWiringBytes(t *testing.T, title, content string, pretty bool) []byte {
	t.Helper()
	feed := GenericFeed{Cursor: "observation-next", Items: []GenericItem{{
		ExternalID: "record-one", Title: title, Content: content,
		ItemType: "email", Provider: "gmail", AccountLabel: "primary",
	}}}
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(feed, "", "  ")
	} else {
		data, err = json.Marshal(feed)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func observationWiringWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func observationWiringSync(t *testing.T, registry *Registry, feed Feed, created, refreshed int) {
	t.Helper()
	report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
	if err != nil || !report.Recorded || len(report.Errors) != 0 || report.ItemsRead != 1 ||
		report.OperationsCreated != created || report.OperationsRefresh != refreshed || report.Cursor != "observation-next" {
		t.Fatalf("sync = %+v / %v, want one read, %d created, %d refreshed", report, err, created, refreshed)
	}
}

func observationWiringOperations(t *testing.T, svc *operations.Service, feed Feed) []models.Operation {
	t.Helper()
	rows, err := svc.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func assertObservationWiringBinding(t *testing.T, op models.Operation, observation operations.SourceObservation, feed Feed) {
	t.Helper()
	if observation.ID == uuid.Nil || observation.Generation <= 0 || observation.StartedAt.IsZero() ||
		observation.OwnerUserID != feed.OwnerUserID || observation.WorkspaceID != feed.WorkspaceID ||
		observation.OriginID != feed.ID || observation.ConfigDigest != feed.SourceObservationStart().ConfigDigest ||
		op.SourceObservationID == nil || *op.SourceObservationID != observation.ID ||
		op.SourceObservationGeneration != observation.Generation || op.SourceIdentityHash == "" ||
		op.AccountFeedID == nil || *op.AccountFeedID != feed.ID {
		t.Fatalf("operation not bound to real pre-read observation: op=%+v observation=%+v", op, observation)
	}
}

func TestRegistrySyncMintsObservationBeforeLocalFileReadAndFreezesProvenance(t *testing.T) {
	registry, svc, repo, feed, path := newObservationWiringRegistry(t)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture must not exist before observation: %v", err)
	}
	// Mint through the real store, then publish the file before Begin returns.
	// A fetch moved ahead of Begin sees a missing file, not this item.
	repo.afterBegin = func(observation operations.SourceObservation) {
		if observation.ID == uuid.Nil || len(repo.observations) != 1 || repo.lookups != 0 || repo.creates != 0 {
			t.Fatal("file publication preceded the real observation or intake started early")
		}
		observationWiringWrite(t, path, observationWiringBytes(t, "published after mint", "first body", false))
	}
	observationWiringSync(t, registry, feed, 1, 0)
	if len(repo.starts) != 1 || len(repo.observations) != 1 || repo.starts[0] != feed.SourceObservationStart() ||
		feed.OwnerUserID != "read-owner" || feed.WorkspaceID != "local" {
		t.Fatalf("registry did not mint with canonical configured scope: %+v / %+v", feed, repo.starts)
	}
	rows := observationWiringOperations(t, svc, feed)
	if len(rows) != 1 || rows[0].Title != "published after mint" {
		t.Fatalf("registry did not read the post-mint file: %+v", rows)
	}
	op := rows[0]
	assertObservationWiringBinding(t, op, repo.observations[0], feed)
	beforeEvents, err := svc.Events(op.ID)
	if err != nil || !observationWiringCreationAudit(beforeEvents, 1) {
		t.Fatalf("creation audit = %+v / %v", beforeEvents, err)
	}
	for _, mutation := range []string{"id", "generation", "feed"} {
		t.Run(mutation, func(t *testing.T) {
			changed := op
			other := uuid.New()
			switch mutation {
			case "id":
				changed.SourceObservationID = &other
			case "generation":
				changed.SourceObservationGeneration++
			case "feed":
				changed.AccountFeedID = &other
			}
			if _, err := svc.Save(changed, "test_mutation", "hai", "must refuse"); !errors.Is(err, operations.ErrSourceIdentityImmutable) {
				t.Fatalf("observation provenance mutation = %v", err)
			}
			stored, err := svc.Get(feed.OwnerUserID, feed.WorkspaceID, op.ID)
			if err != nil || stored == nil || !reflect.DeepEqual(*stored, op) {
				t.Fatalf("refused mutation changed operation: %+v / %v", stored, err)
			}
			events, err := svc.Events(op.ID)
			if err != nil || !reflect.DeepEqual(events, beforeEvents) {
				t.Fatalf("refused mutation changed audit: %+v / %v", events, err)
			}
		})
	}
}

func TestRegistryDuplicateReadsDoNotPromoteObservationAndRevisionGetsLaterGeneration(t *testing.T) {
	registry, svc, repo, feed, path := newObservationWiringRegistry(t)
	observationWiringWrite(t, path, observationWiringBytes(t, "original", "first body", false))
	observationWiringSync(t, registry, feed, 1, 0)
	firstRows := observationWiringOperations(t, svc, feed)
	if len(firstRows) != 1 {
		t.Fatalf("first operation count = %d", len(firstRows))
	}
	first := firstRows[0]
	assertObservationWiringBinding(t, first, repo.observations[0], feed)
	beforeEvents, err := svc.Events(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	observationWiringSync(t, registry, feed, 0, 1)
	unchanged, err := svc.Get(feed.OwnerUserID, feed.WorkspaceID, first.ID)
	if err != nil || unchanged == nil || !reflect.DeepEqual(*unchanged, first) {
		t.Fatalf("identical duplicate promoted/changed operation: %+v / %v", unchanged, err)
	}
	events, err := svc.Events(first.ID)
	if err != nil || !observationWiringCreationAudit(events, 2) {
		t.Fatalf("identical duplicate changed audit: %+v / %v", events, err)
	}
	// Raw evidence can change without a semantic revision. The refresh is audited,
	// but must retain the operation's original observation rather than the new one.
	observationWiringWrite(t, path, observationWiringBytes(t, "original", "first body", true))
	observationWiringSync(t, registry, feed, 0, 1)
	refreshed, err := svc.Get(feed.OwnerUserID, feed.WorkspaceID, first.ID)
	if err != nil || refreshed == nil || refreshed.EvidenceJSON == first.EvidenceJSON || refreshed.Version != first.Version+1 {
		t.Fatalf("expected real duplicate evidence refresh: %+v / %v", refreshed, err)
	}
	assertObservationWiringBinding(t, *refreshed, repo.observations[0], feed)
	events, err = svc.Events(first.ID)
	refreshEvents := 0
	for _, event := range events {
		if event.EventType == "source_evidence_refreshed" {
			refreshEvents++
		}
	}
	if err != nil || len(events) != len(beforeEvents)+3 || refreshEvents != 1 || !observationWiringCreationAudit(events, 3) {
		t.Fatalf("evidence refresh audit = %+v / %v", events, err)
	}
	observationWiringWrite(t, path, observationWiringBytes(t, "revised", "second body", false))
	observationWiringSync(t, registry, feed, 1, 0)
	rows := observationWiringOperations(t, svc, feed)
	if len(rows) != 2 || len(repo.observations) != 4 {
		t.Fatalf("revised read rows=%d observations=%d", len(rows), len(repo.observations))
	}
	for i, observation := range repo.observations {
		if i > 0 && observation.Generation <= repo.observations[i-1].Generation {
			t.Fatal("each actual read must have a greater minted generation")
		}
	}
	for _, op := range rows {
		if op.ID == first.ID {
			if !reflect.DeepEqual(op, *refreshed) {
				t.Fatal("revised item overwrote or promoted the old operation")
			}
			continue
		}
		assertObservationWiringBinding(t, op, repo.observations[3], feed)
		if op.SourceIdentityHash != first.SourceIdentityHash || op.SourceRevisionHash == first.SourceRevisionHash ||
			op.SourceObservationGeneration <= first.SourceObservationGeneration {
			t.Fatal("new revision did not preserve semantic identity with a later observation")
		}
	}
}

func TestRegistryDuplicateDoesNotBackfillUnobservedIdentifiedOperation(t *testing.T) {
	registry, svc, repo, feed, path := newObservationWiringRegistry(t)
	data := observationWiringBytes(t, "original", "first body", false)
	observationWiringWrite(t, path, data)
	parsed, err := ParseGenericFeed(data, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	in, err := feed.ToOperationInput(parsed.Items[0].ToFeedItem())
	if err != nil {
		t.Fatal(err)
	}
	seed, err := svc.IngestContext(t.Context(), in)
	if err != nil || !seed.Created || seed.Operation.SourceObservationID != nil || seed.Operation.SourceObservationGeneration != 0 {
		t.Fatalf("unobserved seed = %+v / %v", seed, err)
	}
	beforeEvents, err := svc.Events(seed.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	rejected, found := registry.Sync(t.Context(), feed.ID)
	if !found || rejected.OperationsCreated != 0 || rejected.OperationsRefresh != 0 || len(rejected.Errors) != 1 {
		t.Fatalf("unobserved row must require reconciliation: %+v", rejected)
	}
	rows := observationWiringOperations(t, svc, feed)
	if len(rows) != 1 || len(repo.observations) != 1 || !reflect.DeepEqual(rows[0], seed.Operation) {
		t.Fatalf("duplicate backfilled or recreated old operation: %+v", rows)
	}
	events, err := svc.Events(seed.Operation.ID)
	if err != nil || !reflect.DeepEqual(events, beforeEvents) {
		t.Fatalf("duplicate backfill changed audit: %+v / %v", events, err)
	}
}

// Creation remains exactly once; accepted observations have their own audit
// entries even when the operation's immutable provenance remains unchanged.
func observationWiringCreationAudit(events []models.OperationEvent, publications int) bool {
	created, published := 0, 0
	for _, event := range events {
		switch event.EventType {
		case "created":
			created++
		case "source_head_published":
			published++
		}
	}
	return created == 1 && published == publications
}

func TestRegistryObservationBeginFailureStopsIntakeAndRedactsConfiguration(t *testing.T) {
	for _, readable := range []bool{false, true} {
		name := "missing file"
		if readable {
			name = "readable file"
		}
		t.Run(name, func(t *testing.T) {
			registry, svc, repo, feed, path := newObservationWiringRegistry(t)
			if readable {
				observationWiringWrite(t, path, observationWiringBytes(t, "must not ingest", "private body", false))
			}
			repo.beginError = errors.New("observation failure at " + path + " https://operator.invalid/feed?token=private-observation-secret password=private-password")
			report, err := registry.SyncContext(t.Context(), feedScope(feed), feed.ID)
			if err != nil || !report.Recorded || report.ItemsRead != 0 || report.OperationsCreated != 0 || report.OperationsRefresh != 0 ||
				report.Cursor != "" || len(report.Errors) != 1 || report.Errors[0] != "source observation could not be recorded; nothing further was read" {
				t.Fatalf("begin failure must precede file error/item read: %+v / %v", report, err)
			}
			if len(repo.starts) != 1 || len(repo.observations) != 0 || repo.lookups != 0 || repo.creates != 0 || repo.updates != 0 ||
				len(observationWiringOperations(t, svc, feed)) != 0 {
				t.Fatal("failed Begin minted authority or entered operation intake")
			}
			audit, err := registry.AuditContext(t.Context(), feedScope(feed), feed.ID)
			if err != nil || len(audit) != 3 {
				t.Fatalf("failed attempt audit = %+v / %v", audit, err)
			}
			wantEvents := map[string]string{
				"registered":   "feed registered",
				"sync_started": "feed sync started",
				"sync_failed":  "read 0 items, 0 new operations, 0 existing operations, 1 rejected, 0 privacy-flagged",
			}
			for _, event := range audit {
				message, expected := wantEvents[event.EventType]
				if !expected || event.Message != message || event.FeedID != feed.ID.String() {
					t.Fatalf("unexpected failed-attempt event: %+v", event)
				}
				delete(wantEvents, event.EventType)
			}
			if len(wantEvents) != 0 {
				t.Fatalf("failed-attempt audit omitted event types: %+v", wantEvents)
			}
			public, err := json.Marshal(struct {
				Report SyncReport
				Audit  []AuditEvent
			}{report, audit})
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{path, "operator.invalid", "private-observation-secret", "private-password", "must not ingest", "private body"} {
				encodedSecret, err := json.Marshal(secret)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(public), string(encodedSecret[1:len(encodedSecret)-1])) {
					t.Fatalf("public observation failure leaked %q: %s", secret, public)
				}
			}
			rows, err := registry.ListContext(t.Context(), feedScope(feed))
			if err != nil || len(rows) != 1 || rows[0].LastSyncedAt != nil || rows[0].LastAttemptAt == nil || rows[0].LastItemsRead != 0 || rows[0].SyncState != "idle" {
				t.Fatalf("failed Begin advanced successful feed state or retained claim: %+v / %v", rows, err)
			}
		})
	}
}

func TestFeedSourceObservationStartHashesCompleteConfigurationAndCanonicalScope(t *testing.T) {
	feed := Feed{ID: uuid.New(), Name: "full configuration", Provider: string(ProviderGenericJSONFeed),
		AccountLabel: "primary", SourceType: SourceLocalJSONFile, Path: "feed.json",
		URL: "https://feed.invalid/items", OwnerUserID: "read-owner", WorkspaceID: "local",
		ProjectKey: "project", OperationType: "review_source_item", Enabled: true}
	start := feed.SourceObservationStart()
	encoded, err := json.Marshal(struct {
		Version int  `json:"version"`
		Feed    Feed `json:"feed"`
	}{1, feed})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if start.OriginID != feed.ID || start.OwnerUserID != feed.OwnerUserID || start.WorkspaceID != feed.WorkspaceID ||
		start.ConfigDigest != hex.EncodeToString(digest[:]) || start != feed.SourceObservationStart() {
		t.Fatalf("observation start is not a deterministic full-config SHA-256: %+v", start)
	}
	for _, change := range []struct {
		name  string
		apply func(*Feed)
	}{
		{"id", func(f *Feed) { f.ID = uuid.New() }},
		{"name", func(f *Feed) { f.Name = "renamed" }},
		{"provider", func(f *Feed) { f.Provider = "gmail" }},
		{"account", func(f *Feed) { f.AccountLabel = "secondary" }},
		{"source type", func(f *Feed) { f.SourceType = SourceHTTPJSONFeed }},
		{"path", func(f *Feed) { f.Path = "other.json" }},
		{"url", func(f *Feed) { f.URL = "https://feed.invalid/other" }},
		{"owner", func(f *Feed) { f.OwnerUserID = "other-owner" }},
		{"workspace", func(f *Feed) { f.WorkspaceID = "other-workspace" }},
		{"project", func(f *Feed) { f.ProjectKey = "other-project" }},
		{"operation type", func(f *Feed) { f.OperationType = "other-type" }},
		{"enabled", func(f *Feed) { f.Enabled = false }},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := feed
			change.apply(&changed)
			if changed.SourceObservationStart().ConfigDigest == start.ConfigDigest {
				t.Fatalf("configuration field %s did not change observation digest", change.name)
			}
		})
	}
	feed.OwnerUserID, feed.WorkspaceID = " read-owner ", " \t"
	canonical := feed.SourceObservationStart()
	if canonical.OwnerUserID != "read-owner" || canonical.WorkspaceID != "local" || canonical.OriginID != feed.ID {
		t.Fatalf("start must trim owner and default blank workspace without guessing origin: %+v", canonical)
	}
	feed.OwnerUserID, feed.WorkspaceID = "", " custom-space "
	canonical = feed.SourceObservationStart()
	if canonical.OwnerUserID != "" || canonical.WorkspaceID != "custom-space" {
		t.Fatalf("start invented an owner or failed to canonicalize explicit scope: %+v", canonical)
	}
}
