package workflow

import (
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type supersessionRaceRepository struct {
	Repository
	item              models.WorkflowItem
	expected          SourceWorkflowSupersession
	transitionWrites  int
	decisionWrites    int
	eventWrites       int
	fullRowSaveWrites int
}

func (r *supersessionRaceRepository) SupersedeSourceWorkflowCAS(expected SourceWorkflowSupersession) (bool, error) {
	r.expected = expected
	// The lookup snapshot was ready, but a worker claims the persisted row
	// immediately before the repository applies its conditional update.
	leaseUntil := time.Now().UTC().Add(time.Minute)
	r.item.CurrentState = StateInProgress
	r.item.WorkerClaimID = "worker-won-race"
	r.item.WorkerLeaseUntil = &leaseUntil

	if r.item.ID != expected.ID || r.item.OwnerIdentity != expected.OwnerIdentity ||
		r.item.SourceType != expected.SourceType || r.item.SourceID != expected.SourceID ||
		r.item.SourceRevision != expected.ExpectedSourceRevision ||
		r.item.CurrentState != expected.ExpectedCurrentState || r.item.Archived ||
		r.item.WorkerClaimID != "" {
		return false, nil
	}
	return true, nil
}

func (r *supersessionRaceRepository) UpdateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	r.fullRowSaveWrites++
	r.item = *item
	return item, nil
}

func (r *supersessionRaceRepository) CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error) {
	r.transitionWrites++
	return transition, nil
}

func (r *supersessionRaceRepository) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	r.decisionWrites++
	return decision, nil
}

func (r *supersessionRaceRepository) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	r.eventWrites++
	return event, nil
}

func TestSupersedeSourceWorkflowCASConflictPreservesWorkerClaim(t *testing.T) {
	snapshot := models.WorkflowItem{
		ID:             uuid.New(),
		OwnerIdentity:  "owner-1",
		SourceType:     "trello",
		SourceID:       "card-1",
		SourceRevision: "revision-1",
		CurrentState:   StateReady,
	}
	repo := &supersessionRaceRepository{item: snapshot}
	service := &service{repo: repo}

	err := service.supersedeSourceWorkflow(&snapshot, IntakeRequest{Actor: "test"}, "revision-2")
	if !errors.Is(err, ErrWorkflowSourceSupersessionConflict) {
		t.Fatalf("supersession error = %v, want source-supersession conflict", err)
	}
	if repo.expected != (SourceWorkflowSupersession{
		ID: snapshot.ID, OwnerIdentity: snapshot.OwnerIdentity, SourceType: snapshot.SourceType,
		SourceID: snapshot.SourceID, ExpectedSourceRevision: snapshot.SourceRevision,
		ExpectedCurrentState: StateReady,
	}) {
		t.Fatalf("CAS expectation = %#v, want exact looked-up source revision and state", repo.expected)
	}
	if repo.item.CurrentState != StateInProgress || repo.item.WorkerClaimID != "worker-won-race" || repo.item.WorkerLeaseUntil == nil {
		t.Fatalf("worker-winning state was overwritten: %#v", repo.item)
	}
	if repo.fullRowSaveWrites != 0 || repo.transitionWrites != 0 || repo.decisionWrites != 0 || repo.eventWrites != 0 {
		t.Fatalf("conflict wrote supersession side effects: full saves=%d transitions=%d decisions=%d events=%d",
			repo.fullRowSaveWrites, repo.transitionWrites, repo.decisionWrites, repo.eventWrites)
	}
	if snapshot.CurrentState != StateReady || snapshot.Archived || snapshot.WorkerClaimID != "" {
		t.Fatalf("stale intake snapshot was mutated despite CAS conflict: %#v", snapshot)
	}
}

func TestSupersedeSourceWorkflowFailsClosedWithoutCASCapability(t *testing.T) {
	var repo Repository = &supersessionRaceRepositoryWithoutCAS{}
	service := &service{repo: repo}
	item := models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "owner-1", SourceType: "trello", SourceID: "card-2",
		SourceRevision: "revision-1", CurrentState: StateReady,
	}

	err := service.supersedeSourceWorkflow(&item, IntakeRequest{}, "revision-2")
	if !errors.Is(err, ErrWorkflowSourceSupersessionConflict) {
		t.Fatalf("supersession error = %v, want fail-closed conflict", err)
	}
}

type supersessionRaceRepositoryWithoutCAS struct {
	Repository
}

type intakeSupersessionRaceRepository struct {
	*fakeWorkflowRepo
	claimWon bool
}

func (r *intakeSupersessionRaceRepository) FindActiveItemBySourceIdentityForOwner(ownerIdentity, sourceType, sourceID string) (*models.WorkflowItem, error) {
	item, err := r.fakeWorkflowRepo.FindActiveItemBySourceIdentityForOwner(ownerIdentity, sourceType, sourceID)
	if err != nil || item == nil {
		return item, err
	}
	snapshot := *item
	return &snapshot, nil
}

func (r *intakeSupersessionRaceRepository) SupersedeSourceWorkflowCAS(expected SourceWorkflowSupersession) (bool, error) {
	item := r.items[expected.ID]
	if item == nil {
		return false, nil
	}
	leaseUntil := time.Now().UTC().Add(time.Minute)
	item.CurrentState = StateInProgress
	item.WorkerClaimID = "worker-won-intake-race"
	item.WorkerLeaseUntil = &leaseUntil
	r.claimWon = true
	return false, nil
}

func TestIntakeDoesNotCreateReplacementAfterSupersessionCASConflict(t *testing.T) {
	baseRepo := newFakeWorkflowRepo()
	repo := &intakeSupersessionRaceRepository{fakeWorkflowRepo: baseRepo}
	service := NewService(repo)

	first, err := service.Intake(IntakeRequest{
		OwnerIdentity: "owner-1", Input: "Follow up: prepare the first source record.",
		SourceType: "email", SourceID: "message-race", SourceURI: "mailto:race@example.test",
	})
	if err != nil {
		t.Fatalf("first intake: %v", err)
	}

	_, err = service.Intake(IntakeRequest{
		OwnerIdentity: "owner-1", Input: "Updated source text: prepare the revised record.",
		SourceType: "email", SourceID: "message-race", SourceURI: "mailto:race@example.test",
	})
	if !errors.Is(err, ErrWorkflowSourceSupersessionConflict) {
		t.Fatalf("revised intake error = %v, want source-supersession conflict", err)
	}
	if !repo.claimWon {
		t.Fatal("test did not interleave a worker claim after lookup and before supersession")
	}
	if len(baseRepo.items) != 1 {
		t.Fatalf("workflow count = %d, want no replacement after CAS conflict", len(baseRepo.items))
	}
	stored := baseRepo.items[first.Item.ID]
	if stored == nil || stored.CurrentState != StateInProgress || stored.WorkerClaimID != "worker-won-intake-race" || stored.WorkerLeaseUntil == nil || stored.Archived {
		t.Fatalf("worker-owned workflow state was not preserved: %#v", stored)
	}
	for _, transition := range baseRepo.transitions[first.Item.ID] {
		if transition.Trigger == "source_revision" {
			t.Fatal("supersession transition was written after CAS conflict")
		}
	}
	for _, event := range baseRepo.events[first.Item.ID] {
		if event.EventType == "workflow.source_superseded" {
			t.Fatal("supersession audit was written after CAS conflict")
		}
	}
	for _, decision := range baseRepo.decisions[first.Item.ID] {
		if decision.DecisionType == "source_revision" {
			t.Fatal("supersession decision was written after CAS conflict")
		}
	}
}

func TestPostgresSupersedeSourceWorkflowCASRequiresUnclaimedExpectedRevision(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := &GormRepository{DB: tx}
	owner := "source-supersession-cas-" + uuid.NewString()

	claimedItem, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Worker race", Description: "Preserve this description",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		SourceType: "trello", SourceID: "card-" + uuid.NewString(), SourceRevision: "revision-1",
		LastWorkerError: "preserve this unrelated field",
	})
	if err != nil {
		t.Fatalf("create claimed workflow: %v", err)
	}
	staleRevisionArchived, err := repo.SupersedeSourceWorkflowCAS(SourceWorkflowSupersession{
		ID: claimedItem.ID, OwnerIdentity: owner, SourceType: claimedItem.SourceType, SourceID: claimedItem.SourceID,
		ExpectedSourceRevision: "stale-revision", ExpectedCurrentState: StateReady,
	})
	if err != nil || staleRevisionArchived {
		t.Fatalf("supersession with stale revision = (%t, %v), want conditional conflict", staleRevisionArchived, err)
	}
	stillReady, err := repo.FindItem(claimedItem.ID)
	if err != nil {
		t.Fatalf("reload workflow after stale-revision conflict: %v", err)
	}
	if stillReady.CurrentState != StateReady || stillReady.Archived {
		t.Fatalf("stale-revision CAS changed the active workflow: %#v", stillReady)
	}
	claimTime := time.Now().UTC()
	leaseUntil := claimTime.Add(time.Minute)
	claimed, won, err := repo.ClaimRunnableItemForOwner(owner, claimedItem.ID, "worker-won-race", claimTime, leaseUntil)
	if err != nil || !won || claimed == nil {
		t.Fatalf("worker claim = (%#v, %t, %v), want worker to claim before supersession", claimed, won, err)
	}
	archived, err := repo.SupersedeSourceWorkflowCAS(SourceWorkflowSupersession{
		ID: claimedItem.ID, OwnerIdentity: owner, SourceType: claimedItem.SourceType, SourceID: claimedItem.SourceID,
		ExpectedSourceRevision: claimedItem.SourceRevision, ExpectedCurrentState: StateReady,
	})
	if err != nil || archived {
		t.Fatalf("supersession against worker claim = (%t, %v), want conditional conflict", archived, err)
	}
	stored, err := repo.FindItem(claimedItem.ID)
	if err != nil {
		t.Fatalf("reload worker-claimed workflow: %v", err)
	}
	if stored.CurrentState != StateInProgress || stored.WorkerClaimID != "worker-won-race" || stored.WorkerLeaseUntil == nil || stored.Archived {
		t.Fatalf("CAS changed worker-owned state: %#v", stored)
	}

	readyItem, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Eligible revision", Description: "Preserve this description",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		SourceType: "trello", SourceID: "card-" + uuid.NewString(), SourceRevision: "revision-4",
		NextAction: "old action", LastWorkerError: "preserve this unrelated field", RetryCount: 1,
	})
	if err != nil {
		t.Fatalf("create supersedable workflow: %v", err)
	}
	archived, err = repo.SupersedeSourceWorkflowCAS(SourceWorkflowSupersession{
		ID: readyItem.ID, OwnerIdentity: owner, SourceType: readyItem.SourceType, SourceID: readyItem.SourceID,
		ExpectedSourceRevision: readyItem.SourceRevision, ExpectedCurrentState: StateReady,
	})
	if err != nil || !archived {
		t.Fatalf("supersession of eligible revision = (%t, %v), want success", archived, err)
	}
	stored, err = repo.FindItem(readyItem.ID)
	if err != nil {
		t.Fatalf("reload superseded workflow: %v", err)
	}
	if !stored.Archived || stored.CurrentState != StateArchived || stored.NextAction != "superseded by revised source content" ||
		stored.NextRunAt != nil || stored.WorkerClaimID != "" || stored.WorkerLeaseUntil != nil {
		t.Fatalf("eligible workflow was not superseded cleanly: %#v", stored)
	}
	if stored.Title != readyItem.Title || stored.Description != readyItem.Description ||
		stored.LastWorkerError != readyItem.LastWorkerError || stored.RetryCount != readyItem.RetryCount {
		t.Fatalf("CAS modified unrelated workflow fields: %#v", stored)
	}
}
