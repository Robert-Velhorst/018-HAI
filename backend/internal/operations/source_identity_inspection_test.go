package operations

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestInspectSourceIdentityObservesBothKeysWithoutWrites(t *testing.T) {
	for _, state := range []string{"unseen", "canonical", "historical", "coexisting"} {
		t.Run(state, func(t *testing.T) {
			base := NewMemoryRepository()
			in := sampleInput()
			in.LegacyDedupeKey = "historical-key"
			for _, key := range []string{in.DedupeKey, in.LegacyDedupeKey} {
				if (key == in.DedupeKey && (state == "canonical" || state == "coexisting")) ||
					(key == in.LegacyDedupeKey && (state == "historical" || state == "coexisting")) {
					seed := in
					seed.DedupeKey, seed.LegacyDedupeKey = key, ""
					if _, err := NewService(base).Ingest(seed); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, events := ingestAtomicSnapshot(base)
			ctx := t.Context()
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			got, err := NewService(repo).InspectSourceIdentity(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Canonical != nil) != (state == "canonical" || state == "coexisting") ||
				(got.Historical != nil) != (state == "historical" || state == "coexisting") {
				t.Fatalf("state %s: %+v", state, got)
			}
			if !reflect.DeepEqual(repo.keys, []string{in.DedupeKey, in.LegacyDedupeKey}) || repo.creates != 0 || repo.updates != 0 || repo.legacyCalls != 0 {
				t.Fatalf("inspection did not remain read-only: %+v", repo)
			}
			ingestAtomicAssertSnapshot(t, base, before, events)
		})
	}
}

func TestInspectSourceIdentityFailsClosedOnInvalidRepositoryResults(t *testing.T) {
	for _, invalid := range []string{"nil", "nil_id", "owner", "workspace", "key", "error", "historical_error", "cancel"} {
		t.Run(invalid, func(t *testing.T) {
			base := NewMemoryRepository()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			in := sampleInput()
			in.LegacyDedupeKey = "historical-key"
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
				op := &models.Operation{ID: uuid.New(), OwnerUserID: owner, WorkspaceID: workspace, DedupeKey: key}
				switch invalid {
				case "nil":
					return nil, true, nil
				case "nil_id":
					op.ID = uuid.Nil
				case "owner":
					op.OwnerUserID = "other-owner"
				case "workspace":
					op.WorkspaceID = "other-workspace"
				case "key":
					op.DedupeKey = "other-key"
				case "error":
					return op, true, errors.New("storage lookup unavailable")
				case "historical_error":
					if key == in.LegacyDedupeKey {
						return nil, false, errors.New("historical lookup unavailable")
					}
				case "cancel":
					cancel()
				}
				return op, true, nil
			}
			got, err := NewService(repo).InspectSourceIdentity(ctx, in)
			if err == nil || got.Canonical != nil || got.Historical != nil || repo.creates != 0 || repo.updates != 0 {
				t.Fatalf("invalid result returned partial identity: got=%+v err=%v", got, err)
			}
			if invalid == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestInspectSourceIdentityValidatesScopeAndContextCapability(t *testing.T) {
	base := NewMemoryRepository()
	in := sampleInput()
	legacy := &ingestAtomicLegacyProbe{Repository: base}
	if _, err := NewService(legacy).InspectSourceIdentity(t.Context(), in); !errors.Is(err, ErrContextIntakeUnsupported) {
		t.Fatalf("legacy store accepted: %v", err)
	}
	for _, field := range []string{"owner", "key"} {
		bad := in
		if field == "owner" {
			bad.OwnerUserID = " "
		} else {
			bad.DedupeKey = " "
		}
		if _, err := NewService(base).InspectSourceIdentity(t.Context(), bad); err == nil {
			t.Fatal("empty scope/key accepted")
		}
	}
	in.LegacyDedupeKey = in.DedupeKey
	ctx := context.Background()
	repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
	if _, err := NewService(repo).InspectSourceIdentity(nil, in); err != nil || repo.lookups != 1 {
		t.Fatalf("nil context or duplicate key handling: %v / %d", err, repo.lookups)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewService(base).InspectSourceIdentity(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
