package workflow

import (
	"strings"
	"sync"
	"testing"

	"automation-hub-backend/internal/models"
)

type retractionBeforeIntakeActivationRepository struct {
	*fakeWorkflowRepo
	once sync.Once
}

func (r *retractionBeforeIntakeActivationRepository) UpdateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	if item != nil && item.CurrentState != StateBlocked {
		r.retractActiveIntake()
	}
	return r.fakeWorkflowRepo.UpdateItem(item)
}

func (r *retractionBeforeIntakeActivationRepository) UpdateWorkflowItemCAS(
	expected, updated *models.WorkflowItem,
) (*models.WorkflowItem, bool, error) {
	if expected != nil && updated != nil && expected.CurrentState == StateNewInput && updated.CurrentState != StateBlocked {
		r.retractActiveIntake()
	}
	return r.fakeWorkflowRepo.UpdateWorkflowItemCAS(expected, updated)
}

func (r *retractionBeforeIntakeActivationRepository) CommitWorkflowIntake(
	finalization WorkflowIntakeFinalization,
) (*models.WorkflowItem, bool, error) {
	if finalization.Expected != nil && finalization.Updated != nil &&
		finalization.Expected.CurrentState == StateNewInput && finalization.Updated.CurrentState != StateBlocked {
		r.retractActiveIntake()
	}
	return r.fakeWorkflowRepo.CommitWorkflowIntake(finalization)
}

func (r *retractionBeforeIntakeActivationRepository) retractActiveIntake() {
	r.once.Do(func() {
		for _, stored := range r.fakeWorkflowRepo.items {
			if stored.SourceType != "email" || stored.SourceID != "intake-retraction-race" {
				continue
			}
			expected := *stored
			updated := expected
			updated.CurrentState = StateBlocked
			updated.BlockedReason = "source record was retracted during intake"
			updated.NextAction = "review the retracted source record before any further execution"
			quarantineRetractedWorkflow(&updated, updated.BlockedReason)
			if _, changed, err := r.fakeWorkflowRepo.UpdateWorkflowItemCAS(&expected, &updated); err != nil || !changed {
				return
			}
			_, _ = r.fakeWorkflowRepo.CreateEvent(&models.WorkflowEvent{
				WorkflowID: updated.ID,
				EventType:  "workflow.source_retracted",
				FromState:  expected.CurrentState,
				ToState:    StateBlocked,
				Message:    "source record was retracted during intake",
				Trigger:    "source_retraction",
				Actor:      "test-source-worker",
			})
			return
		}
	})
}

func TestIntakeCannotOverwriteConcurrentSourceRetraction(t *testing.T) {
	repository := &retractionBeforeIntakeActivationRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
	service := NewService(repository)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a checklist for organizing project documents.",
		SourceType:    "email",
		SourceID:      "intake-retraction-race",
		SourceURI:     "local://message/intake-retraction-race",
	})
	if err == nil {
		t.Fatalf("Intake unexpectedly succeeded after source retraction: %#v", record)
	}

	var stored *models.WorkflowItem
	for _, item := range repository.fakeWorkflowRepo.items {
		if item.SourceType == "email" && item.SourceID == "intake-retraction-race" {
			stored = item
			break
		}
	}
	if stored == nil {
		t.Fatal("source-bound intake item was not retained for recovery")
	}
	if stored.CurrentState != StateBlocked || !strings.HasPrefix(stored.ApprovalReason, sourceRetractionQuarantinePrefix) {
		t.Fatalf("intake overwrote the source-retraction quarantine: %#v", stored)
	}
	if !hasWorkflowEvent(repository.fakeWorkflowRepo.events[stored.ID], "workflow.source_retracted") {
		t.Fatal("source-retraction audit event was lost")
	}
}
