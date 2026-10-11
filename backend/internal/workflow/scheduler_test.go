package workflow

import (
	"testing"
	"time"
)

func TestWorkflowSchedulerRunsOpenLoopsBeforeRunnableItems(t *testing.T) {
	service := &fakeScheduledWorkflowService{
		openLoopResult: &OpenLoopRunSummary{Checked: 1, Triggered: 1},
		runDueResult:   &WorkflowRunSummary{Checked: 1, Completed: 1},
	}
	scheduler := NewScheduler(service, time.Minute, 3)

	scheduler.runOnce()

	if got := service.calls; len(got) != 3 || got[0] != "recover" || got[1] != "open-loops" || got[2] != "run-due" {
		t.Fatalf("calls = %#v, want recover, open-loops, then run-due", got)
	}
	if service.openLoopLimit != 3 || service.runDueLimit != 3 {
		t.Fatalf("limits = open-loop %d run-due %d, want 3", service.openLoopLimit, service.runDueLimit)
	}
}

func TestWorkflowSchedulerCanDisableOpenLoopPass(t *testing.T) {
	t.Setenv("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", "false")
	service := &fakeScheduledWorkflowService{runDueResult: &WorkflowRunSummary{Checked: 1}}
	scheduler := NewScheduler(service, time.Minute, 2)

	scheduler.runOnce()

	if got := service.calls; len(got) != 2 || got[0] != "recover" || got[1] != "run-due" {
		t.Fatalf("calls = %#v, want recover then run-due", got)
	}
}

func TestWorkflowSchedulerDoesNothingWhenBackgroundIsStopped(t *testing.T) {
	service := &fakeScheduledWorkflowService{}
	scheduler := NewScheduler(service, time.Minute, 2, func() bool { return false })

	scheduler.runOnce()

	if len(service.calls) != 0 {
		t.Fatalf("calls = %#v, want no background work while stopped", service.calls)
	}
}

func TestWorkflowSchedulerLimitDefaultsAndCaps(t *testing.T) {
	t.Setenv("WORKFLOW_SCHEDULER_RUN_LIMIT", "75")
	if got := schedulerLimit(); got != 50 {
		t.Fatalf("capped limit = %d, want 50", got)
	}
	t.Setenv("WORKFLOW_SCHEDULER_RUN_LIMIT", "bad")
	if got := schedulerLimit(); got != 2 {
		t.Fatalf("fallback limit = %d, want 2", got)
	}
}

func TestWorkflowClaimLeaseDefaultsAndBounds(t *testing.T) {
	t.Setenv("WORKFLOW_CLAIM_LEASE_SECONDS", "")
	if got := claimLeaseDuration(); got != 15*time.Minute {
		t.Fatalf("default lease = %s, want 15m", got)
	}
	t.Setenv("WORKFLOW_CLAIM_LEASE_SECONDS", "30")
	if got := claimLeaseDuration(); got != 15*time.Minute {
		t.Fatalf("short lease = %s, want safe default 15m", got)
	}
	t.Setenv("WORKFLOW_CLAIM_LEASE_SECONDS", "120")
	if got := claimLeaseDuration(); got != 2*time.Minute {
		t.Fatalf("configured lease = %s, want 2m", got)
	}
	t.Setenv("WORKFLOW_CLAIM_LEASE_SECONDS", "999999")
	if got := claimLeaseDuration(); got != 24*time.Hour {
		t.Fatalf("capped lease = %s, want 24h", got)
	}
}

type fakeScheduledWorkflowService struct {
	calls          []string
	openLoopLimit  int
	runDueLimit    int
	openLoopResult *OpenLoopRunSummary
	runDueResult   *WorkflowRunSummary
}

func (s *fakeScheduledWorkflowService) RecoverStaleClaims(request RunDueRequest) (*ClaimRecoverySummary, error) {
	s.calls = append(s.calls, "recover")
	return &ClaimRecoverySummary{}, nil
}

func (s *fakeScheduledWorkflowService) RunDue(request RunDueRequest) (*WorkflowRunSummary, error) {
	s.calls = append(s.calls, "run-due")
	s.runDueLimit = request.Limit
	if s.runDueResult != nil {
		return s.runDueResult, nil
	}
	return &WorkflowRunSummary{}, nil
}

func (s *fakeScheduledWorkflowService) RunDueOpenLoops(request RunDueRequest) (*OpenLoopRunSummary, error) {
	s.calls = append(s.calls, "open-loops")
	s.openLoopLimit = request.Limit
	if s.openLoopResult != nil {
		return s.openLoopResult, nil
	}
	return &OpenLoopRunSummary{}, nil
}
