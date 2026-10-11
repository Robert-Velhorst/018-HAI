package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
)

func TestDurableTaskServiceReadsOnlyAuthenticatedOwnerState(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	base := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	for _, fixture := range []struct {
		owner string
		plan  string
	}{
		{owner: "alice", plan: "alice-plan"},
		{owner: "bob", plan: "bob-plan"},
	} {
		if err := repository.AppendCompletionPlan(
			fixture.owner,
			taskStateTestPlan(fixture.plan, fixture.owner, base),
		); err != nil {
			t.Fatalf("append %s completion plan: %v", fixture.owner, err)
		}
		review := taskStateTestReviewItem(fixture.owner, fixture.plan, base)
		if _, err := repository.CreateReviewItem(fixture.owner, review); err != nil {
			t.Fatalf("create %s review item: %v", fixture.owner, err)
		}
	}

	taskService := &service{
		stateRepository: repository,
		logs: []CompletionPlan{
			taskStateTestPlan("untrusted-mirror-plan", "bob", base),
		},
		reviewQueue: []ReviewQueueItem{
			taskStateTestReviewItem("bob", "untrusted-mirror-review", base),
		},
	}

	logs, err := taskService.LogsForOwnerWithError("alice")
	if err != nil {
		t.Fatalf("read Alice completion plans: %v", err)
	}
	if len(logs) != 1 || logs[0].ID != "alice-plan" || logs[0].OwnerIdentity != "alice" {
		t.Fatalf("Alice completion plans = %#v, want only durable Alice state", logs)
	}
	history, err := taskService.HistoryForOwnerWithLimit("alice", 10)
	if err != nil {
		t.Fatalf("read Alice compact task history: %v", err)
	}
	if len(history) != 1 || history[0].ID != "alice-plan" || history[0].Request == "" {
		t.Fatalf("Alice compact task history = %#v", history)
	}
	items, err := taskService.ReviewQueueForOwnerWithError("alice")
	if err != nil {
		t.Fatalf("read Alice review queue: %v", err)
	}
	if len(items) != 1 || items[0].TaskID != "alice-plan" || items[0].Request.OwnerIdentity != "alice" {
		t.Fatalf("Alice review queue = %#v, want only durable Alice state", items)
	}
}

func TestDurableTaskReviewApprovalReplaysExactStoredActionOnce(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	executor := &fakeToolExecutor{result: completedToolResult()}
	taskService := newDurableTaskTestService(t, repository, executor)
	scoped := taskService.(OwnerScopedService)

	plan, err := taskService.Run(IntakeRequest{
		OwnerIdentity:   "alice",
		Request:         "Delete account data by running a local script",
		ProjectKey:      "018-HAI",
		AutomationID:    executor.result.AutomationID,
		SuccessCriteria: []string{"the exact controlled action is verified"},
		ExecuteAllowed:  true,
	})
	if err != nil {
		t.Fatalf("queue reviewed task: %v", err)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatal("high-risk task did not create a durable review item")
	}
	reviewID := plan.ReviewQueueItem.ID

	if result, err := scoped.ResolveReviewItemForOwner(
		"bob",
		reviewID,
		ApprovalDecision{Approved: true, Note: "foreign approval"},
	); !errors.Is(err, ErrTaskStateNotFound) || result != nil {
		t.Fatalf("foreign owner resolution = %#v, %v; want owner-scoped not found", result, err)
	}
	if executor.calls != 0 {
		t.Fatalf("foreign owner resolution executed %d actions, want zero", executor.calls)
	}

	result, err := scoped.ResolveReviewItemForOwner(
		"alice",
		reviewID,
		ApprovalDecision{Approved: true, Note: "Approve this exact reviewed action"},
	)
	if err != nil {
		t.Fatalf("resolve exact reviewed action: %v", err)
	}
	if result == nil || result.Plan == nil || executor.calls != 1 || len(executor.requests) != 1 {
		t.Fatalf("exact replay result=%#v calls=%d requests=%#v", result, executor.calls, executor.requests)
	}
	if got, want := executor.requests[0].ApprovalSourceID, "task-review:"+reviewID; got != want {
		t.Fatalf("approval source = %q, want %q", got, want)
	}
	if executor.requests[0].OwnerIdentity != "alice" ||
		executor.requests[0].OriginalRequest != "Delete account data by running a local script" ||
		executor.requests[0].ProjectKey != "018-HAI" {
		t.Fatalf("replayed action differs from reviewed action: %#v", executor.requests[0])
	}

	stored, err := repository.FindReviewItem("alice", reviewID)
	if err != nil {
		t.Fatalf("load completed durable review item: %v", err)
	}
	if stored.Status != "completed" || stored.TaskID != result.Plan.ID {
		t.Fatalf("durable review outcome = %#v, want completed exact replay", stored)
	}
	decisions, err := repository.ListReviewDecisions("alice", reviewID, 50)
	if err != nil {
		t.Fatalf("list durable decisions: %v", err)
	}
	if len(decisions) != 1 ||
		decisions[0].ApprovalSourceID != "task-review:"+reviewID ||
		decisions[0].ResolvedBy != "alice" {
		t.Fatalf("durable approval decisions = %#v", decisions)
	}
}

func TestDurableTaskApprovalRejectsChangedRequestAndSuccessCriteria(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	review := taskStateTestReviewItem("alice", "reviewed-plan", createdAt)
	review.Request.Request = "Run the reviewed local check"
	review.Request.ProjectKey = "018-HAI"
	review.Request.AutomationID = "controlled-runtime"
	review.Request.SuccessCriteria = []string{"the reviewed result is verified"}
	stored, err := repository.CreateReviewItem("alice", review)
	if err != nil {
		t.Fatalf("create durable review item: %v", err)
	}
	resolution, err := repository.ResolveReviewItem("alice", stored.ID, ReviewResolution{
		Decision:   "approved",
		Note:       "Approve exact request and success criteria",
		ResolvedAt: createdAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("approve durable review item: %v", err)
	}

	taskService := &service{stateRepository: repository}
	exactRequest := resolution.Item.Request
	exactRequest.ExecuteAllowed = true
	exactRequest.HumanApproved = true
	exactRequest.ApprovalSourceID = resolution.Decision.ApprovalSourceID
	exactRequest.reviewItemID = resolution.Item.ID
	exactPlan := &CompletionPlan{
		OwnerIdentity: "alice",
		Request:       exactRequest.Request,
		ProjectKey:    exactRequest.ProjectKey,
		RealGoal:      exactRequest.Request,
	}
	if _, err := taskService.verifiedApprovalDecisionForExecution(exactPlan, exactRequest); err != nil {
		t.Fatalf("exact approved request was rejected: %v", err)
	}

	changedRequest := exactRequest
	changedRequest.Request = "Run a different local check"
	changedRequestPlan := *exactPlan
	changedRequestPlan.Request = changedRequest.Request
	if _, err := taskService.verifiedApprovalDecisionForExecution(&changedRequestPlan, changedRequest); err == nil ||
		!strings.Contains(err.Error(), "no longer matches the approved action") {
		t.Fatalf("changed request replay error = %v, want digest rejection", err)
	}

	changedCriteria := exactRequest
	changedCriteria.SuccessCriteria = []string{"a different completion condition is accepted"}
	if _, err := taskService.verifiedApprovalDecisionForExecution(exactPlan, changedCriteria); err == nil ||
		!strings.Contains(err.Error(), "no longer matches the approved action") {
		t.Fatalf("changed success criteria replay error = %v, want digest rejection", err)
	}

	wrongOwner := exactRequest
	wrongOwner.OwnerIdentity = "bob"
	wrongOwnerPlan := *exactPlan
	wrongOwnerPlan.OwnerIdentity = "bob"
	if _, err := taskService.verifiedApprovalDecisionForExecution(&wrongOwnerPlan, wrongOwner); err == nil {
		t.Fatal("foreign owner reused Alice approval")
	}
}

func TestDurableTaskReviewDecisionAndOutcomeTransitionsAreImmutable(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	review := taskStateTestReviewItem("alice", "reviewed-plan", createdAt)
	if _, err := repository.CreateReviewItem("alice", review); err != nil {
		t.Fatalf("create review item: %v", err)
	}
	first, err := repository.ResolveReviewItem("alice", review.ID, ReviewResolution{
		Decision:   "approved",
		ResolvedAt: createdAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("approve review item: %v", err)
	}
	if _, err := repository.ResolveReviewItem("alice", review.ID, ReviewResolution{
		Decision: "rejected",
	}); !errors.Is(err, ErrTaskReviewAlreadyResolved) {
		t.Fatalf("conflicting decision error = %v, want immutable-decision rejection", err)
	}

	completed, err := repository.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "completed-plan",
		Status:     "completed",
		At:         createdAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("mark approved review completed: %v", err)
	}
	if _, err := repository.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "replacement-plan",
		Status:     "needs_review",
		At:         createdAt.Add(3 * time.Second),
	}); !errors.Is(err, ErrTaskReviewInvalidTransition) {
		t.Fatalf("completed outcome rewrite error = %v, want invalid transition", err)
	}

	decisions, err := repository.ListReviewDecisions("alice", review.ID, 50)
	if err != nil {
		t.Fatalf("list immutable decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ID != first.Decision.ID {
		t.Fatalf("immutable decision history changed: %#v", decisions)
	}
	reloaded, err := repository.FindReviewItem("alice", review.ID)
	if err != nil {
		t.Fatalf("reload completed review: %v", err)
	}
	if reloaded.Status != "completed" ||
		reloaded.TaskID != completed.TaskID ||
		reloaded.TaskID == "replacement-plan" {
		t.Fatalf("completed review outcome was rewritten: %#v", reloaded)
	}
}

func TestDurableTaskServiceSupportsInternalOwnerReviewCompatibility(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	executor := &fakeToolExecutor{result: completedToolResult()}
	taskService := newDurableTaskTestService(t, repository, executor)

	plan, err := taskService.Run(IntakeRequest{
		Request:        "Delete account data by running a local script",
		ProjectKey:     "018-HAI",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("queue internal task review: %v", err)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatal("internal task did not create a review item")
	}

	result, err := taskService.ResolveReviewItem(
		plan.ReviewQueueItem.ID,
		ApprovalDecision{Approved: true, Note: "Approve exact internal action"},
	)
	if err != nil {
		t.Fatalf("resolve internal task review: %v", err)
	}
	if result == nil || result.Plan == nil || executor.calls != 1 {
		t.Fatalf("internal review replay result=%#v calls=%d", result, executor.calls)
	}
	stored, err := repository.FindReviewItem(internalTaskStateOwnerIdentity, plan.ReviewQueueItem.ID)
	if err != nil {
		t.Fatalf("load internal durable review item: %v", err)
	}
	if stored.Status != "completed" ||
		stored.Request.OwnerIdentity != internalTaskStateOwnerIdentity {
		t.Fatalf("internal durable review = %#v", stored)
	}
	decisions, err := repository.ListReviewDecisions(
		internalTaskStateOwnerIdentity,
		plan.ReviewQueueItem.ID,
		50,
	)
	if err != nil {
		t.Fatalf("list internal decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ResolvedBy != internalTaskStateOwnerIdentity {
		t.Fatalf("internal decision provenance = %#v", decisions)
	}
}

func newDurableTaskTestService(
	t *testing.T,
	repository TaskStateRepository,
	executor ToolExecutor,
) Service {
	t.Helper()
	selector := &fakeFrameworkSelector{decision: &frameworkregistry.SelectionDecision{
		ID:                   "durable-task-test-selection",
		LifeDomain:           "system",
		NeedOrCommitment:     "controlled test execution",
		MaximumAutonomyLevel: 10,
		Selected: []frameworkregistry.SelectedFramework{{
			ID: "least-authority", Version: "1.0.0", Name: "Least authority",
		}},
		ConstitutionVersion: 1,
	}}
	return NewServiceWithDependencies(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
		nil,
		selector,
		repository,
	)
}
