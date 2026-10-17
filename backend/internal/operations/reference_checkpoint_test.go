package operations

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// Run the same behavioral contract against memory and real PostgreSQL stores.
func exerciseReferenceCheckpoints(t *testing.T, service func() *Service) {
	t.Helper()
	ctx := context.Background()
	key := ReferenceCheckpointKey{OwnerUserID: "checkpoint-" + uuid.NewString(), WorkspaceID: "local", FeedID: uuid.New()}
	if _, err := service().EnsureReferenceSource(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	input := func(sequence uint64, record string) NewOperationInput {
		in := referenceInput(sequence)
		in.OwnerUserID, in.WorkspaceID, in.AccountFeedID = key.OwnerUserID, key.WorkspaceID, &key.FeedID
		in.DedupeKey = key.FeedID.String() + ":" + record
		return in
	}
	load := func() ReferenceCheckpoint {
		t.Helper()
		cp, err := service().LoadReferenceCheckpoint(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return cp
	}
	if load() != (ReferenceCheckpoint{}) {
		t.Fatal("new source has progress")
	}
	// The second record fails during storage, after the first has been staged.
	bad := input(2, "second")
	bad.OperationType = "executable"
	if results, err := service().SyncReferencePage(ctx, key, 1, ReferenceCheckpoint{}, "Mg", []NewOperationInput{input(1, "first"), bad}); err == nil || len(results) != 0 {
		t.Fatal("failed page reported imported records")
	}
	rows, err := service().List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID})
	if err != nil || len(rows) != 0 || load() != (ReferenceCheckpoint{}) {
		t.Fatal("failed page left partial records/progress")
	}
	first := input(1, "first")
	results, err := service().SyncReferencePage(ctx, key, 1, ReferenceCheckpoint{}, "MQ", []NewOperationInput{first})
	if err != nil || len(results) != 1 || !results[0].Created {
		t.Fatalf("first commit: %v", err)
	}
	cp := load() // A fresh service must read the committed repository state.
	if cp != (ReferenceCheckpoint{Cursor: "MQ", Sequence: 1}) {
		t.Fatal("checkpoint was not committed")
	}
	// A stale writer cannot insert a different row or regress progress.
	if _, err := service().SyncReferencePage(ctx, key, 1, ReferenceCheckpoint{}, "Mw", []NewOperationInput{input(3, "stale")}); !errors.Is(err, ErrReferenceCheckpointConflict) {
		t.Fatalf("stale writer: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*NewOperationInput)
	}{
		{"owner", func(in *NewOperationInput) { in.OwnerUserID += "-other" }},
		{"workspace", func(in *NewOperationInput) { in.WorkspaceID += "-other" }},
		{"feed", func(in *NewOperationInput) { id := uuid.New(); in.AccountFeedID = &id }},
		{"sequence", func(in *NewOperationInput) { in.SourceSequence = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input(2, "invalid")
			tc.edit(&in)
			if _, err := service().SyncReferencePage(ctx, key, 1, cp, "Mg", []NewOperationInput{in}); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service().SyncReferencePage(canceled, key, 1, cp, "Mg", []NewOperationInput{input(2, "canceled")}); err == nil {
		t.Fatal("canceled commit succeeded")
	}
	if _, err := service().LoadReferenceCheckpoint(canceled, key); err == nil {
		t.Fatal("canceled checkpoint read succeeded")
	}
	for _, foreign := range []ReferenceCheckpointKey{
		{OwnerUserID: key.OwnerUserID + "-other", WorkspaceID: key.WorkspaceID, FeedID: key.FeedID},
		{OwnerUserID: key.OwnerUserID, WorkspaceID: "other", FeedID: key.FeedID},
		{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID, FeedID: uuid.New()},
	} {
		if got, err := service().LoadReferenceCheckpoint(ctx, foreign); err != nil || got != (ReferenceCheckpoint{}) {
			t.Fatal("checkpoint crossed a source/owner/workspace boundary")
		}
	}
	if load() != cp {
		t.Fatal("rejected page changed checkpoint")
	}
	rows, _ = service().List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID})
	if len(rows) != 1 {
		t.Fatal("rejected page inserted records")
	}
	// Two snapshots of one listing in one page remain ordered, with deletion last.
	updated := input(2, "first")
	deleted := input(3, "first")
	deleted.Title, deleted.Description, deleted.EvidenceJSON = "Deleted", "", `{"deleted":true}`
	results, err = service().SyncReferencePage(ctx, key, 1, cp, "Mw", []NewOperationInput{updated, deleted})
	if err != nil || len(results) != 2 || load().Sequence != 3 {
		t.Fatalf("update/delete page: %v", err)
	}
	rows, _ = service().List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID})
	if len(rows) != 3 {
		t.Fatal("page lost immutable source versions")
	}
	deletedFound := false
	for _, row := range rows {
		if row.Title == "Deleted" && row.Description == "" && row.Status == "archived" {
			deletedFound = true
		}
	}
	if !deletedFound {
		t.Fatal("page lost its final tombstone")
	}
	// Independent callers that fetched the same checkpoint race at commit time.
	cp = load()
	var wg sync.WaitGroup
	var committed atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			in := input(uint64(4+i), "first")
			in.Title = fmt.Sprintf("candidate-%d", in.SourceSequence)
			_, err := service().SyncReferencePage(ctx, key, 1, cp, fmt.Sprintf("cursor_%d", i), []NewOperationInput{in})
			if err == nil {
				committed.Add(1)
			} else if !errors.Is(err, ErrReferenceCheckpointConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, _ = service().List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID})
	if committed.Load() != 1 || len(rows) != 4 {
		t.Fatal("concurrent pages did not atomically elect one matching snapshot/checkpoint")
	}
}

func TestReferenceCheckpointsMemory(t *testing.T) {
	repo := NewMemoryRepository()
	exerciseReferenceCheckpoints(t, func() *Service { return NewService(repo) })
}
