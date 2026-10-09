package operations

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAtomicMutationSaveCannotBypassStateMachine(t *testing.T) {
	for _, status := range []OperationStatus{StatusRunning, StatusVerifying, StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			repo := NewMemoryRepository()
			before := mutationSeed(t, repo)
			forged := before
			forged.Status = string(status)
			forged.VerificationStatus = string(VerificationNotRequired)
			if saved, err := NewService(repo).Save(forged, "metadata", "hai", "must not change status"); saved != nil || !errors.Is(err, ErrInvalidMutationEvent) {
				t.Fatalf("save changed status without a transition: %+v, %v", saved, err)
			}
			assertMutationUnchanged(t, repo, before, 0)
		})
	}
}

func TestClaimedMutationRejectsForgedBeforeStatusAndRetainsLease(t *testing.T) {
	repo := NewMemoryRepository()
	before := mutationSeed(t, repo)
	service := NewService(repo)
	claimed, err := service.ClaimOperation(context.Background(), before.OwnerUserID, before.WorkspaceID, before.ID, uuid.New(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v, %v", claimed, err)
	}
	forged := claimed.Operation
	forged.Status = string(StatusVerifying)
	forged.VerificationStatus = string(VerificationNotRequired)
	if saved, err := service.TransitionClaimed(context.Background(), claimed.Claim, forged, StatusCompleted, "hai", "", "forged before-status"); saved != nil || !errors.Is(err, ErrStaleOperation) {
		t.Fatalf("claimed transition trusted caller status: %+v, %v", saved, err)
	}
	assertMutationUnchanged(t, repo, before, 0)
	if err := service.RenewClaim(context.Background(), claimed.Claim, time.Minute); err != nil {
		t.Fatalf("refusal changed lease authority: %v", err)
	}
}

func TestClaimedMutationDuplicateAuditLeavesStateAndLeaseIntact(t *testing.T) {
	repo := NewMemoryRepository()
	before := mutationSeed(t, repo)
	claimed, err := repo.ClaimOperation(context.Background(), before.OwnerUserID, before.WorkspaceID, before.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	updated, event, err := ApplyTransition(before, StatusRunning, "hai", "", "duplicate audit", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	event.ID = uuid.New()
	if err := repo.AppendEvent(&event); err != nil {
		t.Fatal(err)
	}
	if saved, err := repo.TransitionClaimed(context.Background(), claimed.Claim, updated, event, true); saved != nil || err == nil {
		t.Fatalf("duplicate audit committed state/release: %+v, %v", saved, err)
	}
	assertMutationUnchanged(t, repo, before, 1)
	if err := repo.RenewClaim(context.Background(), claimed.Claim, time.Minute); err != nil {
		t.Fatalf("audit refusal changed lease authority: %v", err)
	}
}

func TestAtomicMutationCreationTimeRemainsImmutable(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "claimed transition"}[claimed], func(t *testing.T) {
			repo := NewMemoryRepository()
			before := mutationSeed(t, repo)
			service := NewService(repo)
			op := before
			op.CreatedAt = before.CreatedAt.Add(-365 * 24 * time.Hour)
			var saved *models.Operation
			var err error
			if claimed {
				lease, claimErr := service.ClaimOperation(context.Background(), before.OwnerUserID, before.WorkspaceID, before.ID, uuid.New(), time.Minute)
				if claimErr != nil {
					t.Fatal(claimErr)
				}
				saved, err = service.TransitionClaimed(context.Background(), lease.Claim, op, StatusRunning, "hai", "", "preserve chronology")
			} else {
				saved, err = service.Save(op, "metadata", "hai", "preserve chronology")
			}
			if err != nil || saved == nil || !saved.CreatedAt.Equal(before.CreatedAt) {
				t.Fatalf("creation time changed: %+v, %v", saved, err)
			}
			stored, err := service.Get(before.OwnerUserID, before.WorkspaceID, before.ID)
			if err != nil || !reflect.DeepEqual(stored, saved) {
				t.Fatalf("returned chronology differs from stored state: %+v, %v", stored, err)
			}
		})
	}
}

func TestAtomicMutationGormUsesLockedStatusBeforeWrites(t *testing.T) {
	for _, kind := range []string{"forged before-status", "save status change", "illegal transition", "unverified completion"} {
		t.Run(kind, func(t *testing.T) {
			op := mutationSeed(t, NewMemoryRepository())
			state := &mutationSQL{op: op, casRows: 1}
			op.Version++
			event := models.OperationEvent{OperationID: op.ID, EventType: "status_change", BeforeStatus: state.op.Status, CreatedAt: time.Now().UTC()}
			switch kind {
			case "forged before-status":
				event.BeforeStatus = string(StatusVerifying)
				op.Status = string(StatusCompleted)
				op.VerificationStatus = string(VerificationNotRequired)
			case "save status change":
				event.EventType, event.BeforeStatus = "metadata", ""
				op.Status = string(StatusRunning)
			case "illegal transition":
				op.Status = string(StatusCompleted)
				op.VerificationStatus = string(VerificationNotRequired)
			case "unverified completion":
				state.op.Status = string(StatusVerifying)
				event.BeforeStatus = state.op.Status
				op.Status = string(StatusCompleted)
				op.VerificationStatus = string(VerificationPending)
			}
			event.AfterStatus = op.Status
			sqlDB := sql.OpenDB(mutationConnector{state: state})
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if saved, err := NewGormRepository(db).UpdateWithEvent(&op, &event); saved != nil || err == nil || state.writes != 0 || state.events != 0 || state.commits != 0 || state.rollbacks != 1 {
				t.Fatalf("locked status guard bypassed: %+v, %v, %+v", saved, err, state)
			}
		})
	}
}

func TestAtomicMutationGormReturnsStoredCreationTime(t *testing.T) {
	op := mutationSeed(t, NewMemoryRepository())
	state := &mutationSQL{op: op, casRows: 1}
	original := op.CreatedAt
	op.CreatedAt = original.Add(-365 * 24 * time.Hour)
	op.Version++
	input := op
	event := models.OperationEvent{OperationID: op.ID, EventType: "metadata", AfterStatus: op.Status, CreatedAt: time.Now().UTC()}
	sqlDB := sql.OpenDB(mutationConnector{state: state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := NewGormRepository(db).UpdateWithEvent(&op, &event)
	if err != nil || saved == nil || !saved.CreatedAt.Equal(original) || !reflect.DeepEqual(op, input) {
		t.Fatalf("response chronology or caller input changed: %+v, %v, input=%+v", saved, err, op)
	}
}
