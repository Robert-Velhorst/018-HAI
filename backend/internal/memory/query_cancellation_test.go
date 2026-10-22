package memory

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/models"
)

type cancellationLegacyRepository struct {
	*fakeRepository
	reads     int
	afterRead func()
}

func (r *cancellationLegacyRepository) FindAll(project string, archived bool) ([]models.ContextMemory, error) {
	r.reads++
	if r.afterRead != nil {
		r.afterRead()
	}
	return []models.ContextMemory{{OwnerIdentity: "owner", Content: "owned memory"}}, nil
}

type cancellationPagedRepository struct {
	*fakeRepository
	reads     int
	afterRead func()
}

func (r *cancellationPagedRepository) QueryForOwner(ctx context.Context, owner, project string, archived bool, params QueryParams) (PageResult, error) {
	r.reads++
	if ctx == nil {
		return PageResult{}, errors.New("missing context")
	}
	if r.afterRead != nil {
		r.afterRead()
	}
	return PageResult{Items: []models.ContextMemory{{OwnerIdentity: owner}}, Total: 1}, nil
}

func TestOwnerQueryCancellationBoundary(t *testing.T) {
	for _, paged := range []bool{false, true} {
		for _, duringRead := range []bool{false, true} {
			name := "legacy"
			if paged {
				name = "paged"
			}
			if duringRead {
				name += "/during_read"
			} else {
				name += "/before_read"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				legacy := &cancellationLegacyRepository{fakeRepository: newFakeRepository()}
				query := &cancellationPagedRepository{fakeRepository: newFakeRepository()}
				var repo Repository = legacy
				if paged {
					repo = query
				}
				if duringRead {
					legacy.afterRead, query.afterRead = cancel, cancel
				} else {
					cancel()
				}
				result, err := NewService(repo).(OwnerQueryService).QueryForOwner(ctx, "owner", "", false, QueryParams{})
				if !errors.Is(err, context.Canceled) || len(result.Items) != 0 || result.Total != 0 {
					t.Fatalf("cancelled query returned data or success: result=%+v err=%v", result, err)
				}
				if !duringRead && legacy.reads+query.reads != 0 {
					t.Fatal("cancelled query reached repository")
				}
			})
		}
	}
}

func TestOwnerQueryAcceptsNilContext(t *testing.T) {
	for _, repo := range []Repository{
		&cancellationLegacyRepository{fakeRepository: newFakeRepository()},
		&cancellationPagedRepository{fakeRepository: newFakeRepository()},
	} {
		result, err := NewService(repo).(OwnerQueryService).QueryForOwner(nil, "owner", "", false, QueryParams{})
		if err != nil || result.Total != 1 {
			t.Fatalf("live query failed: result=%+v err=%v", result, err)
		}
	}
}
