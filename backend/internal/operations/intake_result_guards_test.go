package operations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type invalidIntakeResultRepository struct {
	*MemoryRepository
	lookup                    func(string, string, string) (*models.Operation, bool, error)
	write                     func(*models.Operation) (*models.Operation, error)
	lookups, creates, updates int
}

func (r *invalidIntakeResultRepository) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	r.lookups++
	if r.lookup != nil {
		return r.lookup(owner, workspace, key)
	}
	return r.MemoryRepository.FindByDedupeKey(owner, workspace, key)
}

func (r *invalidIntakeResultRepository) FindByDedupeKeyContext(_ context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	return r.FindByDedupeKey(owner, workspace, key)
}

func (r *invalidIntakeResultRepository) CreateWithEvent(op *models.Operation, evt *models.OperationEvent) (*models.Operation, error) {
	r.creates++
	if r.write != nil {
		return r.write(op)
	}
	return r.MemoryRepository.CreateWithEvent(op, evt)
}

func (r *invalidIntakeResultRepository) CreateWithEventContext(_ context.Context, op *models.Operation, evt *models.OperationEvent) (*models.Operation, error) {
	return r.CreateWithEvent(op, evt)
}

func (r *invalidIntakeResultRepository) UpdateWithEvent(op *models.Operation, evt *models.OperationEvent) (*models.Operation, error) {
	r.updates++
	if r.write != nil {
		return r.write(op)
	}
	return r.MemoryRepository.UpdateWithEvent(op, evt)
}

func (r *invalidIntakeResultRepository) UpdateWithEventContext(_ context.Context, op *models.Operation, evt *models.OperationEvent) (*models.Operation, error) {
	return r.UpdateWithEvent(op, evt)
}

func corruptIntakeResult(op models.Operation, kind string) (*models.Operation, bool) {
	switch kind {
	case "nil":
		return nil, true
	case "nil_id":
		op.ID = uuid.Nil
	case "owner":
		op.OwnerUserID = "private-other-owner"
	case "workspace":
		op.WorkspaceID = "private-other-workspace"
	case "key":
		op.DedupeKey = "private-other-key"
	case "archived":
		op.Status = string(StatusArchived)
	case "dismissed":
		op.Status = string(StatusDismissed)
	case "contradictory_miss":
		return &op, false
	case "different_id":
		op.ID = uuid.New()
	case "version":
		op.Version++
	default:
		panic("unknown invalid-result fixture")
	}
	return &op, true
}

func TestIntakeRejectsInvalidLookupResultsAcrossAllPaths(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, path := range []string{"canonical", "historical", "creation_race"} {
			for _, kind := range []string{"nil", "nil_id", "owner", "workspace", "key", "archived", "dismissed", "contradictory_miss"} {
				t.Run(fmt.Sprintf("context_%t/%s/%s", contextual, path, kind), func(t *testing.T) {
					base := NewMemoryRepository()
					in := sampleInput()
					in.LegacyDedupeKey = "historical-key"
					in.EvidenceJSON = `{"new":true}`
					before, events := ingestAtomicSnapshot(base)
					repo := &invalidIntakeResultRepository{MemoryRepository: base}
					repo.lookup = func(owner, workspace, key string) (*models.Operation, bool, error) {
						if (path == "historical" && key == in.DedupeKey) || (path == "creation_race" && repo.creates == 0) {
							return nil, false, nil
						}
						op, _ := ingestAtomicPair(t)
						op.OwnerUserID, op.WorkspaceID, op.DedupeKey = owner, workspace, key
						bad, found := corruptIntakeResult(op, kind)
						return bad, found, nil
					}
					if path == "creation_race" {
						repo.write = func(*models.Operation) (*models.Operation, error) { return nil, ErrDuplicateDedupeKey }
					}
					svc := NewService(repo)
					var got IngestResult
					var err error
					if contextual {
						got, err = svc.IngestContext(t.Context(), in)
					} else {
						got, err = svc.Ingest(in)
					}
					if !errors.Is(err, ErrInvalidRepositoryResult) || !reflect.DeepEqual(got, IngestResult{}) || repo.updates != 0 {
						t.Fatalf("invalid lookup was accepted: result=%+v err=%v writes=%d", got, err, repo.updates)
					}
					wantCreates := 0
					if path == "creation_race" {
						wantCreates = 1
					}
					if repo.creates != wantCreates {
						t.Fatalf("unexpected create calls: %d", repo.creates)
					}
					ingestAtomicAssertSnapshot(t, base, before, events)
				})
			}
		}
	}
}

func TestIntakeRejectsInvalidSuccessfulWriteResults(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, path := range []string{"create", "refresh"} {
			for _, kind := range []string{"nil", "nil_id", "owner", "workspace", "key", "archived", "dismissed", "different_id", "version"} {
				t.Run(fmt.Sprintf("context_%t/%s/%s", contextual, path, kind), func(t *testing.T) {
					base := NewMemoryRepository()
					in := sampleInput()
					if path == "refresh" {
						if _, err := NewService(base).Ingest(in); err != nil {
							t.Fatal(err)
						}
						in.EvidenceJSON = `{"new":true}`
					}
					before, events := ingestAtomicSnapshot(base)
					repo := &invalidIntakeResultRepository{MemoryRepository: base}
					repo.write = func(op *models.Operation) (*models.Operation, error) {
						bad, _ := corruptIntakeResult(*op, kind)
						// A repository may also mutate the input pointer: it must not
						// change the service's expected identity used for validation.
						if bad != nil {
							*op = *bad
						}
						return bad, nil
					}
					svc := NewService(repo)
					var got IngestResult
					var err error
					if contextual {
						got, err = svc.IngestContext(t.Context(), in)
					} else {
						got, err = svc.Ingest(in)
					}
					if !errors.Is(err, ErrInvalidRepositoryResult) || !reflect.DeepEqual(got, IngestResult{}) {
						t.Fatalf("invalid successful write was accepted: result=%+v err=%v", got, err)
					}
					if repo.creates+repo.updates != 1 {
						t.Fatalf("write automatically retried: %+v", repo)
					}
					// This injected adapter writes nothing. A real adapter error
					// after commit is uncertainty, never proof of rollback.
					ingestAtomicAssertSnapshot(t, base, before, events)
				})
			}
		}
	}
}

func TestIdentityInspectionRejectsContradictoryMissAndInactiveRecords(t *testing.T) {
	for _, kind := range []string{"contradictory_miss", "archived", "dismissed"} {
		t.Run(kind, func(t *testing.T) {
			base := NewMemoryRepository()
			in := sampleInput()
			op, _ := ingestAtomicPair(t)
			op.OwnerUserID, op.WorkspaceID, op.DedupeKey = in.OwnerUserID, in.WorkspaceID, in.DedupeKey
			repo := &invalidIntakeResultRepository{MemoryRepository: base}
			repo.lookup = func(string, string, string) (*models.Operation, bool, error) {
				bad, found := corruptIntakeResult(op, kind)
				return bad, found, nil
			}
			got, err := NewService(repo).InspectSourceIdentity(t.Context(), in)
			if !errors.Is(err, ErrInvalidRepositoryResult) || got.Canonical != nil || got.Historical != nil || repo.creates+repo.updates != 0 {
				t.Fatalf("inspection accepted invalid result: %+v / %v", got, err)
			}
		})
	}
}
