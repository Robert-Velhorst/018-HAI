package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func exerciseReferenceControls(t *testing.T, service func() *Service) {
	t.Helper()
	ctx := context.Background()
	key := ReferenceCheckpointKey{OwnerUserID: "control-" + uuid.NewString(), WorkspaceID: "local", FeedID: uuid.New()}
	in := referenceInput(1)
	in.OwnerUserID, in.AccountFeedID, in.DedupeKey = key.OwnerUserID, &key.FeedID, key.FeedID.String()+":listing:1"
	page := func(revision uint64) error {
		_, err := service().SyncReferencePage(ctx, key, revision, ReferenceCheckpoint{}, "MQ", []NewOperationInput{in})
		return err
	}
	if err := page(1); err == nil {
		t.Fatal("unregistered control authorized a page")
	}
	first, err := service().EnsureReferenceSource(ctx, key, true)
	if err != nil || !first.Enabled || first.Revision != 1 {
		t.Fatalf("initialize: %+v %v", first, err)
	}
	if _, err := service().SetReferenceSourceEnabled(ctx, key, false); err != nil {
		t.Fatal(err)
	}
	// Startup configuration is not an instruction to undo an operator's stop.
	stopped, err := service().EnsureReferenceSource(ctx, key, true)
	if err != nil || stopped.Enabled || stopped.Revision != 2 {
		t.Fatalf("restart reset disabled state: %+v %v", stopped, err)
	}
	if err := page(first.Revision); !errors.Is(err, ErrReferenceSourceDisabled) {
		t.Fatalf("disabled page: %v", err)
	}
	again, err := service().SetReferenceSourceEnabled(ctx, key, false)
	if err != nil || again != stopped {
		t.Fatal("idempotent disable changed the fence")
	}
	enabled, err := service().SetReferenceSourceEnabled(ctx, key, true)
	if err != nil || !enabled.Enabled || enabled.Revision != 3 {
		t.Fatal("enable did not advance the fence")
	}
	if err := page(first.Revision); !errors.Is(err, ErrReferenceSourceChanged) {
		t.Fatalf("disable/re-enable authorized a stale page: %v", err)
	}
	rows, err := service().List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID})
	cp, cpErr := service().LoadReferenceCheckpoint(ctx, key)
	if err != nil || cpErr != nil || len(rows) != 0 || cp != (ReferenceCheckpoint{}) {
		t.Fatal("stopped/stale page left source records or progress")
	}
	for _, foreign := range []ReferenceCheckpointKey{
		{OwnerUserID: key.OwnerUserID + "-other", WorkspaceID: key.WorkspaceID, FeedID: key.FeedID},
		{OwnerUserID: key.OwnerUserID, WorkspaceID: "other", FeedID: key.FeedID},
		{OwnerUserID: key.OwnerUserID, WorkspaceID: key.WorkspaceID, FeedID: uuid.New()},
	} {
		if _, err := service().GetReferenceSourceControl(ctx, foreign); err == nil {
			t.Fatal("control read crossed owner/workspace/source binding")
		}
		if _, err := service().SetReferenceSourceEnabled(ctx, foreign, false); err == nil {
			t.Fatal("unknown control was implicitly registered by a state change")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service().SetReferenceSourceEnabled(canceled, key, false); err == nil {
		t.Fatal("canceled control change succeeded")
	}
	current, err := service().GetReferenceSourceControl(ctx, key)
	if err != nil || current != enabled {
		t.Fatal("failed state change altered control")
	}
	if err := page(current.Revision); err != nil {
		t.Fatalf("fresh sync could not resume after explicit enable: %v", err)
	}
}

func TestReferenceSourceControlsMemory(t *testing.T) {
	repo := NewMemoryRepository()
	exerciseReferenceControls(t, func() *Service { return NewService(repo) })
}
