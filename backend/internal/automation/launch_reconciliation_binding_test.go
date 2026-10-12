package automation

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestLaunchOutcomeReplayRequiresIntentBoundEventAndTarget(t *testing.T) {
	intentID := uuid.New()
	automationID := uuid.New()
	intent := &models.AutomationLaunchEvent{
		ID:            intentID,
		AutomationID:  automationID,
		OwnerIdentity: "alice",
		LaunchType:    "api_intent",
		EventKey:      automationLaunchRequestEventKey(automationID, "alice", "request-1"),
		Target:        "POST https://service.example/apply?token=%5BREDACTED%5D",
		Status:        "pending",
		StartedAt:     time.Now().UTC(),
	}
	automation := &models.Automation{ID: automationID, LaunchType: "api"}
	valid := &models.AutomationLaunchEvent{
		ID:            uuid.New(),
		AutomationID:  automationID,
		OwnerIdentity: "alice",
		LaunchType:    "api",
		EventKey:      automationLaunchOutcomeEventKey(intentID),
		Target:        intent.Target,
		Status:        "completed",
	}

	if !launchOutcomeMatchesIntent(automation, intent, valid, "api") {
		t.Fatal("correctly bound launch outcome was rejected")
	}

	t.Run("target mismatch", func(t *testing.T) {
		outcome := *valid
		outcome.Target = "POST https://service.example/other"
		if launchOutcomeMatchesIntent(automation, intent, &outcome, "api") {
			t.Fatal("outcome for a different target was accepted as this intent's result")
		}
	})

	t.Run("event key mismatch", func(t *testing.T) {
		outcome := *valid
		outcome.EventKey = runtimeStopOutcomeEventKey(intentID)
		if launchOutcomeMatchesIntent(automation, intent, &outcome, "api") {
			t.Fatal("non-launch outcome key was accepted as this launch's result")
		}
	})
}
