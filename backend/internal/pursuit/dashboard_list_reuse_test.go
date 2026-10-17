package pursuit

import (
	"testing"

	"automation-hub-backend/internal/models"
)

type countingPursuitListRepository struct {
	Repository
	listCalls int
}

func (r *countingPursuitListRepository) FindAll(includeArchived bool) ([]models.Pursuit, error) {
	r.listCalls++
	return r.Repository.FindAll(includeArchived)
}

func (r *countingPursuitListRepository) FindAllForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error) {
	r.listCalls++
	return r.Repository.FindAll(includeArchived)
}

func TestDashboardWithPursuitsForOwnerLoadsPursuitsOnce(t *testing.T) {
	repo := &countingPursuitListRepository{Repository: newFakeRepo()}
	service := NewService(repo, nil)
	provider, ok := service.(interface {
		DashboardWithPursuitsForOwner(string) (*Dashboard, error)
	})
	if !ok {
		t.Fatal("service does not expose the expanded dashboard projection")
	}

	dashboard, err := provider.DashboardWithPursuitsForOwner("dashboard-owner")
	if err != nil {
		t.Fatal(err)
	}
	if repo.listCalls != 1 {
		t.Fatalf("pursuit list calls = %d, want 1", repo.listCalls)
	}
	if dashboard.Pursuits == nil || len(dashboard.Pursuits) != 0 {
		t.Fatalf("empty expanded pursuit list = %#v, want a non-nil empty list", dashboard.Pursuits)
	}
}
