package workflow

import (
	"testing"

	"automation-hub-backend/internal/safety"
)

func TestRunDueFailsClosedWhenSafetyControlIsUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()

	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	item, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low-risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 0 {
		t.Fatalf("unreadable safety control did not block worker dispatch: summary=%#v calls=%d", summary, len(runner.requests))
	}
	updated, err := service.Get(item.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready so unavailable control cannot consume work", updated.Item.CurrentState)
	}
}
