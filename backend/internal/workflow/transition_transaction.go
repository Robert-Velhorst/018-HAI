package workflow

import (
	"errors"
	"fmt"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WorkflowTransitionFinalization binds a manual workflow state change to the
// durable records that explain it. The repository commits all three together.
type WorkflowTransitionFinalization struct {
	Expected   *models.WorkflowItem
	Updated    *models.WorkflowItem
	Transition models.WorkflowTransition
	Event      models.WorkflowEvent
}

// CommitManualWorkflowTransition prevents an API error from leaving the
// workflow in its new state without its transition and audit event.
func (r *GormRepository) CommitManualWorkflowTransition(
	finalization WorkflowTransitionFinalization,
) (*models.WorkflowItem, bool, error) {
	expected, updated := finalization.Expected, finalization.Updated
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID ||
		expected.CurrentState == updated.CurrentState {
		return nil, false, fmt.Errorf("matching workflow revisions with a state change are required")
	}
	if finalization.Transition.WorkflowID != expected.ID ||
		finalization.Transition.FromState != expected.CurrentState ||
		finalization.Transition.ToState != updated.CurrentState ||
		finalization.Event.WorkflowID != expected.ID ||
		finalization.Event.EventType != "workflow.transition" ||
		finalization.Event.FromState != expected.CurrentState ||
		finalization.Event.ToState != updated.CurrentState ||
		finalization.Event.Trigger != finalization.Transition.Trigger ||
		finalization.Event.Actor != finalization.Transition.Actor {
		return nil, false, fmt.Errorf("workflow transition and audit event must match the state change")
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return nil, false, workflowPersistenceFailure("manual workflow transition", fmt.Errorf("PostgreSQL is required"))
	}

	var committed *models.WorkflowItem
	changed := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		txRepository := &GormRepository{DB: tx}
		item, didChange, err := txRepository.UpdateWorkflowItemCAS(expected, updated)
		if err != nil {
			return fmt.Errorf("update workflow state: %w", err)
		}
		if !didChange || item == nil {
			return nil
		}
		if _, err := txRepository.CreateTransition(&finalization.Transition); err != nil {
			return workflowAuditPersistenceFailure("manual transition record", err)
		}
		if _, err := txRepository.CreateEvent(&finalization.Event); err != nil {
			return workflowAuditPersistenceFailure("manual transition event", err)
		}
		committed = item
		changed = true
		return nil
	})
	if err != nil {
		var auditErr *WorkflowAuditPersistenceError
		if errors.As(err, &auditErr) {
			return nil, false, err
		}
		return nil, false, workflowPersistenceFailure("manual workflow transition", err)
	}
	return committed, changed, nil
}
