package accountfeed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

func TestFeedEnvelopeRequiresExplicitItems(t *testing.T) {
	for _, payload := range []string{`{}`, `{"cursor":"unsafe"}`, `{"cursor":"unsafe","items":null}`,
		`{"cursor":"unsafe","error":"upstream unavailable"}`, `{"items":{}}`, `{"items":"[]"}`} {
		t.Run(payload, func(t *testing.T) {
			parsed, err := ParseGenericFeed([]byte(payload), 0, 0)
			if err == nil || parsed.Cursor != "" || len(parsed.Items) != 0 {
				t.Fatalf("invalid envelope was accepted: %+v err=%v", parsed, err)
			}
			reg, root := newTestRegistry(t)
			if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			feed, err := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile,
				Path: "feed.json", OwnerUserID: "u", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			report, found := reg.Sync(context.Background(), feed.ID)
			if !found || len(report.Errors) == 0 || report.Cursor != "" || report.ItemsRead != 0 {
				t.Fatalf("malformed envelope advertised progress: %+v", report)
			}
			health := reg.HealthForOwner("u")
			if health[0].LastSyncedAt != nil || health[0].LastAttemptAt == nil || reg.Audit(feed.ID)[0].EventType != "sync_failed" {
				t.Fatalf("malformed envelope advertised success: %+v", health)
			}
		})
	}
	for _, payload := range []string{`[]`, `{"items":[]}`, `{"cursor":"next","items":[]}`} {
		parsed, err := ParseGenericFeed([]byte(payload), 0, 0)
		if err != nil || len(parsed.Items) != 0 {
			t.Fatalf("explicit empty collection must remain valid: %+v err=%v", parsed, err)
		}
	}
}

type blockingFeedIntakeRepository struct {
	*operations.MemoryRepository
	blocked atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (r *blockingFeedIntakeRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return r.CreateWithEventContext(context.Background(), op, event)
}

func (r *blockingFeedIntakeRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if r.blocked.CompareAndSwap(false, true) {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func TestOverlappingFeedSyncCannotRegressEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "feed.json")
	// Only an extra evidence annotation changes here, not the normalized
	// title/body/type/time whose changes must now create a separate revision.
	old := `{"items":[{"externalId":"same","title":"same","content":"unchanged","itemType":"message","provider":"generic_json_feed","note":"older"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &blockingFeedIntakeRepository{MemoryRepository: operations.NewMemoryRepository(), started: make(chan struct{}), release: make(chan struct{})}
	reg := NewRegistry(operations.NewService(repo), nil, FetchOptions{FeedsRoot: root})
	feed, err := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile,
		Path: "feed.json", OwnerUserID: "u", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan SyncReport, 1)
	go func() { report, _ := reg.Sync(context.Background(), feed.ID); done <- report }()
	released := false
	defer func() {
		if !released {
			close(repo.release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("owned sync did not finish after release")
			}
		}
	}()
	select {
	case <-repo.started:
	case <-time.After(5 * time.Second):
		t.Fatal("old observation did not reach intake")
	}
	newer := strings.Replace(old, "older", "newer", 1)
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	startedHealth := reg.HealthForOwner("u")[0]
	startedAudit := reg.Audit(feed.ID)
	if startedHealth.LastAttemptAt == nil || startedHealth.SyncState != "running_or_interrupted" || len(startedAudit) != 2 || startedAudit[0].EventType != "sync_started" {
		t.Fatal("owned attempt was not recorded before intake")
	}
	overlap, found := reg.SyncForOwner(context.Background(), feed.ID, "u")
	due := reg.SyncDueForOwner(context.Background(), "u")
	for _, report := range append([]SyncReport{overlap}, due...) {
		if !found || len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "running or interrupted") || report.ItemsRead != 0 || report.Cursor != "" || report.Recorded {
			t.Fatalf("overlap executed another observation: %+v", report)
		}
	}
	if len(due) != 1 || !reg.HealthForOwner("u")[0].LastAttemptAt.Equal(*startedHealth.LastAttemptAt) || len(reg.Audit(feed.ID)) != len(startedAudit) {
		t.Fatal("rejected overlap changed freshness or claimed an attempt")
	}
	// A busy feed does not hold the registry lock across I/O or block another feed.
	if err := os.WriteFile(filepath.Join(root, "empty.json"), []byte(`{"items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := reg.Register(Feed{Name: "other", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile,
		Path: "empty.json", OwnerUserID: "other-owner", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	independent, found := reg.SyncForOwner(context.Background(), other.ID, "other-owner")
	if !found || len(independent.Errors) != 0 {
		t.Fatalf("independent feed was blocked: %+v", independent)
	}
	close(repo.release)
	released = true
	select {
	case first := <-done:
		if len(first.Errors) != 0 || first.OperationsCreated != 1 {
			t.Fatalf("first sync: %+v", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned sync did not complete")
	}
	retry, found := reg.SyncForOwner(context.Background(), feed.ID, "u")
	if !found || len(retry.Errors) != 0 || retry.OperationsCreated != 0 || retry.OperationsRefresh != 1 {
		t.Fatalf("new observation could not refresh after release: %+v", retry)
	}
	stored, err := repo.List(operations.Filter{OwnerUserID: "u", WorkspaceID: "local"})
	if err != nil || len(stored) != 1 || !strings.Contains(string(stored[0].EvidenceJSON), `"note":"newer"`) {
		t.Fatalf("latest evidence lost: %+v err=%v", stored, err)
	}
}
