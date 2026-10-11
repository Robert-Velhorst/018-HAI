package workflow

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type atomicManualTransitionTestRepository struct {
	*fakeWorkflowRepo
	commitErr error
	commits   int
}

func (r *fakeWorkflowRepo) CommitManualWorkflowTransition(
	finalization WorkflowTransitionFinalization,
) (*models.WorkflowItem, bool, error) {
	id := finalization.Expected.ID
	original := *r.items[id]
	transitionsBefore := append([]models.WorkflowTransition(nil), r.transitions[id]...)
	eventsBefore := append([]models.WorkflowEvent(nil), r.events[id]...)
	rollback := func() {
		copy := original
		r.items[id] = &copy
		r.transitions[id] = transitionsBefore
		r.events[id] = eventsBefore
	}

	item, changed, err := r.UpdateWorkflowItemCAS(finalization.Expected, finalization.Updated)
	if err != nil || !changed {
		return item, changed, err
	}
	if _, err := r.CreateTransition(&finalization.Transition); err != nil {
		rollback()
		return nil, false, workflowAuditPersistenceFailure("manual transition record", err)
	}
	if _, err := r.CreateEvent(&finalization.Event); err != nil {
		rollback()
		return nil, false, workflowAuditPersistenceFailure("manual transition event", err)
	}
	return item, true, nil
}

func (r *workflowAuditFailureRepository) CommitManualWorkflowTransition(
	finalization WorkflowTransitionFinalization,
) (*models.WorkflowItem, bool, error) {
	if r.transitionErr != nil {
		return nil, false, workflowAuditPersistenceFailure("manual transition record", r.transitionErr)
	}
	if r.eventErr != nil && (r.eventType == "" || r.eventType == finalization.Event.EventType) {
		return nil, false, workflowAuditPersistenceFailure("manual transition event", r.eventErr)
	}
	return r.fakeWorkflowRepo.CommitManualWorkflowTransition(finalization)
}

func (r *atomicManualTransitionTestRepository) CommitManualWorkflowTransition(
	finalization WorkflowTransitionFinalization,
) (*models.WorkflowItem, bool, error) {
	r.commits++
	if r.commitErr != nil {
		return nil, false, r.commitErr
	}
	return r.fakeWorkflowRepo.CommitManualWorkflowTransition(finalization)
}

func TestManualTransitionUsesAtomicCommitCapability(t *testing.T) {
	base := newFakeWorkflowRepo()
	repository := &atomicManualTransitionTestRepository{fakeWorkflowRepo: base}
	service := NewService(repository)
	created, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create an internal low-risk checklist.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	workflowID := created.Item.ID
	transitionsBefore := len(base.transitions[workflowID])
	eventsBefore := len(base.events[workflowID])
	originalState := base.items[workflowID].CurrentState
	cause := errors.New("audit storage unavailable")
	repository.commitErr = workflowAuditPersistenceFailure("manual transition event", cause)

	_, err = service.Transition(workflowID, TransitionRequest{
		TargetState: StateBlocked,
		Message:     "manual blocker",
	})
	if !errors.Is(err, cause) {
		t.Fatalf("Transition error = %v, want audit persistence failure", err)
	}
	if repository.commits != 1 {
		t.Fatalf("atomic transition commits = %d, want 1", repository.commits)
	}
	if got := base.items[workflowID].CurrentState; got != originalState {
		t.Fatalf("state after failed atomic transition = %q, want unchanged %q", got, originalState)
	}
	if got := len(base.transitions[workflowID]); got != transitionsBefore {
		t.Fatalf("transition records after failure = %d, want unchanged count %d", got, transitionsBefore)
	}
	if got := len(base.events[workflowID]); got != eventsBefore {
		t.Fatalf("events after failure = %d, want unchanged count %d", got, eventsBefore)
	}
}

func TestPostgresManualTransitionAuditEventFailureRollsBackState(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin test transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	workflowID := uuid.New()
	constraintName := "chk_workflow_transition_event_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	constraint := "CHECK (workflow_id <> '" + workflowID.String() + "'::uuid OR event_type <> 'workflow.transition')"
	if err := tx.Exec("ALTER TABLE workflow_events ADD CONSTRAINT " + constraintName + " " + constraint).Error; err != nil {
		t.Fatalf("install transaction-scoped audit failure: %v", err)
	}
	repository := NewGormRepository(tx)
	owner := "workflow-transition-atomicity-" + uuid.NewString()
	item, err := repository.CreateItem(&models.WorkflowItem{
		ID: workflowID, OwnerIdentity: owner, Title: "Manual transition audit atomicity",
		Description:  "A rejected audit write must not leave a partial state change.",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
	})
	if err != nil {
		t.Fatalf("create ready workflow: %v", err)
	}
	service := NewService(repository)
	_, err = service.Transition(item.ID, TransitionRequest{TargetState: StateBlocked, Message: "manual blocker"})
	var auditErr *WorkflowAuditPersistenceError
	if !errors.As(err, &auditErr) || auditErr.Operation != "manual transition event" {
		t.Fatalf("Transition error = %v, want typed manual-transition event persistence failure", err)
	}
	persisted, err := repository.FindItem(item.ID)
	if err != nil || persisted.CurrentState != StateReady {
		t.Fatalf("state after audit failure = %#v, err=%v; want original ready state", persisted, err)
	}
	transitions, err := repository.FindTransitions(item.ID)
	if err != nil || len(transitions) != 0 {
		t.Fatalf("transitions after failed commit = %#v, err=%v; want none", transitions, err)
	}
	events, err := repository.FindEvents(item.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after failed commit = %#v, err=%v; want none", events, err)
	}

	if err := tx.Exec("ALTER TABLE workflow_events DROP CONSTRAINT " + constraintName).Error; err != nil {
		t.Fatalf("remove transaction-scoped audit failure: %v", err)
	}
	if _, err := service.Transition(item.ID, TransitionRequest{TargetState: StateBlocked, Message: "manual blocker"}); err != nil {
		t.Fatalf("Transition after audit recovery: %v", err)
	}
	persisted, err = repository.FindItem(item.ID)
	if err != nil || persisted.CurrentState != StateBlocked {
		t.Fatalf("state after successful transition = %#v, err=%v; want blocked", persisted, err)
	}
	transitions, err = repository.FindTransitions(item.ID)
	if err != nil || len(transitions) != 1 || transitions[0].ToState != StateBlocked {
		t.Fatalf("successful transition records = %#v, err=%v; want one blocked transition", transitions, err)
	}
	events, err = repository.FindEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "workflow.transition" {
		t.Fatalf("successful transition events = %#v, err=%v; want one transition event", events, err)
	}
}
