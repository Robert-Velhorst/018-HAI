package workflow

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestPostgresIntakeFinalizationRollsBackWhenAuditWriteFails(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-intake-rollback-" + uuid.NewString()
	item := createWorkflowForRetraction(t, db, owner, uuid.NewString(), StateNewInput)
	cleanupWorkflowAfterTest(t, db, item.ID)

	suffix := uuid.NewString()
	functionName := "hai_test_fail_intake_audit_" + suffix[:8]
	triggerName := functionName
	functionSQL := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger
LANGUAGE plpgsql AS $body$
BEGIN
  IF NEW.workflow_id = '%s'::uuid AND NEW.event_type = 'workflow.intake' THEN
    RAISE EXCEPTION 'forced workflow intake audit failure';
  END IF;
  RETURN NEW;
END
$body$`, functionName, item.ID)
	if err := db.Exec(functionSQL).Error; err != nil {
		t.Fatalf("create intake audit failure function: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(
		"CREATE TRIGGER %s BEFORE INSERT ON workflow_events FOR EACH ROW EXECUTE FUNCTION %s()",
		triggerName,
		functionName,
	)).Error; err != nil {
		_ = db.Exec(fmt.Sprintf("DROP FUNCTION %s()", functionName)).Error
		t.Fatalf("create intake audit failure trigger: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON workflow_events", triggerName)).Error; err != nil {
			t.Errorf("drop intake audit failure trigger: %v", err)
		}
		if err := db.Exec(fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName)).Error; err != nil {
			t.Errorf("drop intake audit failure function: %v", err)
		}
	})

	updated := *item
	updated.CurrentState = StateReady
	finalization := WorkflowIntakeFinalization{
		Expected: item,
		Updated:  &updated,
		Transition: models.WorkflowTransition{
			WorkflowID: item.ID, FromState: StateNewInput, ToState: StateReady,
			Trigger: "manual", Actor: "engine", Reason: "classified during intake",
		},
		Decisions: []models.WorkflowDecision{{
			WorkflowID: item.ID, DecisionType: "classification", Decision: "administrative", Actor: "engine",
		}},
		Events: []models.WorkflowEvent{{
			WorkflowID: item.ID, EventType: "workflow.intake", ToState: StateReady, Actor: "engine",
		}},
	}
	repository := NewGormRepository(db)
	if _, changed, err := repository.CommitWorkflowIntake(finalization); err == nil || changed {
		t.Fatalf("CommitWorkflowIntake = (changed=%t, err=%v), want rollback on injected event failure", changed, err)
	} else {
		var auditErr *WorkflowAuditPersistenceError
		if !errors.As(err, &auditErr) {
			t.Fatalf("CommitWorkflowIntake error = %v, want audit persistence error", err)
		}
	}

	stored, err := repository.FindItem(item.ID)
	if err != nil || stored.CurrentState != StateNewInput {
		t.Fatalf("workflow after failed finalization = %#v, err=%v; want new_input", stored, err)
	}
	for name, load := range map[string]func() (int, error){
		"transitions": func() (int, error) {
			rows, err := repository.FindTransitions(item.ID)
			return len(rows), err
		},
		"decisions": func() (int, error) {
			rows, err := repository.FindDecisions(item.ID)
			return len(rows), err
		},
		"events": func() (int, error) {
			rows, err := repository.FindEvents(item.ID)
			return len(rows), err
		},
	} {
		count, err := load()
		if err != nil || count != 0 {
			t.Fatalf("%s after failed finalization = %d rows, err=%v; want no partial audit rows", name, count, err)
		}
	}
	if candidates, err := repository.FindRunnableItemsForOwner(owner, time.Now().UTC(), 10); err != nil {
		t.Fatalf("find runnable workflows: %v", err)
	} else {
		for _, candidate := range candidates {
			if candidate.ID == item.ID {
				t.Fatal("workflow became visible to a worker after audit transaction rollback")
			}
		}
	}
	if _, claimed, err := repository.ClaimRunnableItemForOwner(owner, item.ID, "intake-rollback-worker", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || claimed {
		t.Fatalf("claim after failed finalization = (claimed=%t, err=%v), want no claim", claimed, err)
	}
}
