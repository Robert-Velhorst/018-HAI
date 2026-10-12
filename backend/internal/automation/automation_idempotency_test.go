package automation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestLaunchRetryReusesPersistedIntentWithoutRepeatingEffect(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL + "/effect",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Apply the reviewed change.",
		ProjectKey:    "018-hai",
	})

	first, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	retry, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("retry LaunchTask: %v", err)
	}
	if first.Status != "completed" || retry.Status != "completed" || first.LaunchEventID != retry.LaunchEventID {
		t.Fatalf("first/retry results = %#v / %#v; expected the same completed outcome", first, retry)
	}
	if calls.Load() != 1 || len(repo.launchIntents) != 1 || len(repo.launchEvents) != 1 {
		t.Fatalf("repeated delivery duplicated work: calls=%d intents=%d outcomes=%d", calls.Load(), len(repo.launchIntents), len(repo.launchEvents))
	}
	wantEventKey := automationLaunchRequestEventKey(id, "alice", request.IdempotencyKey)
	if repo.launchIntents[0].EventKey != wantEventKey || repo.launchIntents[0].EventKey == request.IdempotencyKey {
		t.Fatalf("persisted retry identity = %q, want owner-scoped digest %q without raw key", repo.launchIntents[0].EventKey, wantEventKey)
	}
}

func TestLaunchRetryRepairsAutomationSummaryAfterProjectionWriteFailure(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, LaunchType: "api", LaunchTarget: "POST " + target.URL + "/effect",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	repo.updateErr = errors.New("summary store unavailable")
	repo.updateErrOnCall = 1
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice", Task: "Apply the reviewed change.", ProjectKey: "018-hai",
	})

	if _, err := service.LaunchTask(id, request); err == nil {
		t.Fatal("first launch must report the failed summary projection write")
	}
	if calls.Load() != 1 || len(repo.launchEvents) != 1 || repo.automation.LastLaunchAt != nil {
		t.Fatalf("first launch state = calls %d, events %d, summary %v; expected one effect, durable outcome, and missing projection", calls.Load(), len(repo.launchEvents), repo.automation.LastLaunchAt)
	}

	replayed, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("retry should project the durable outcome without redispatch: %v", err)
	}
	if replayed.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("retry = %#v with %d external calls; want completed replay and one effect", replayed, calls.Load())
	}
	if repo.automation.LastLaunchAt == nil || !repo.automation.LastLaunchAt.Equal(repo.launchEvents[0].StartedAt) {
		t.Fatalf("retry did not repair LastLaunchAt from the durable event: summary=%v event=%v", repo.automation.LastLaunchAt, repo.launchEvents[0].StartedAt)
	}
}

func TestNewLogicalLaunchUsesDifferentIdempotencyKey(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL + "/effect",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	firstRequest := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Apply the reviewed change.",
		ProjectKey:    "018-hai",
	})
	first, err := service.LaunchTask(id, firstRequest)
	if err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	secondRequest := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Apply the reviewed change.",
		ProjectKey:    "018-hai",
	})
	if firstRequest.IdempotencyKey == secondRequest.IdempotencyKey {
		t.Fatal("new logical launch reused the previous request key")
	}
	second, err := service.LaunchTask(id, secondRequest)
	if err != nil {
		t.Fatalf("second logical LaunchTask: %v", err)
	}
	if calls.Load() != 2 || len(repo.launchIntents) != 2 || len(repo.launchEvents) != 2 || first.LaunchEventID == second.LaunchEventID {
		t.Fatalf("new logical launch was not independent: calls=%d intents=%d outcomes=%d first=%s second=%s", calls.Load(), len(repo.launchIntents), len(repo.launchEvents), first.LaunchEventID, second.LaunchEventID)
	}
	if repo.launchIntents[0].EventKey == repo.launchIntents[1].EventKey {
		t.Fatal("distinct logical launches persisted the same idempotency identity")
	}
}

func TestAmbiguousLaunchOutcomeFailsClosedOnRetry(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL + "/effect",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	repo.saveLaunchErr = errors.New("outcome persistence response lost")
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Apply the reviewed change.",
		ProjectKey:    "018-hai",
	})

	first, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	if first.Status != "indeterminate" || calls.Load() != 1 {
		t.Fatalf("first uncertain outcome = %#v calls=%d; want indeterminate after one effect", first, calls.Load())
	}
	retry, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("retry LaunchTask: %v", err)
	}
	if retry.Status != "indeterminate" || calls.Load() != 1 || len(repo.launchIntents) != 1 {
		t.Fatalf("ambiguous retry replayed or changed the run: retry=%#v calls=%d intents=%d", retry, calls.Load(), len(repo.launchIntents))
	}
}

func TestRuntimeLaunchRetryResolvesReconciledTerminalOutcome(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw", LaunchTarget: "runtime://openclaw",
	})
	repo.saveLaunchErr = errors.New("initial outcome persistence failed")
	repo.saveLaunchErrOnCall = 1
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, agentruntime.NewRegistry(adapter))
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice", Task: "Complete the reviewed runtime task.", ProjectKey: "018-hai",
	})

	first, err := service.LaunchTask(id, request)
	if err != nil || first.Status != "indeterminate" || len(repo.launchIntents) != 1 {
		t.Fatalf("initial launch = %#v, err=%v; want an immutable unresolved intent", first, err)
	}
	intent := repo.launchIntents[0]
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw",
		LaunchType: "agent_runtime_openclaw_terminal", RuntimeTaskID: intent.RuntimeTaskID,
		ExecutionReference: intent.ExecutionReference, EventKey: "openclaw-terminal:test",
		Status: "completed", Message: "Gateway terminal outcome verified", Output: "reconciled result",
	})

	retry, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("replay after terminal reconciliation: %v", err)
	}
	if retry.Status != "completed" || retry.Output != "reconciled result" || retry.LaunchType != "agent_runtime" || len(repo.launchIntents) != 1 {
		t.Fatalf("reconciled terminal outcome was not replayed: %#v intents=%d", retry, len(repo.launchIntents))
	}
}

func TestLaunchIdempotencyKeyCannotBindDifferentAction(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL + "/effect",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	firstRequest := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity:  "alice",
		Task:           "Apply the reviewed change.",
		ProjectKey:     "018-hai",
		IdempotencyKey: "reused-request-key",
	})
	if _, err := service.LaunchTask(id, firstRequest); err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	changedRequest := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity:  "alice",
		Task:           "Apply a different change.",
		ProjectKey:     "018-hai",
		IdempotencyKey: firstRequest.IdempotencyKey,
	})
	if _, err := service.LaunchTask(id, changedRequest); !errors.Is(err, ErrLaunchIdempotencyConflict) {
		t.Fatalf("reused key for changed action error = %v, want %v", err, ErrLaunchIdempotencyConflict)
	}
	if calls.Load() != 1 || len(repo.launchIntents) != 1 {
		t.Fatalf("changed action was dispatched or claimed: calls=%d intents=%d", calls.Load(), len(repo.launchIntents))
	}
}

func TestEveryAPIHTTPMethodRequiresIdempotency(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			automation := &models.Automation{
				LaunchType:   "api",
				LaunchTarget: method + " https://example.invalid/resource",
			}
			if !automationLaunchRequiresIdempotency(automation) {
				t.Fatalf("%s API launch was treated as read-only based on its verb", method)
			}
		})
	}
}

func TestLaunchReplayPreservesPersistedApprovalRequirement(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		LaunchType:         "api",
		LaunchTarget:       "GET " + target.URL + "/probe",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity:  "alice",
		Task:           "Verify the bounded endpoint.",
		ProjectKey:     "018-hai",
		IdempotencyKey: uuid.NewString(),
	})
	first, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	if first.Status != "completed" || calls.Load() != 1 || len(repo.launchEvents) != 1 {
		t.Fatalf("first launch result = %#v calls=%d outcomes=%d", first, calls.Load(), len(repo.launchEvents))
	}

	repo.launchEventsMu.Lock()
	repo.launchEvents[0].RequiresApproval = true
	repo.launchEventsMu.Unlock()
	replayed, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if !replayed.RequiresApproval || calls.Load() != 1 {
		t.Fatalf("replay = %#v calls=%d; approval requirement must survive replay without redispatch", replayed, calls.Load())
	}
}

func TestPostgresLaunchIntentIdempotencyIsAtomicAcrossConcurrentRequests(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := NewGormUserRepository(db)
	automationID := uuid.New()
	eventKey := automationLaunchRequestEventKey(automationID, "idempotency-postgres-test", uuid.NewString())
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Where("event_key = ?", eventKey).Delete(&models.AutomationLaunchEvent{}).Error; err != nil {
			t.Errorf("remove isolated launch intent test record: %v", err)
		}
	})

	intents := []*models.AutomationLaunchEvent{
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "idempotency-postgres-test", LaunchType: "api_intent", EventKey: eventKey, Status: "pending", StartedAt: now, CompletedAt: now},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "idempotency-postgres-test", LaunchType: "api_intent", EventKey: eventKey, Status: "pending", StartedAt: now, CompletedAt: now},
	}
	results := make(chan error, len(intents))
	var workers sync.WaitGroup
	for _, intent := range intents {
		workers.Add(1)
		go func(intent *models.AutomationLaunchEvent) {
			defer workers.Done()
			results <- repo.SaveLaunchIntent(intent)
		}(intent)
	}
	workers.Wait()
	close(results)

	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errLaunchIntentAlreadyExists):
			conflicts++
		default:
			t.Fatalf("concurrent launch intent persistence: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent intent claims = successes %d, conflicts %d; want one of each", successes, conflicts)
	}

	stored, err := repo.FindLaunchIntentByEventKey(eventKey)
	if err != nil {
		t.Fatalf("load winning idempotency intent: %v", err)
	}
	if stored == nil || (stored.ID != intents[0].ID && stored.ID != intents[1].ID) {
		t.Fatalf("stored idempotency intent = %#v; want exactly one submitted intent", stored)
	}
	var count int64
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("event_key = ?", eventKey).Count(&count).Error; err != nil {
		t.Fatalf("count idempotency intents: %v", err)
	}
	if count != 1 {
		t.Fatalf("database stored %d records for one idempotency key; want one", count)
	}
}

func TestPostgresLaunchEventIdempotencyReturnsStoredIdentityAndRejectsConflicts(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := NewGormUserRepository(db)
	eventKey := "automation-launch-outcome:audit:" + uuid.NewString()
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Where("event_key = ?", eventKey).Delete(&models.AutomationLaunchEvent{}).Error; err != nil {
			t.Errorf("remove isolated launch outcome test record: %v", err)
		}
	})

	stored := &models.AutomationLaunchEvent{
		ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: "launch-event-idempotency-test",
		LaunchType: "api", EventKey: eventKey, Status: "completed", Message: "effect completed",
		Output: "durable output", ExitCode: 0, StartedAt: now, CompletedAt: now.Add(time.Second),
	}
	if err := repo.SaveLaunchEvent(stored); err != nil {
		t.Fatalf("save initial launch outcome: %v", err)
	}

	retry := *stored
	retry.ID = uuid.New()
	if err := repo.SaveLaunchEvent(&retry); err != nil {
		t.Fatalf("save identical launch outcome retry: %v", err)
	}
	if retry.ID != stored.ID {
		t.Fatalf("idempotent outcome retry returned ID %s; want stored row ID %s", retry.ID, stored.ID)
	}

	conflicting := *stored
	conflicting.ID = uuid.New()
	conflicting.Status = "failed"
	conflicting.Message = "different outcome for the same event key"
	if err := repo.SaveLaunchEvent(&conflicting); !errors.Is(err, errLaunchEventKeyConflict) {
		t.Fatalf("conflicting launch outcome error = %v; want %v", err, errLaunchEventKeyConflict)
	}
	var persisted models.AutomationLaunchEvent
	if err := db.Where("event_key = ?", eventKey).First(&persisted).Error; err != nil {
		t.Fatalf("reload durable outcome after conflicting retry: %v", err)
	}
	if persisted.ID != stored.ID || persisted.Status != "completed" || persisted.Message != stored.Message {
		t.Fatalf("conflicting retry changed durable outcome: %#v", persisted)
	}
}

func TestPostgresOwnerLaunchEventsAreOwnerScopedAndExcludeIntentRecords(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := NewGormUserRepository(db)
	automationID := uuid.New()
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Where("automation_id = ?", automationID).Delete(&models.AutomationLaunchEvent{}).Error; err != nil {
			t.Errorf("remove isolated owner diagnostics test records: %v", err)
		}
	})

	events := []models.AutomationLaunchEvent{
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "owner-a", LaunchType: "api", Status: "completed", Message: "owner-a-private-output", StartedAt: now, CompletedAt: now},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "owner-b", LaunchType: "api", Status: "completed", Message: "owner-b-private-output", StartedAt: now.Add(time.Second), CompletedAt: now.Add(time.Second)},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "owner-a", LaunchType: "api_intent", Status: "pending", Message: "intent-record", StartedAt: now.Add(2 * time.Second), CompletedAt: now.Add(2 * time.Second)},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "owner-a", LaunchType: "approval_decision", Status: "approved", Message: "approval-record", StartedAt: now.Add(3 * time.Second), CompletedAt: now.Add(3 * time.Second)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("insert isolated launch diagnostic events: %v", err)
	}

	ownerAEvents, err := repo.FindOwnerLaunchEvents(automationID, " owner-a ", 10)
	if err != nil {
		t.Fatalf("find owner-a launch diagnostics: %v", err)
	}
	if len(ownerAEvents) != 1 || ownerAEvents[0].OwnerIdentity != "owner-a" || ownerAEvents[0].Message != "owner-a-private-output" {
		t.Fatalf("owner-a diagnostics = %#v; want only owner's non-intent operational event", ownerAEvents)
	}

	ownerBEvents, err := repo.FindOwnerLaunchEvents(automationID, "owner-b", 10)
	if err != nil {
		t.Fatalf("find owner-b launch diagnostics: %v", err)
	}
	if len(ownerBEvents) != 1 || ownerBEvents[0].OwnerIdentity != "owner-b" || ownerBEvents[0].Message != "owner-b-private-output" {
		t.Fatalf("owner-b diagnostics = %#v; want only owner's non-intent operational event", ownerBEvents)
	}

	if _, err := repo.FindOwnerLaunchEvents(automationID, " ", 10); err == nil {
		t.Fatal("owner diagnostics query accepted an empty owner identity")
	}
}

func TestPostgresReconciledRuntimeOutcomeOverridesStaleStatusAndBlocksStopLookup(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := NewGormUserRepository(db)
	automationID := uuid.New()
	intentID := uuid.New()
	owner := "mixed-case-runtime-owner-" + uuid.NewString()
	taskID := "automation:" + automationID.String() + ":intent:" + intentID.String()
	reference := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Where("automation_id = ?", automationID).Delete(&models.AutomationLaunchEvent{}).Error; err != nil {
			t.Errorf("remove isolated runtime reconciliation test records: %v", err)
		}
	})

	events := []models.AutomationLaunchEvent{
		{ID: intentID, AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "OpenClaw", LaunchType: "agent_runtime_intent", RuntimeTaskID: taskID, ExecutionReference: reference, Status: "pending", StartedAt: now, CompletedAt: now},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "OpenClaw", LaunchType: "agent_runtime", EventKey: automationLaunchOutcomeEventKey(intentID), RuntimeTaskID: taskID, ExecutionReference: reference, Status: "running", StartedAt: now, CompletedAt: now.Add(time.Second)},
		{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw", LaunchType: "agent_runtime_openclaw_terminal", EventKey: "openclaw-gateway-terminal:" + reference, RuntimeTaskID: taskID, ExecutionReference: reference, Status: "completed", StartedAt: now, CompletedAt: now.Add(2 * time.Second)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("insert runtime launch and terminal events: %v", err)
	}

	outcome, err := repo.FindLaunchOutcomeByIntentID(intentID)
	if err != nil {
		t.Fatalf("find runtime outcome after reconciliation: %v", err)
	}
	if outcome == nil || outcome.LaunchType != "agent_runtime_openclaw_terminal" || outcome.Status != "completed" {
		t.Fatalf("replayed runtime outcome = %#v; want reconciled completed status", outcome)
	}
	active, err := repo.FindOwnerActiveRuntimeLaunch(automationID, "openclaw", owner)
	if err != nil {
		t.Fatalf("find active runtime after terminal reconciliation: %v", err)
	}
	if active != nil {
		t.Fatalf("terminally reconciled runtime remained stoppable: %#v", active)
	}
	if pending, err := repo.FindPendingRuntimeLaunchIntent(automationID, owner); err != nil || pending != nil {
		t.Fatalf("terminal launch was returned as pending: intent=%#v err=%v", pending, err)
	}

	if err := db.Delete(&events[2]).Error; err != nil {
		t.Fatalf("remove terminal event to verify mixed-case active lookup: %v", err)
	}
	active, err = repo.FindOwnerActiveRuntimeLaunch(automationID, "openclaw", owner)
	if err != nil {
		t.Fatalf("find mixed-case active runtime: %v", err)
	}
	if active == nil || active.RuntimeType != "OpenClaw" || active.RuntimeTaskID != taskID {
		t.Fatalf("mixed-case active runtime lookup = %#v; want the persisted OpenClaw task", active)
	}
}

func TestPostgresPendingRuntimeIntentExcludedByTerminalEventWithoutOrdinaryOutcome(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := NewGormUserRepository(db)
	automationID := uuid.New()
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Where("automation_id = ?", automationID).Delete(&models.AutomationLaunchEvent{}).Error; err != nil {
			t.Errorf("remove isolated terminal intent test records: %v", err)
		}
	})

	cases := []struct {
		name         string
		owner        string
		runtimeType  string
		terminalType string
	}{
		{name: "openclaw", owner: "openclaw-terminal-" + uuid.NewString(), runtimeType: "OpenClaw", terminalType: "agent_runtime_openclaw_terminal"},
		{name: "host completion", owner: "host-terminal-" + uuid.NewString(), runtimeType: "Hermes", terminalType: "agent_runtime_host_completion"},
	}
	var events []models.AutomationLaunchEvent
	for index, testCase := range cases {
		intentID := uuid.New()
		taskID := "automation:" + automationID.String() + ":intent:" + intentID.String()
		startedAt := now.Add(time.Duration(index) * time.Minute)
		events = append(events,
			models.AutomationLaunchEvent{
				ID: intentID, AutomationID: automationID, OwnerIdentity: testCase.owner,
				RuntimeType: testCase.runtimeType, LaunchType: "agent_runtime_intent",
				RuntimeTaskID: taskID, Status: "pending", StartedAt: startedAt, CompletedAt: startedAt,
			},
			models.AutomationLaunchEvent{
				ID: uuid.New(), AutomationID: automationID, OwnerIdentity: testCase.owner,
				RuntimeType: strings.ToLower(testCase.runtimeType), LaunchType: testCase.terminalType,
				RuntimeTaskID: taskID, EventKey: testCase.terminalType + ":" + intentID.String(),
				Status: "completed", StartedAt: startedAt.Add(time.Second), CompletedAt: startedAt.Add(time.Second),
			},
		)
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("insert pending intents and terminal events without ordinary outcomes: %v", err)
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pending, err := repo.FindPendingRuntimeLaunchIntent(automationID, testCase.owner)
			if err != nil {
				t.Fatalf("find pending intent after terminal reconciliation: %v", err)
			}
			if pending != nil {
				t.Fatalf("terminally reconciled intent remained pending without an ordinary outcome: %#v", pending)
			}
		})
	}
}
