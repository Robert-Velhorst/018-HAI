package task

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryTaskOperationClaimIsOwnerScopedIdempotentAndFenced(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	digest := stringsOfLength("a", 64)
	first, err := repository.ClaimTaskOperation("alice", "source-event:42", digest, "run", "worker:one", now, time.Minute)
	if err != nil {
		t.Fatalf("claim first operation: %v", err)
	}
	if first.Disposition != TaskOperationAcquired || first.Operation.LeaseGeneration != 1 {
		t.Fatalf("first claim = %#v", first)
	}

	active, err := repository.ClaimTaskOperation("alice", "source-event:42", digest, "run", "worker:two", now.Add(10*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("claim active operation: %v", err)
	}
	if active.Disposition != TaskOperationInProgress || active.Operation.ID != first.Operation.ID {
		t.Fatalf("active claim = %#v", active)
	}
	if _, err := repository.ClaimTaskOperation("alice", "source-event:42", stringsOfLength("b", 64), "run", "worker:two", now, time.Minute); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("changed request error = %v, want conflict", err)
	}

	owned, err := repository.CompleteTaskOperation("alice", first.Operation.ID, "worker:two", first.Operation.LeaseGeneration, "plan-1", now)
	if err != nil || owned {
		t.Fatalf("stale worker completed operation = (%v, %v)", owned, err)
	}
	owned, err = repository.CompleteTaskOperation("alice", first.Operation.ID, "worker:one", first.Operation.LeaseGeneration, "plan-1", now.Add(20*time.Second))
	if err != nil || !owned {
		t.Fatalf("lease owner complete = (%v, %v)", owned, err)
	}
	replay, err := repository.ClaimTaskOperation("alice", "source-event:42", digest, "run", "worker:three", now.Add(30*time.Second), time.Minute)
	if err != nil || replay.Disposition != TaskOperationReplay || replay.Operation.TaskPlanID != "plan-1" {
		t.Fatalf("replay claim = (%#v, %v)", replay, err)
	}

	bob, err := repository.ClaimTaskOperation("bob", "source-event:42", digest, "run", "worker:bob", now, time.Minute)
	if err != nil || bob.Disposition != TaskOperationAcquired || bob.Operation.ID == first.Operation.ID {
		t.Fatalf("owner-scoped claim = (%#v, %v)", bob, err)
	}
}

func TestMemoryTaskOperationConcurrentClaimHasOneWinner(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	now := time.Now().UTC()
	digest := stringsOfLength("c", 64)
	results := make(chan TaskOperationClaim, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for _, worker := range []string{"worker:one", "worker:two"} {
		wait.Add(1)
		go func(worker string) {
			defer wait.Done()
			claim, err := repository.ClaimTaskOperation("alice", "concurrent:1", digest, "plan", worker, now, time.Minute)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- claim
		}(worker)
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent claim: %v", err)
	}
	counts := map[string]int{}
	for claim := range results {
		counts[claim.Disposition]++
	}
	if counts[TaskOperationAcquired] != 1 || counts[TaskOperationInProgress] != 1 {
		t.Fatalf("claim dispositions = %#v", counts)
	}
}

func TestMemoryTaskOperationExpiredLeaseFailsClosed(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	now := time.Now().UTC()
	digest := stringsOfLength("d", 64)
	first, err := repository.ClaimTaskOperation("alice", "expired:1", digest, "run", "worker:one", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := repository.ClaimTaskOperation("alice", "expired:1", digest, "run", "worker:two", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Disposition != TaskOperationNeedsReview || recovered.Operation.ID != first.Operation.ID || recovered.Operation.LastError == "" {
		t.Fatalf("expired claim = %#v", recovered)
	}
}

func TestTaskServiceReplaysSameIdempotentPlanAndRejectsChangedInput(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	taskService, ok := newDurableTaskTestService(t, repository, nil).(*service)
	if !ok {
		t.Fatal("durable task test service does not expose the concrete operation boundary")
	}
	request := IntakeRequest{
		OwnerIdentity:  "alice",
		IdempotencyKey: "manual-capture:2026-08-04:1",
		Request:        "Summarize the connected project notes",
		ProjectKey:     "018-HAI",
	}
	first, err := taskService.Plan(request)
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	second, err := taskService.Plan(request)
	if err != nil {
		t.Fatalf("replayed plan: %v", err)
	}
	if second.ID != first.ID || second.OperationID != first.OperationID || second.IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("replayed plan differs: first=%#v second=%#v", first, second)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first plan: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal replayed plan: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("first delivery and durable replay are not byte-identical")
	}
	plans, err := repository.ListCompletionPlans("alice", 50)
	if err != nil || len(plans) != 1 {
		t.Fatalf("durable plans = (%d, %v), want one", len(plans), err)
	}

	changed := request
	changed.Request = "Send the connected project notes"
	if _, err := taskService.Plan(changed); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("changed input error = %v, want conflict", err)
	}
}

func TestTaskOperationIdempotencyBindsStructuredDeadline(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	taskService, ok := newDurableTaskTestService(t, repository, nil).(*service)
	if !ok {
		t.Fatal("durable task test service does not expose the concrete operation boundary")
	}
	firstDeadline := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	request := IntakeRequest{
		OwnerIdentity:  "alice",
		IdempotencyKey: "manual-capture:deadline-binding",
		Request:        "Prepare a project action plan",
		ProjectKey:     "018-HAI",
		Deadline:       &firstDeadline,
	}
	if _, err := taskService.Plan(request); err != nil {
		t.Fatalf("first plan: %v", err)
	}

	changedDeadline := firstDeadline.Add(24 * time.Hour)
	request.Deadline = &changedDeadline
	if _, err := taskService.Plan(request); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("same idempotency key with a different deadline = %v, want conflict", err)
	}
}

func TestTaskOperationFailureCreatesOneOwnerReviewBeforeRetry(t *testing.T) {
	repository := NewMemoryTaskStateRepository()
	taskService, ok := newDurableTaskTestService(t, repository, nil).(*service)
	if !ok {
		t.Fatal("durable task test service does not expose the concrete operation boundary")
	}
	request := IntakeRequest{
		OwnerIdentity:  "alice",
		IdempotencyKey: "source-event:uncertain-1",
		Request:        "Prepare a safe summary without contacting anyone",
		ProjectKey:     "018-HAI",
	}
	operationFailure := errors.New("worker response was lost after an uncertain attempt")
	_, err := taskService.withTaskOperation(request, "run", func(IntakeRequest) (*CompletionPlan, error) {
		return nil, operationFailure
	})
	if !errors.Is(err, operationFailure) {
		t.Fatalf("first operation error = %v", err)
	}

	reviews, err := repository.ListReviewItems("alice", 50)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("operation reviews = (%d, %v), want one", len(reviews), err)
	}
	if reviews[0].Status != "needs_review" || reviews[0].Priority != "high" ||
		!strings.HasPrefix(reviews[0].TaskID, "operation:") ||
		!strings.Contains(reviews[0].Reason, "never resumes or rewrites") {
		t.Fatalf("uncertain operation review = %#v", reviews[0])
	}
	if reviews[0].Request.IdempotencyKey != "" || reviews[0].Request.ExecuteAllowed || reviews[0].Request.HumanApproved {
		t.Fatalf("review retained transient execution authority: %#v", reviews[0].Request)
	}

	if _, err := taskService.withTaskOperation(request, "run", func(IntakeRequest) (*CompletionPlan, error) {
		t.Fatal("uncertain operation was executed again before review")
		return nil, nil
	}); !errors.Is(err, ErrTaskOperationNeedsReview) {
		t.Fatalf("second operation error = %v, want needs review", err)
	}
	reviews, err = repository.ListReviewItems("alice", 50)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("idempotent operation reviews = (%d, %v), want one", len(reviews), err)
	}
	if otherOwner, err := repository.ListReviewItems("bob", 50); err != nil || len(otherOwner) != 0 {
		t.Fatalf("operation review leaked across owners: (%#v, %v)", otherOwner, err)
	}
	priorOperationID := strings.TrimPrefix(reviews[0].TaskID, "operation:")
	if resolution, err := taskService.ResolveReviewItemForOwner("alice", reviews[0].ID, ApprovalDecision{
		Approved: true,
		Note:     "Operator checked the prior audit.",
	}); !errors.Is(err, ErrTaskOperationRetryConfirmation) || resolution != nil {
		t.Fatalf("operation retry without confirmation = (%#v, %v)", resolution, err)
	}
	reviewsAfterDeniedRetry, err := repository.ListReviewItems("alice", 50)
	if err != nil || len(reviewsAfterDeniedRetry) != 1 || reviewsAfterDeniedRetry[0].Status != "needs_review" {
		t.Fatalf("denied retry mutated review = (%#v, %v)", reviewsAfterDeniedRetry, err)
	}
	resolution, err := taskService.ResolveReviewItemForOwner("alice", reviews[0].ID, ApprovalDecision{
		Approved:     true,
		Note:         "Operator checked the prior audit and authorized a separate attempt.",
		Confirmation: TaskOperationRetryConfirmation,
	})
	if err != nil {
		t.Fatalf("approve operation review: %v", err)
	}
	if resolution.Plan == nil || resolution.Plan.OperationID == "" || resolution.Plan.OperationID == priorOperationID {
		t.Fatalf("operation review did not create a separately identified attempt: %#v", resolution)
	}
}

func stringsOfLength(value string, count int) string {
	result := ""
	for len(result) < count {
		result += value
	}
	return result[:count]
}

func TestTaskOperationIdentifierValidation(t *testing.T) {
	for _, value := range []string{"", "contains space", "unsafe/segment", stringsOfLength("x", 121)} {
		if validTaskOperationIdentifier(value, 120) {
			t.Fatalf("identifier %q should be rejected", value)
		}
	}
	for _, value := range []string{uuid.NewString(), "source:event_1.retry-2"} {
		if !validTaskOperationIdentifier(value, 120) {
			t.Fatalf("identifier %q should be accepted", value)
		}
	}
}
