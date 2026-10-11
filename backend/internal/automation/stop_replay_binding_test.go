package automation

import (
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type stopReplayProbe struct {
	Repository
	outcome   *models.AutomationLaunchEvent
	requested uuid.UUID
}

func (r *stopReplayProbe) FindLaunchOutcomeByIntentID(id uuid.UUID) (*models.AutomationLaunchEvent, error) {
	r.requested = id
	return r.outcome, nil
}

func TestRuntimeStopReplayRequiresExactOutcomeReceipt(t *testing.T) {
	for _, mode := range []string{"valid", "valid_cli", "no_outcome", "nil_outcome_id", "different_reference", "different_owner", "different_task", "different_event_key", "different_automation", "different_runtime", "different_kind"} {
		t.Run(mode, func(t *testing.T) {
			automation := &models.Automation{ID: uuid.New()}
			intent := &models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automation.ID, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime_stop_intent", RuntimeTaskID: "task-1", ExecutionReference: "ocgw:v2:run-1"}
			outcome := &models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automation.ID, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime_stop", RuntimeTaskID: intent.RuntimeTaskID, ExecutionReference: intent.ExecutionReference, EventKey: runtimeStopOutcomeEventKey(intent.ID), Status: "cancellation_requested"}
			switch mode {
			case "valid_cli":
				intent.ExecutionReference = ""
				outcome.ExecutionReference = ""
			case "no_outcome":
				outcome = nil
			case "nil_outcome_id":
				outcome.ID = uuid.Nil
			case "different_reference":
				outcome.ExecutionReference = "ocgw:v2:other-run"
			case "different_owner":
				outcome.OwnerIdentity = "bob"
			case "different_task":
				outcome.RuntimeTaskID = "task-2"
			case "different_event_key":
				outcome.EventKey = runtimeStopOutcomeEventKey(uuid.New())
			case "different_automation":
				outcome.AutomationID = uuid.New()
			case "different_runtime":
				outcome.RuntimeType = "hermes"
			case "different_kind":
				outcome.LaunchType = "agent_runtime"
			}
			base := newFakeAutomationRepo(automation)
			probe := &stopReplayProbe{Repository: base, outcome: outcome}
			svc := newTestService(probe, events.Publisher{}).(*service)
			result, err := svc.replayRuntimeStopIntent(automation, "alice", "openclaw", "task-1", intent)
			if probe.requested != intent.ID {
				t.Fatal("replay did not look up the exact intent")
			}
			switch mode {
			case "valid", "valid_cli":
				if err != nil || result == nil || result.Status != "cancellation_requested" || result.EvidenceURI != "automation-launch://"+outcome.ID.String() {
					t.Fatal("valid receipt was lost")
				}
			case "no_outcome":
				if err != nil || result == nil || result.Status != "indeterminate" || result.EvidenceURI != "automation-launch://"+intent.ID.String() {
					t.Fatal("missing receipt did not retain unresolved intent")
				}
			default:
				if err == nil || result != nil {
					t.Fatal("mismatched receipt was replayed")
				}
			}
			if len(base.launchIntents) != 0 || len(base.launchEvents) != 0 {
				t.Fatal("replay must not write or repeat cancellation")
			}
		})
	}
}
