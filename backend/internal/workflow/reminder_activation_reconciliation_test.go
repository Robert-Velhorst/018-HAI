package workflow

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func TestReminderActivationValidationRejectsMissingDecisionWithoutPanic(t *testing.T) {
	fixture := newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute))
	for _, test := range []struct {
		name       string
		activation *models.WorkflowReminderActivationRequest
	}{
		{name: "existing activation", activation: &fixture.activation},
		{name: "missing activation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatal("missing decision evidence caused a panic instead of rejection")
				}
			}()
			if err := validateReminderActivationDecision(test.activation, nil); err == nil {
				t.Fatal("missing decision evidence was accepted")
			}
		})
	}
}

type missingSavedReminderDecisionRepository struct {
	*reminderDeliveryFakeRepo
	saveCalls int
}

func (r *missingSavedReminderDecisionRepository) SaveReminderActivationDecision(*models.WorkflowReminderActivationDecision) (*models.WorkflowReminderActivationDecision, bool, error) {
	r.saveCalls++
	return nil, false, nil
}

func TestReminderActivationDoesNotReturnSuccessWithoutSavedDecision(t *testing.T) {
	fixture := newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute))
	repository := &missingSavedReminderDecisionRepository{reminderDeliveryFakeRepo: newReminderDeliveryFakeRepo()}
	fixture.seed(repository.reminderDeliveryFakeRepo)
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatal("missing saved decision caused a service panic")
		}
	}()
	result, err := NewService(repository).(ReminderActivationService).DecideReminderActivationForOwner(
		"alice", "alice", fixture.activation.ID, ReminderActivationDecisionRequest{
			Decision: ReminderActivationDecisionRejected, Reason: "Do not deliver this reminder.",
			Confirmation:                    ReminderActivationRejectConfirmation,
			ExpectedActivationRequestDigest: fixture.activation.RecordDigest,
			ExpectedPreviousDecisionID:      fixture.approval.ID.String(),
		},
	)
	if repository.saveCalls != 1 || result != nil || err == nil {
		t.Fatalf("missing saved evidence: calls=%d result-present=%v error-present=%v", repository.saveCalls, result != nil, err != nil)
	}
}

func TestReminderActivationValidationRetainsValidDecisionAndRejectsMissingActivation(t *testing.T) {
	fixture := newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute))
	if err := validateReminderActivationDecision(&fixture.activation, &fixture.approval); err != nil {
		t.Fatalf("valid owner-bound approval was rejected: %v", err)
	}
	if err := validateReminderActivationDecision(nil, &fixture.approval); err == nil {
		t.Fatal("decision without its activation evidence was accepted")
	}
}
