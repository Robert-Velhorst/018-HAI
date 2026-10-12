package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type outcomeEvidenceRuntimeAdapter struct{ fakeAgentRuntimeAdapter }

func (a *outcomeEvidenceRuntimeAdapter) ExecuteTask(ctx context.Context, task agentruntime.Task) agentruntime.Result {
	result := a.fakeAgentRuntimeAdapter.ExecuteTask(ctx, task)
	result.ExecutionReference = "runtime-reference-fixture"
	return result
}

func TestOutcomePersistenceFailureRetainsRuntimeOutputAndRoute(t *testing.T) {
	id := uuid.New()
	adapter := &outcomeEvidenceRuntimeAdapter{fakeAgentRuntimeAdapter: fakeAgentRuntimeAdapter{id: "hermes"}}
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: "Runtime evidence", URLPath: "runtime-evidence", Host: "localhost", Port: 8080,
		LaunchType: "agent_runtime", RuntimeType: "hermes", LaunchTarget: "runtime://hermes",
	})
	engine := newTestServiceWithAuthorizedRuntime(t, repo, events.Publisher{}, adapter)
	request := approvedTaskLaunchRequest(t, engine, id, TaskLaunchRequest{OwnerIdentity: "alice", Task: "Inspect approved local sources"})
	repo.saveLaunchErr = errors.New("outcome persistence unavailable")
	result, err := engine.LaunchTask(id, request)
	if err != nil || result == nil || result.Status != "indeterminate" || !adapter.called {
		t.Fatalf("expected uncertain executed receipt: result=%#v err=%v", result, err)
	}
	if result.Output != "verified runtime output" || result.RuntimeRouteTrace == nil || result.RuntimeRouteTrace.RuntimeID != "hermes" ||
		!strings.Contains(result.RuntimeRouteTrace.Intent, "software engineering") || result.RuntimeTaskID == "" || result.ExecutionReference != "runtime-reference-fixture" {
		t.Fatalf("actual available evidence was lost: %#v", result)
	}
	if len(repo.launchEvents) != 0 || len(repo.launchIntents) != 1 || result.LaunchEventID != repo.launchIntents[0].ID {
		t.Fatalf("uncertain receipt fabricated persisted outcome: result=%#v intents=%#v", result, repo.launchIntents)
	}
}
