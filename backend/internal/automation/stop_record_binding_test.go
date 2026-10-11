package automation

import (
	"errors"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type stopRecordProbe struct {
	Repository
	record      *models.AutomationLaunchEvent
	pending     bool
	stopLookups int
}

func (r *stopRecordProbe) FindOwnerActiveRuntimeLaunch(uuid.UUID, string, string) (*models.AutomationLaunchEvent, error) {
	if r.pending {
		return nil, nil
	}
	return r.record, nil
}
func (r *stopRecordProbe) FindPendingRuntimeLaunchIntent(uuid.UUID, string) (*models.AutomationLaunchEvent, error) {
	return r.record, nil
}
func (r *stopRecordProbe) FindLaunchIntentByEventKey(string) (*models.AutomationLaunchEvent, error) {
	r.stopLookups++
	return nil, errors.New("test stops before cancellation admission")
}

func TestRuntimeStopRechecksRecordKindAndState(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status    string
		pending, nilID, valid bool
	}{
		{"active", "agent_runtime", "running", false, false, true},
		{"pending", "agent_runtime_intent", "pending", true, false, true},
		{"wrong_active_kind", "agent_runtime_stop", "running", false, false, false},
		{"intent_from_active_query", "agent_runtime_intent", "pending", false, false, false},
		{"outcome_from_pending_query", "agent_runtime", "running", true, false, false},
		{"finished_intent", "agent_runtime_intent", "completed", true, false, false},
		{"missing_active_id", "agent_runtime", "running", false, true, false},
		{"missing_pending_id", "agent_runtime_intent", "pending", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			base := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
			record := &models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", RuntimeTaskID: "task-1", LaunchType: tc.kind, Status: tc.status}
			if tc.nilID {
				record.ID = uuid.Nil
			}
			probe := &stopRecordProbe{Repository: base, record: record, pending: tc.pending}
			result, err := newTestService(probe, events.Publisher{}).StopRuntimeTaskForOwner(id, "alice")
			if tc.valid {
				if probe.stopLookups != 1 || err == nil {
					t.Fatal("valid record did not reach stop admission")
				}
			} else if err != nil || result == nil || result.Status != "indeterminate" || probe.stopLookups != 0 {
				t.Fatal("invalid record reached stop admission or did not retain uncertainty")
			}
			if len(base.launchIntents) != 0 {
				t.Fatal("test must not create a cancellation intent")
			}
		})
	}
}
