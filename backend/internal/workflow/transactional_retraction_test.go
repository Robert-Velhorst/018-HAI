package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type transactionalWorkflowProjectionFunc func(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error)

func (function transactionalWorkflowProjectionFunc) ProjectOperationalRecord(
	ctx context.Context,
	request lifeontology.OperationalProjectionRequest,
) (lifeontology.OperationalProjectionResult, error) {
	return function(ctx, request)
}

func TestRetractSourceInTransactionRejectsNonPostgresRepository(t *testing.T) {
	engine := NewService(newFakeWorkflowRepo())
	transactional, ok := engine.(TransactionalSourceRetraction)
	if !ok {
		t.Fatalf("workflow service %T does not expose TransactionalSourceRetraction", engine)
	}
	projection, err := transactional.RetractSourceInTransaction(nil, "owner-1", "actor-1", "email", "message-1", "removed")
	if !errors.Is(err, ErrWorkflowPostgresTransactionRequired) {
		t.Fatalf("error = %v, want ErrWorkflowPostgresTransactionRequired", err)
	}
	if projection != nil {
		t.Fatal("rejected request returned a post-commit projection")
	}
}

func TestPostgresRetractSourceCommitsWritesAndDefersProjection(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-retraction-" + uuid.NewString()
	actor := "operator-" + uuid.NewString()
	sourceID := "email-" + uuid.NewString()
	item := createWorkflowForRetraction(t, db, owner, sourceID, StateReady)
	cleanupWorkflowAfterTest(t, db, item.ID)

	engine := NewService(NewGormRepository(db))
	implementation := engine.(*service)
	var projectionCalls atomic.Int32
	if _, err := WithLifeOntologyProjection(engine, transactionalWorkflowProjectionFunc(func(
		_ context.Context,
		_ lifeontology.OperationalProjectionRequest,
	) (lifeontology.OperationalProjectionResult, error) {
		projectionCalls.Add(1)
		return lifeontology.OperationalProjectionResult{AdvisoryOnly: true}, nil
	})); err != nil {
		t.Fatal(err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	projection, err := implementation.RetractSourceInTransaction(tx, owner, actor, "email", sourceID, "operator removed the source")
	if err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("RetractSourceInTransaction: %v", err)
	}
	if projection == nil || projectionCalls.Load() != 0 {
		t.Fatalf("projection=%v calls=%d; want deferred projection", projection != nil, projectionCalls.Load())
	}
	unchanged, err := NewGormRepository(db).FindItem(item.ID)
	if err != nil || unchanged.CurrentState != StateReady {
		t.Fatalf("uncommitted workflow state=%#v err=%v", unchanged, err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	updated, err := NewGormRepository(db).FindItem(item.ID)
	if err != nil || updated.CurrentState != StateBlocked || updated.VerificationStatus != "needs_review" ||
		updated.BlockedReason != "operator removed the source" || updated.NextRunAt != nil ||
		updated.WorkerClaimID != "" || updated.WorkerLeaseUntil != nil || !updated.RequiresApproval ||
		updated.ApprovalStatus != "pending" || !strings.HasPrefix(updated.ApprovalReason, sourceRetractionQuarantinePrefix) {
		t.Fatalf("committed workflow=%#v err=%v", updated, err)
	}
	transitions, err := NewGormRepository(db).FindTransitions(item.ID)
	if err != nil || len(transitions) != 1 || transitions[0].Trigger != "source_retraction" || transitions[0].Actor != actor {
		t.Fatalf("transitions=%#v err=%v", transitions, err)
	}
	decisions, err := NewGormRepository(db).FindDecisions(item.ID)
	if err != nil || len(decisions) != 1 || decisions[0].DecisionType != "source_retraction" || decisions[0].Actor != actor {
		t.Fatalf("decisions=%#v err=%v", decisions, err)
	}
	events, err := NewGormRepository(db).FindEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "workflow.source_retracted" || events[0].Actor != actor {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	if err := projection(context.Background()); err != nil || projectionCalls.Load() != 1 {
		t.Fatalf("post-commit projection calls=%d err=%v", projectionCalls.Load(), err)
	}
	if err := projection(context.Background()); err != nil || projectionCalls.Load() != 1 {
		t.Fatalf("repeated projection calls=%d err=%v; expected one-shot closure", projectionCalls.Load(), err)
	}
	events, err = NewGormRepository(db).FindEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("advisory projection wrote workflow audit events: %#v err=%v", events, err)
	}
}

func TestPostgresRetractSourcePropagatesEveryDurableWriteFailure(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	cases := []struct {
		name  string
		table string
		check func(uuid.UUID) string
	}{
		{name: "item update", table: "workflow_items", check: func(id uuid.UUID) string {
			return "id <> '" + id.String() + "'::uuid OR current_state <> 'blocked'"
		}},
		{name: "transition", table: "workflow_transitions", check: func(id uuid.UUID) string {
			return "workflow_id <> '" + id.String() + "'::uuid OR trigger <> 'source_retraction'"
		}},
		{name: "decision", table: "workflow_decisions", check: func(id uuid.UUID) string {
			return "workflow_id <> '" + id.String() + "'::uuid OR decision_type <> 'source_retraction'"
		}},
		{name: "event", table: "workflow_events", check: func(id uuid.UUID) string {
			return "workflow_id <> '" + id.String() + "'::uuid OR event_type <> 'workflow.source_retracted'"
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			owner := "workflow-retraction-failure-" + uuid.NewString()
			sourceID := "email-" + uuid.NewString()
			item := createWorkflowForRetraction(t, db, owner, sourceID, StateReady)
			cleanupWorkflowAfterTest(t, db, item.ID)
			engine := NewService(NewGormRepository(db)).(*service)
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatalf("begin transaction: %v", tx.Error)
			}
			constraint := "chk_source_retract_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			statement := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", testCase.table, constraint, testCase.check(item.ID))
			if err := tx.Exec(statement).Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("install write failure: %v", err)
			}
			projection, err := engine.RetractSourceInTransaction(tx, owner, owner, "email", sourceID, "operator removed the source")
			if err == nil || projection != nil {
				_ = tx.Rollback().Error
				t.Fatalf("projection=%v err=%v; expected durable write failure", projection != nil, err)
			}
			if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
				t.Fatalf("rollback failed transaction: %v", rollbackErr)
			}
			persisted, err := NewGormRepository(db).FindItem(item.ID)
			if err != nil || persisted.CurrentState != StateReady {
				t.Fatalf("workflow after rollback=%#v err=%v", persisted, err)
			}
			transitions, transitionErr := NewGormRepository(db).FindTransitions(item.ID)
			decisions, decisionErr := NewGormRepository(db).FindDecisions(item.ID)
			events, eventErr := NewGormRepository(db).FindEvents(item.ID)
			if transitionErr != nil || decisionErr != nil || eventErr != nil ||
				len(transitions) != 0 || len(decisions) != 0 || len(events) != 0 {
				t.Fatalf("partial writes after rollback: transitions=%d decisions=%d events=%d errors=(%v,%v,%v)",
					len(transitions), len(decisions), len(events), transitionErr, decisionErr, eventErr)
			}
		})
	}
}

func TestPostgresRetractSourcePreservesInProgressAndCompletedStates(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	for _, state := range []string{StateInProgress, StateCompleted, StateArchived} {
		t.Run(state, func(t *testing.T) {
			owner := "workflow-retraction-state-" + uuid.NewString()
			sourceID := state + "-" + uuid.NewString()
			item := createWorkflowForRetraction(t, db, owner, sourceID, state)
			cleanupWorkflowAfterTest(t, db, item.ID)
			engine := NewService(NewGormRepository(db)).(*service)
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatalf("begin transaction: %v", tx.Error)
			}
			projection, err := engine.RetractSourceInTransaction(tx, owner, owner, "email", sourceID, "operator removed the source")
			if state == StateInProgress {
				if err == nil || projection != nil {
					_ = tx.Rollback().Error
					t.Fatalf("projection=%v err=%v; in-progress work must require interruption review", projection != nil, err)
				}
				_ = tx.Rollback().Error
				persisted, readErr := NewGormRepository(db).FindItem(item.ID)
				if readErr != nil || persisted.CurrentState != StateInProgress {
					t.Fatalf("in-progress workflow changed: %#v err=%v", persisted, readErr)
				}
				return
			}
			if err != nil || projection != nil {
				_ = tx.Rollback().Error
				t.Fatalf("projection=%v err=%v; completed workflow should remain unchanged", projection != nil, err)
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatalf("commit audit-only transaction: %v", err)
			}
			persisted, readErr := NewGormRepository(db).FindItem(item.ID)
			if readErr != nil || persisted.CurrentState != state {
				t.Fatalf("completed workflow state=%#v err=%v", persisted, readErr)
			}
			events, eventErr := NewGormRepository(db).FindEvents(item.ID)
			if eventErr != nil || len(events) != 1 || events[0].EventType != "workflow.source_retracted_after_completion" {
				t.Fatalf("completed-workflow audit=%#v err=%v", events, eventErr)
			}
			transitions, transitionErr := NewGormRepository(db).FindTransitions(item.ID)
			decisions, decisionErr := NewGormRepository(db).FindDecisions(item.ID)
			if transitionErr != nil || decisionErr != nil || len(transitions) != 0 || len(decisions) != 0 {
				t.Fatalf("completed workflow acquired state-change audit: transitions=%d decisions=%d errors=(%v,%v)", len(transitions), len(decisions), transitionErr, decisionErr)
			}
		})
	}
}

func TestPostgresRetractSourcePreservesUnreconciledWorkerFence(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-retraction-recovery-" + uuid.NewString()
	sourceID := "email-" + uuid.NewString()
	item := createWorkflowForRetraction(t, db, owner, sourceID, StateBlocked)
	cleanupWorkflowAfterTest(t, db, item.ID)
	claimID := uuid.NewString()
	leaseUntil := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	item.WorkerClaimID = claimID
	item.WorkerLeaseUntil = &leaseUntil
	item.RecoveryStatus = RecoveryNeedsReview
	if _, err := NewGormRepository(db).UpdateItem(item); err != nil {
		t.Fatalf("persist recovered worker claim: %v", err)
	}

	engine := NewService(NewGormRepository(db)).(*service)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	projection, err := engine.RetractSourceInTransaction(tx, owner, owner, "email", sourceID, "operator removed the source")
	if err != nil || projection != nil {
		_ = tx.Rollback().Error
		t.Fatalf("projection=%v err=%v; retraction should quarantine while retaining the unresolved worker fence", projection != nil, err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit retraction: %v", err)
	}

	stored, err := NewGormRepository(db).FindItem(item.ID)
	if err != nil || stored.CurrentState != StateBlocked || stored.RecoveryStatus != RecoveryNeedsReview ||
		stored.WorkerClaimID != claimID || stored.WorkerLeaseUntil == nil || !stored.WorkerLeaseUntil.Equal(leaseUntil) ||
		!strings.HasPrefix(stored.ApprovalReason, sourceRetractionQuarantinePrefix) {
		t.Fatalf("retracted workflow=%#v err=%v; unresolved runner claim must remain fenced", stored, err)
	}
	events, err := NewGormRepository(db).FindEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "workflow.source_retracted" {
		t.Fatalf("retraction events=%#v err=%v", events, err)
	}
}

func TestPostgresRetractSourceIsScopedToExactOwner(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	ownerA := "workflow-retraction-owner-a-" + uuid.NewString()
	ownerB := "workflow-retraction-owner-b-" + uuid.NewString()
	actorA := "operator-a-" + uuid.NewString()
	sourceID := "shared-source-" + uuid.NewString()
	itemA := createWorkflowForRetraction(t, db, ownerA, sourceID, StateReady)
	itemB := createWorkflowForRetraction(t, db, ownerB, sourceID, StateReady)
	cleanupWorkflowAfterTest(t, db, itemA.ID)
	cleanupWorkflowAfterTest(t, db, itemB.ID)

	engine := NewService(NewGormRepository(db)).(*service)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	projection, err := engine.RetractSourceInTransaction(tx, ownerA, actorA, "email", sourceID, "owner A removed the source")
	if err != nil || projection != nil {
		_ = tx.Rollback().Error
		t.Fatalf("owner-scoped retraction projection=%v err=%v", projection != nil, err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit owner-scoped retraction: %v", err)
	}

	updatedA, errA := NewGormRepository(db).FindItem(itemA.ID)
	updatedB, errB := NewGormRepository(db).FindItem(itemB.ID)
	if errA != nil || updatedA.CurrentState != StateBlocked {
		t.Fatalf("owner A workflow=%#v err=%v, want blocked", updatedA, errA)
	}
	if errB != nil || updatedB.CurrentState != StateReady {
		t.Fatalf("owner B workflow=%#v err=%v, want unchanged", updatedB, errB)
	}
	eventsA, err := NewGormRepository(db).FindEvents(itemA.ID)
	if err != nil || len(eventsA) != 1 || eventsA[0].Actor != actorA {
		t.Fatalf("owner A audit events=%#v err=%v, want authenticated actor", eventsA, err)
	}
	eventsB, err := NewGormRepository(db).FindEvents(itemB.ID)
	if err != nil || len(eventsB) != 0 {
		t.Fatalf("owner B audit events=%#v err=%v, want no cross-owner write", eventsB, err)
	}
}

func workflowTransactionalPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open dedicated workflow PostgreSQL test database failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = pool.Close() })
	// Each test must work on a fresh database, independently of execution order.
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("canonical workflow test migrations: %v", err)
	}
	return db
}

func createWorkflowForRetraction(t *testing.T, db *gorm.DB, owner, sourceID, state string) *models.WorkflowItem {
	t.Helper()
	item, err := NewGormRepository(db).CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Transactional source retraction",
		CurrentState: state, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		SourceType: "email", SourceID: sourceID, SourceURI: "local://email/" + sourceID,
	})
	if err != nil {
		t.Fatalf("create workflow fixture: %v", err)
	}
	return item
}

func cleanupWorkflowAfterTest(t *testing.T, db *gorm.DB, workflowID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		for _, model := range []interface{}{
			&models.WorkflowEvent{}, &models.WorkflowDecision{}, &models.WorkflowTransition{},
		} {
			if err := db.Where("workflow_id = ?", workflowID).Delete(model).Error; err != nil {
				t.Errorf("clean up workflow test data: %v", err)
			}
		}
		if err := db.Where("id = ?", workflowID).Delete(&models.WorkflowItem{}).Error; err != nil {
			t.Errorf("clean up workflow test item: %v", err)
		}
	})
}
