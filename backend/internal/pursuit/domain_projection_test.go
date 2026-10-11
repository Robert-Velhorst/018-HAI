package pursuit

import (
	"automation-hub-backend/internal/lifeops"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeLifeDomainLinker struct {
	requests []lifeops.LinkEntityRequest
	err      error
}

func (f *fakeLifeDomainLinker) LinkEntity(request lifeops.LinkEntityRequest) (*lifeops.EntityDomainLink, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	return &lifeops.EntityDomainLink{ID: uuid.New()}, nil
}

func TestCreateProjectsCanonicalLifeDomain(t *testing.T) {
	repo := newFakeRepo()
	linker := &fakeLifeDomainLinker{}
	service := WithLifeDomainLinker(NewService(repo, nil), linker)

	created, err := service.Create(CreateRequest{
		OwnerIdentity: "owner@example.test",
		Title:         "Prepare case evidence",
		Domain:        string(lifeops.DomainLegalGovernment),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(linker.requests) != 1 {
		t.Fatalf("expected one domain projection, got %d", len(linker.requests))
	}
	request := linker.requests[0]
	if request.OwnerIdentity != "owner@example.test" || request.EntityType != "pursuit" || request.EntityID != created.ID.String() || request.DomainID != lifeops.DomainLegalGovernment || !request.Primary {
		t.Fatalf("unexpected domain projection: %#v", request)
	}
}

func TestUpdateProjectsChangedCanonicalLifeDomain(t *testing.T) {
	repo := newFakeRepo()
	linker := &fakeLifeDomainLinker{}
	service := WithLifeDomainLinker(NewService(repo, nil), linker)
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "owner@example.test",
		Title:         "Prepare case evidence",
		Domain:        string(lifeops.DomainLegalGovernment),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updatedDomain := string(lifeops.DomainFinancial)
	if _, err := service.UpdateForOwner("owner@example.test", created.ID, UpdateRequest{Domain: &updatedDomain}); err != nil {
		t.Fatalf("UpdateForOwner: %v", err)
	}
	if len(linker.requests) != 2 || linker.requests[1].DomainID != lifeops.DomainFinancial {
		t.Fatalf("expected changed domain projection, got %#v", linker.requests)
	}
}

func TestProjectionSkipsLegacyOwnerlessAndNonCanonicalPursuits(t *testing.T) {
	repo := newFakeRepo()
	linker := &fakeLifeDomainLinker{}
	service := WithLifeDomainLinker(NewService(repo, nil), linker)
	if _, err := service.Create(CreateRequest{Title: "Ownerless pursuit"}); err != nil {
		t.Fatalf("Create ownerless pursuit: %v", err)
	}
	if len(linker.requests) != 0 {
		t.Fatalf("ownerless pursuits must not create an owner-scoped projection: %#v", linker.requests)
	}
	if _, err := service.Create(CreateRequest{OwnerIdentity: "owner@example.test", Title: "Legacy domain", Domain: "operations"}); err == nil {
		t.Fatal("expected non-canonical domain rejection")
	}
}

func TestProjectionFailureDoesNotLosePursuit(t *testing.T) {
	repo := newFakeRepo()
	linker := &fakeLifeDomainLinker{err: errors.New("life-domain index unavailable")}
	service := WithLifeDomainLinker(NewService(repo, nil), linker)
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "owner@example.test",
		Title:         "Prepare case evidence",
		Domain:        string(lifeops.DomainLegalGovernment),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.FindByID(created.ID); err != nil {
		t.Fatalf("pursuit should remain durable when projection fails: %v", err)
	}
	activity, err := repo.FindActivities(created.ID, 20)
	if err != nil {
		t.Fatalf("FindActivities: %v", err)
	}
	found := false
	for _, item := range activity {
		if item.EventType == "pursuit.life_domain_projection_failed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected projection failure audit entry, got %#v", activity)
	}
}

func TestReconcileLifeDomainsForOwnerRepairsCanonicalPursuits(t *testing.T) {
	repo := newFakeRepo()
	linker := &fakeLifeDomainLinker{}
	pursuitService := WithLifeDomainLinker(NewService(repo, nil), linker)
	if _, err := pursuitService.Create(CreateRequest{OwnerIdentity: "alice", Title: "Legal case", Domain: string(lifeops.DomainLegalGovernment)}); err != nil {
		t.Fatalf("Create canonical pursuit: %v", err)
	}
	if _, err := pursuitService.Create(CreateRequest{OwnerIdentity: "bob", Title: "Financial review", Domain: string(lifeops.DomainFinancial)}); err != nil {
		t.Fatalf("Create foreign pursuit: %v", err)
	}
	if _, err := pursuitService.Create(CreateRequest{Title: "Local legacy pursuit"}); err != nil {
		t.Fatalf("Create ownerless pursuit: %v", err)
	}
	linker.requests = nil

	result, err := pursuitService.(*service).ReconcileLifeDomainsForOwner("alice", "alice")
	if err != nil {
		t.Fatalf("ReconcileLifeDomainsForOwner: %v", err)
	}
	if result.Scanned != 2 || result.Projected != 1 || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("unexpected reconciliation result: %#v", result)
	}
	if len(linker.requests) != 1 || linker.requests[0].OwnerIdentity != "alice" {
		t.Fatalf("reconciliation leaked across owners: %#v", linker.requests)
	}
}
