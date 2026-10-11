package operations

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type sourceObservationOriginFixture struct {
	repo        *MemoryRepository
	op          models.Operation
	event       models.OperationEvent
	observation SourceObservation
	otherFeed   SourceObservation
}

func newSourceObservationOriginFixture(t *testing.T) sourceObservationOriginFixture {
	t.Helper()
	repo := NewMemoryRepository()
	start := sourceObservationTestStart()
	observed, err := repo.BeginSourceObservation(context.Background(), start)
	if err != nil || observed.ID == uuid.Nil || observed.Generation != 1 {
		t.Fatalf("mint actual source observation: %+v / %v", observed, err)
	}
	otherStart := start
	otherStart.OriginID = uuid.New()
	otherFeed, err := repo.BeginSourceObservation(context.Background(), otherStart)
	if err != nil || otherFeed.ID == uuid.Nil || otherFeed.ID == observed.ID || otherFeed.Generation != 2 || otherFeed.OriginID == observed.OriginID {
		t.Fatalf("mint actual other-feed observation: %+v / %v", otherFeed, err)
	}
	op := sourceIdentityContractOperation(t, sourceObservationTestInput())
	id := observed.ID
	op.SourceObservationID, op.SourceObservationGeneration = &id, observed.Generation
	event := models.OperationEvent{
		OperationID: op.ID, EventType: "created", ActorType: string(OwnerHAI),
		AfterStatus: op.Status, PayloadJSON: "{}", CreatedAt: op.CreatedAt,
	}
	if err := Validate(op); err != nil {
		t.Fatalf("otherwise-valid observed operation fixture: %v", err)
	}
	if err := validateCreationEvent(&op, &event); err != nil {
		t.Fatalf("otherwise-valid created event fixture: %v", err)
	}
	fixture := sourceObservationOriginFixture{repo: repo, op: op, event: event, observation: observed, otherFeed: otherFeed}
	assertSourceObservationOriginTicketsUnchanged(t, fixture)
	return fixture
}

func assertSourceObservationOriginTicketsUnchanged(t *testing.T, f sourceObservationOriginFixture) {
	t.Helper()
	wantObservations := map[uuid.UUID]SourceObservation{
		f.observation.ID: f.observation,
		f.otherFeed.ID:   f.otherFeed,
	}
	wantClocks := map[sourceObservationScope]int64{
		{owner: f.observation.OwnerUserID, workspace: f.observation.WorkspaceID}: 2,
	}
	if !reflect.DeepEqual(f.repo.observations, wantObservations) || !reflect.DeepEqual(f.repo.observationClocks, wantClocks) {
		t.Fatal("operation creation changed observation UUIDs, generations, provenance, or the tenant clock")
	}
}

func sourceObservationOriginOperation(f sourceObservationOriginFixture, kind string) models.Operation {
	op := f.op
	switch kind {
	case "missing":
		op.AccountFeedID = nil
	case "zero":
		id := uuid.Nil
		op.AccountFeedID = &id
	case "wrong":
		id := uuid.New()
		op.AccountFeedID = &id
	case "other_feed":
		id := f.otherFeed.OriginID
		op.AccountFeedID = &id
	}
	return op
}

func TestSourceObservationValidateRequiresNonzeroFeedOrigin(t *testing.T) {
	for _, kind := range []string{"missing", "zero", "exact"} {
		t.Run(kind, func(t *testing.T) {
			f := newSourceObservationOriginFixture(t)
			op := sourceObservationOriginOperation(f, kind)
			err := Validate(op)
			if kind == "exact" {
				if err != nil {
					t.Fatalf("healthy exact-origin operation rejected: %v", err)
				}
			} else if !errors.Is(err, ErrInvalidSourceObservation) {
				t.Fatalf("invalid %s origin accepted/wrong error: %v", kind, err)
			}
			assertSourceObservationMetadata(t, op, f.observation)
			assertSourceObservationOriginTicketsUnchanged(t, f)
			if len(f.repo.ops) != 0 || len(f.repo.events) != 0 || len(f.repo.claims) != 0 {
				t.Fatal("domain validation wrote an operation, audit, or claim")
			}
		})
	}
}

func TestSourceObservationMemoryCreationRequiresExactFeedOrigin(t *testing.T) {
	for _, path := range []string{"create", "atomic_create", "context_create"} {
		for _, kind := range []string{"missing", "zero", "wrong", "other_feed", "exact"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				f := newSourceObservationOriginFixture(t)
				op := sourceObservationOriginOperation(f, kind)
				event, beforeEvent := f.event, f.event
				before := sourceIdentitySnapshot(f.repo)
				var saved *models.Operation
				var err error
				switch path {
				case "create":
					saved, err = f.repo.Create(&op)
				case "atomic_create":
					saved, err = f.repo.CreateWithEvent(&op, &event)
				case "context_create":
					saved, err = f.repo.CreateWithEventContext(context.Background(), &op, &event)
				}
				if kind != "exact" {
					if !errors.Is(err, ErrInvalidSourceObservation) || saved != nil {
						t.Fatalf("invalid origin created operation/audit: saved=%+v error=%v", saved, err)
					}
					sourceIdentityAssertUnchanged(t, f.repo, before)
				} else {
					if err != nil || saved == nil || saved.ID != op.ID || saved.AccountFeedID == nil || *saved.AccountFeedID != f.observation.OriginID || len(f.repo.ops) != 1 || len(f.repo.claims) != 0 {
						t.Fatalf("healthy exact-origin creation rejected: saved=%+v error=%v", saved, err)
					}
					assertSourceObservationMetadata(t, *saved, f.observation)
					stored, getErr := f.repo.GetByID(op.OwnerUserID, op.WorkspaceID, op.ID)
					if getErr != nil || stored == nil || stored.AccountFeedID == nil || *stored.AccountFeedID != f.observation.OriginID {
						t.Fatalf("exact-origin creation not stored: %+v / %v", stored, getErr)
					}
					assertSourceObservationMetadata(t, *stored, f.observation)
					wantEvents := 1
					if path == "create" {
						wantEvents = 0
					}
					if len(f.repo.events) != wantEvents {
						t.Fatalf("healthy creation audit count=%d want=%d", len(f.repo.events), wantEvents)
					}
					if wantEvents == 1 {
						audit := f.repo.events[0]
						if audit.ID == uuid.Nil || audit.OperationID != op.ID || audit.EventType != "created" || audit.ActorType != string(OwnerHAI) || audit.AfterStatus != op.Status {
							t.Fatalf("healthy creation lost its valid audit: %+v", audit)
						}
					}
				}
				if !reflect.DeepEqual(event, beforeEvent) {
					t.Fatal("creation mutated the caller's audit event")
				}
				assertSourceObservationMetadata(t, op, f.observation)
				assertSourceObservationOriginTicketsUnchanged(t, f)
			})
		}
	}
}

func TestSourceObservationGormCreationRefusesMissingOrZeroOriginBeforeSQL(t *testing.T) {
	for _, path := range []string{"create", "atomic_create", "context_create"} {
		for _, kind := range []string{"missing", "zero"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				f := newSourceObservationOriginFixture(t)
				op := sourceObservationOriginOperation(f, kind)
				event := f.event
				state := &creationSQL{operationID: op.ID}
				repo := creationTransactionRepository(t, state)
				var saved *models.Operation
				var err error
				switch path {
				case "create":
					saved, err = repo.Create(&op)
				case "atomic_create":
					saved, err = repo.CreateWithEvent(&op, &event)
				case "context_create":
					saved, err = repo.CreateWithEventContext(context.Background(), &op, &event)
				}
				if !errors.Is(err, ErrInvalidSourceObservation) || saved != nil || state.active || state.begins != 0 || state.operations != 0 || state.events != 0 || state.commits != 0 || state.rollbacks != 0 {
					t.Fatalf("invalid origin reached SQL or lost domain error: saved=%+v error=%v state=%+v", saved, err, state)
				}
				if !reflect.DeepEqual(event, f.event) {
					t.Fatal("refused Gorm creation mutated audit input")
				}
				assertSourceObservationMetadata(t, op, f.observation)
				assertSourceObservationOriginTicketsUnchanged(t, f)
			})
		}
	}
}
