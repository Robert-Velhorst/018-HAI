package accountfeed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
)

type syncAtomicFailureRepository struct {
	*operations.MemoryRepository
	rejectedTitle string
	rejectAll     bool
}

func (r *syncAtomicFailureRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if r.rejectAll || op.Title == r.rejectedTitle {
		return nil, errors.New("private storage path or credential must not be exposed")
	}
	return r.MemoryRepository.CreateWithEvent(op, event)
}

func (r *syncAtomicFailureRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.rejectAll || op.Title == r.rejectedTitle {
		return nil, errors.New("private storage path or credential must not be exposed")
	}
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func TestSyncAuditDoesNotClaimSuccessAfterAtomicIntakeFailure(t *testing.T) {
	for _, allFailed := range []bool{false, true} {
		name := "partial"
		if allFailed {
			name = "all_failed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "feed.json"), []byte(genericFeed), 0o600); err != nil {
				t.Fatal(err)
			}
			repo := &syncAtomicFailureRepository{MemoryRepository: operations.NewMemoryRepository(), rejectedTitle: "Bug", rejectAll: allFailed}
			reg := NewRegistry(operations.NewService(repo), nil, FetchOptions{FeedsRoot: root})
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			reg.now = func() time.Time { return now }
			feed, err := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile,
				Path: "feed.json", OwnerUserID: "u", WorkspaceID: "local", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			report, found := reg.Sync(context.Background(), feed.ID)
			wantCreated, wantErrors, wantAudit := 1, 1, "sync_partial"
			if allFailed {
				wantCreated, wantErrors, wantAudit = 0, 2, "sync_failed"
			}
			if !found || report.ItemsRead != 2 || report.OperationsCreated != wantCreated || len(report.Errors) != wantErrors || report.Cursor != "" {
				t.Fatalf("failed item advertised progress: %#v", report)
			}
			for _, message := range report.Errors {
				if message != "a feed item could not be recorded" {
					t.Fatalf("raw storage error leaked: %q", message)
				}
			}
			audit := reg.Audit(feed.ID)
			if len(audit) < 1 || audit[0].EventType != wantAudit || strings.Contains(audit[0].Message, "credential") {
				t.Fatalf("failure audit misleading or sensitive: %#v", audit)
			}
			health := reg.HealthForOwner("u")
			if len(health) != 1 || health[0].LastSyncedAt != nil || health[0].LastAttemptAt == nil || !health[0].LastAttemptAt.Equal(now) {
				t.Fatalf("failed attempt advertised successful freshness: %#v", health)
			}
			// Repeated failure reuses committed active pairs without skipping rejected items.
			retry, found := reg.Sync(context.Background(), feed.ID)
			if !found || retry.OperationsCreated != 0 || retry.OperationsRefresh != wantCreated || len(retry.Errors) != wantErrors || retry.Cursor != "" {
				t.Fatalf("retry skipped or duplicated failed intake: %#v", retry)
			}
			stored, err := repo.List(operations.Filter{OwnerUserID: "u", WorkspaceID: "local"})
			if err != nil || len(stored) != wantCreated {
				t.Fatalf("partial durable count mismatch: operations=%+v err=%v", stored, err)
			}
			for _, op := range stored {
				events, err := repo.ListEvents(op.ID, 0)
				if err != nil || len(events) != 3 || !observationWiringCreationAudit(events, 2) {
					t.Fatalf("retry lost or duplicated creation audit: events=%+v err=%v", events, err)
				}
			}
			repo.rejectAll, repo.rejectedTitle = false, ""
			now = now.Add(time.Minute)
			recovered, found := reg.Sync(context.Background(), feed.ID)
			if !found || len(recovered.Errors) != 0 || recovered.Cursor != "next-42" ||
				recovered.OperationsCreated != 2-wantCreated || recovered.OperationsRefresh != wantCreated {
				t.Fatalf("recovery did not record rejected items exactly once: %#v", recovered)
			}
			stored, err = repo.List(operations.Filter{OwnerUserID: "u", WorkspaceID: "local"})
			if err != nil || len(stored) != 2 {
				t.Fatalf("recovery operation count: operations=%+v err=%v", stored, err)
			}
			for _, op := range stored {
				events, err := repo.ListEvents(op.ID, 0)
				if err != nil || !observationWiringCreationAudit(events, 1) && !observationWiringCreationAudit(events, 3) {
					t.Fatalf("recovery creation audit: events=%+v err=%v", events, err)
				}
			}
			health = reg.HealthForOwner("u")
			if health[0].LastSyncedAt == nil || !health[0].LastSyncedAt.Equal(now) || reg.Audit(feed.ID)[0].EventType != "synced" {
				t.Fatalf("recovery did not publish successful local ingestion: %#v", health)
			}
		})
	}
}

func TestSyncFailurePreservesLastSuccessfulTimestamp(t *testing.T) {
	reg, root := newTestRegistry(t)
	path := filepath.Join(root, "feed.json")
	if err := os.WriteFile(path, []byte(genericFeed), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return now }
	feed, err := reg.Register(Feed{Name: "inbox", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile,
		Path: "feed.json", OwnerUserID: "u", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first, found := reg.Sync(context.Background(), feed.ID)
	if !found || len(first.Errors) != 0 || first.Cursor != "next-42" {
		t.Fatalf("initial successful sync: %#v", first)
	}
	succeededAt := now
	now = now.Add(time.Minute)
	if err := os.WriteFile(path, []byte(`{"items":`), 0o600); err != nil {
		t.Fatal(err)
	}
	failed, found := reg.Sync(context.Background(), feed.ID)
	if !found || len(failed.Errors) == 0 || failed.Cursor != "" {
		t.Fatalf("invalid feed sync: %#v", failed)
	}
	health := reg.HealthForOwner("u")
	if len(health) != 1 || health[0].LastSyncedAt == nil || !health[0].LastSyncedAt.Equal(succeededAt) ||
		health[0].LastAttemptAt == nil || !health[0].LastAttemptAt.Equal(now) {
		t.Fatalf("failed attempt rewrote successful sync timestamp: %#v", health)
	}
}
