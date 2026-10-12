package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestContextualWorkflowCancellationKeepsEnteredRunnerFencedUntilReturn(t *testing.T) {
	repo := newFakeWorkflowRepo()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan string, 1)
	release := make(chan struct{})
	runner := &fakeTaskRunner{result: &TaskRunResult{PlanID: "cancelled-plan", Passed: true, CompletionStatus: "validated", VerificationStatus: "verified"}}
	runner.onRun = func(request TaskRunRequest) {
		if request.ExecutionContext != ctx {
			t.Error("runner lifetime was dropped")
		}
		cancel()
		entered <- request.WorkflowID
		<-release
	}
	svc := NewServiceWithTaskRunner(repo, runner).(*service)
	for _, name := range []string{"first", "second"} {
		if _, err := svc.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk admin checklist for " + name}); err != nil {
			t.Fatal(err)
		}
	}
	type response struct {
		result *WorkflowRunSummary
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := svc.RunDueForOwnerContext(ctx, "alice", RunDueRequest{Limit: 5})
		done <- response{result, err}
	}()
	var id uuid.UUID
	select {
	case value := <-entered:
		id = uuid.MustParse(value)
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("runner not entered")
	}
	if _, active := svc.activeTaskRuns.Load(id); !active {
		close(release)
		t.Fatal("cancellation prematurely released active runner")
	}
	select {
	case result := <-done:
		close(release)
		t.Fatalf("uncooperative runner falsely reported terminal: %#v", result)
	default:
	}
	close(release)
	select {
	case response := <-done:
		if !errors.Is(response.err, context.Canceled) || response.result == nil || response.result.Completed != 0 || len(runner.requests) != 1 || len(response.result.Results) != 1 {
			t.Fatalf("cancelled batch completed or continued: %#v err=%v calls=%d", response.result, response.err, len(runner.requests))
		}
		if response.result.Results[0].Status != "blocked" {
			t.Fatal("cancellation did not require review")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("returned runner not joined")
	}
	if _, active := svc.activeTaskRuns.Load(id); active {
		t.Fatal("terminal runner retained active registration")
	}
}

type legacyExecutionRunner struct{ calls int }

func (r *legacyExecutionRunner) RunWorkflowTask(TaskRunRequest) (*TaskRunResult, error) {
	r.calls++
	return nil, nil
}

func TestContextualWorkflowRefusesLegacyRunnerBeforeClaim(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &legacyExecutionRunner{}
	svc := NewServiceWithTaskRunner(repo, runner).(*service)
	item, err := svc.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk admin checklist"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RunDueForOwnerContext(context.Background(), "alice", RunDueRequest{Limit: 5})
	if !errors.Is(err, ErrTaskExecutionContextUnavailable) || result != nil || runner.calls != 0 || repo.items[item.Item.ID].CurrentState != StateReady {
		t.Fatalf("legacy execution accepted: result=%#v err=%v", result, err)
	}
}
