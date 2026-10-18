package operations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func referenceInput(sequence uint64) NewOperationInput {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	return NewOperationInput{OwnerUserID: "reference-owner", WorkspaceID: "local",
		Title: "Chair", Description: "Current description", SourceType: "autoposter_api",
		OperationType: "read_only_reference", DedupeKey: "test-feed:listing:1",
		SourceSequence: sequence, SourceReceivedAt: &now, EvidenceJSON: `{"title":"Chair"}`}
}

func exerciseReferenceStore(t *testing.T, svc *Service) NewOperationInput {
	t.Helper()
	ctx := context.Background()
	initial := referenceInput(1)
	initial.OwnerUserID += "-" + uuid.NewString()
	first, err := svc.SyncReference(ctx, initial)
	if err != nil || !first.Created {
		t.Fatalf("create: %v", err)
	}
	updated := initial
	updated.SourceSequence = 3
	updated.Title, updated.Description, updated.EvidenceJSON = "Deleted listing:1", "", `{"deleted":true}`
	// Same timestamp deliberately: only source sequence can preserve order.
	last, err := svc.SyncReference(ctx, updated)
	if err != nil || !last.Created || last.Operation.ID == first.Operation.ID {
		t.Fatalf("append immutable source revision: %v", err)
	}
	for _, replay := range []NewOperationInput{initial, updated} {
		got, err := svc.SyncReference(ctx, replay)
		wantTitle := replay.Title
		if err != nil || got.Operation.Title != wantTitle {
			t.Fatalf("replay %d changed immutable source state: %v", replay.SourceSequence, err)
		}
	}
	foreign := initial
	foreign.OwnerUserID += "-other"
	other, err := svc.SyncReference(ctx, foreign)
	if err != nil || other.Operation.ID == first.Operation.ID {
		t.Fatalf("owner isolation: %v", err)
	}
	for _, owner := range []string{initial.OwnerUserID, foreign.OwnerUserID} {
		rows, err := svc.List(Filter{OwnerUserID: owner, WorkspaceID: "local"})
		wantRows := 2
		if owner == foreign.OwnerUserID {
			wantRows = 1
		}
		if err != nil || len(rows) != wantRows {
			t.Fatalf("reference rows for %s: %v", owner, err)
		}
		for _, row := range rows {
			if row.Status != "archived" {
				t.Fatal("references must remain terminal")
			}
		}
		due, err := svc.ListDue(owner, "local", 100)
		if err != nil || len(due) != 0 {
			t.Fatal("references must never be executable work")
		}
	}
	return initial
}

func TestReferenceSequenceAndOwnership(t *testing.T) {
	exerciseReferenceStore(t, NewService(NewMemoryRepository()))
}

func TestReferenceRejectsMissingSequenceAndCanceledContext(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if _, err := svc.SyncReference(context.Background(), referenceInput(0)); err == nil {
		t.Fatal("missing sequence accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.SyncReference(ctx, referenceInput(1)); err == nil {
		t.Fatal("canceled write accepted")
	}
}
