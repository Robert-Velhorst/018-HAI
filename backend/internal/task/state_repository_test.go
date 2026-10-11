package task

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryTaskStateRepositoryCompletionPlansAreOwnerScopedRedactedAndOrdered(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	base := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	older := taskStateTestPlan("older-plan", "alice", base)
	newer := taskStateTestPlan("newer-plan", "alice", base.Add(time.Minute))
	newer.Request = "Review api_key=completion-secret before execution"
	newer.ExecutionResult = &ExecutionResult{
		VerificationStatus: "verified",
		Output:             "Bearer completion-output-secret",
	}
	newer.CompletionStatus = "validated"
	if err := repo.AppendCompletionPlan("alice", older); err != nil {
		t.Fatalf("append older plan: %v", err)
	}
	if err := repo.AppendCompletionPlan("alice", newer); err != nil {
		t.Fatalf("append newer plan: %v", err)
	}
	if err := repo.AppendCompletionPlan("alice", newer); err != nil {
		t.Fatalf("idempotent completion append: %v", err)
	}
	if err := repo.AppendCompletionPlan("bob", taskStateTestPlan("bob-plan", "bob", base.Add(2*time.Minute))); err != nil {
		t.Fatalf("append Bob plan: %v", err)
	}

	alice, err := repo.ListCompletionPlans("alice", 50)
	if err != nil {
		t.Fatalf("list Alice plans: %v", err)
	}
	if len(alice) != 2 || alice[0].ID != "newer-plan" || alice[1].ID != "older-plan" {
		t.Fatalf("Alice plan order = %#v", taskStatePlanIDs(alice))
	}
	encoded := fmt.Sprintf("%#v", alice[0])
	if strings.Contains(encoded, "completion-secret") || strings.Contains(encoded, "completion-output-secret") {
		t.Fatalf("completion history retained a secret: %s", encoded)
	}
	if alice[0].OwnerIdentity != "alice" || alice[0].ExecutionResult.VerificationStatus != "verified" {
		t.Fatalf("completion projection lost owner or verification: %#v", alice[0])
	}
	bob, err := repo.ListCompletionPlans("bob", 50)
	if err != nil || len(bob) != 1 || bob[0].ID != "bob-plan" {
		t.Fatalf("Bob plans = %#v, %v", bob, err)
	}
	found, err := repo.FindCompletionPlan("alice", "older-plan")
	if err != nil || found.ID != "older-plan" {
		t.Fatalf("find completion plan = %#v, %v", found, err)
	}
	if _, err := repo.FindCompletionPlan("bob", "older-plan"); !errors.Is(err, ErrTaskStateNotFound) {
		t.Fatalf("foreign owner find error = %v, want not found", err)
	}
	alice[0].Request = "caller mutation"
	again, err := repo.FindCompletionPlan("alice", "newer-plan")
	if err != nil || again.Request == "caller mutation" {
		t.Fatalf("stored completion changed through caller-owned projection: %#v, %v", again, err)
	}
}

func TestMemoryTaskStateRepositoryRejectsMalformedOrTamperedCompletionPayloads(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	row, err := completionPlanToModel("alice", taskStateTestPlan("malformed-plan", "alice", time.Now().UTC()))
	if err != nil {
		t.Fatalf("build completion row: %v", err)
	}
	row.PayloadJSON = `{"unexpected":true}`
	sum := sha256.Sum256([]byte(row.PayloadJSON))
	row.PayloadDigest = fmt.Sprintf("%x", sum[:])
	repo.completions = append(repo.completions, row)
	if _, err := repo.ListCompletionPlans("alice", 50); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("structurally malformed completion error = %v", err)
	}

	repo.completions = nil
	row.PayloadJSON = `{"id":`
	sum = sha256.Sum256([]byte(row.PayloadJSON))
	row.PayloadDigest = fmt.Sprintf("%x", sum[:])
	repo.completions = append(repo.completions, row)
	if _, err := repo.ListCompletionPlans("alice", 50); err == nil || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("syntactically malformed completion error = %v", err)
	}

	repo.completions = nil
	row, err = completionPlanToModel("alice", taskStateTestPlan("tampered-plan", "alice", time.Now().UTC()))
	if err != nil {
		t.Fatalf("build tampered row: %v", err)
	}
	row.PayloadDigest = strings.Repeat("0", 64)
	repo.completions = append(repo.completions, row)
	if _, err := repo.ListCompletionPlans("alice", 50); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered completion error = %v", err)
	}

	repo.completions = nil
	row, err = completionPlanToModel("alice", taskStateTestPlan("duplicate-json-plan", "alice", time.Now().UTC()))
	if err != nil {
		t.Fatalf("build duplicate-key row: %v", err)
	}
	row.PayloadJSON = strings.Replace(row.PayloadJSON, `"id":"duplicate-json-plan"`, `"id":"duplicate-json-plan","id":"duplicate-json-plan"`, 1)
	canonical, err := canonicalTaskStateJSONObject(strings.Replace(row.PayloadJSON, `,"id":"duplicate-json-plan"`, "", 1))
	if err != nil {
		t.Fatalf("canonical duplicate-key baseline: %v", err)
	}
	sum = sha256.Sum256(canonical)
	row.PayloadDigest = fmt.Sprintf("%x", sum[:])
	repo.completions = append(repo.completions, row)
	if _, err := repo.ListCompletionPlans("alice", 50); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate-key completion error = %v", err)
	}
}

func TestMemoryTaskStateRepositoryReviewDecisionLifecycleIsDurableAndBound(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	createdAt := time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	review := taskStateTestReviewItem("alice", "initial-plan", createdAt)
	review.Request.Request = "Deploy after review with token=review-secret"
	stored, err := repo.CreateReviewItem("alice", review)
	if err != nil {
		t.Fatalf("create review item: %v", err)
	}
	if stored.Status != "open" || strings.Contains(stored.Request.Request, "review-secret") {
		t.Fatalf("unsafe stored review item: %#v", stored)
	}
	if _, err := repo.FindReviewItem("bob", review.ID); !errors.Is(err, ErrTaskStateNotFound) {
		t.Fatalf("foreign owner find error = %v, want not found", err)
	}
	if _, err := repo.ResolveReviewItem("bob", review.ID, ReviewResolution{Decision: "approved"}); !errors.Is(err, ErrTaskStateNotFound) {
		t.Fatalf("foreign owner resolve error = %v, want not found", err)
	}

	resolvedAt := createdAt.Add(time.Minute)
	resolution, err := repo.ResolveReviewItem("alice", review.ID, ReviewResolution{
		Decision:   "approved",
		Note:       "Owner approved password=decision-secret",
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		t.Fatalf("approve review item: %v", err)
	}
	if resolution.Item.Status != "approved" ||
		resolution.Decision.ApprovalSource != taskReviewApprovalSource ||
		resolution.Decision.ApprovalSourceID != "task-review:"+review.ID ||
		resolution.Decision.ResolvedBy != "alice" ||
		resolution.Decision.RequestDigest == "" ||
		strings.Contains(resolution.Decision.ResolutionNote, "decision-secret") {
		t.Fatalf("approval provenance = %#v", resolution)
	}
	approved, err := repo.FindApprovedReviewDecision("alice", review.ID)
	if err != nil || approved.ID != resolution.Decision.ID {
		t.Fatalf("find approved decision = %#v, %v", approved, err)
	}
	if retriedResolution, err := repo.ResolveReviewItem("alice", review.ID, ReviewResolution{Decision: "approved"}); !errors.Is(err, ErrTaskReviewAlreadyResolved) || retriedResolution != nil {
		t.Fatalf("replayed approval = %#v, %v; want nil, already resolved", retriedResolution, err)
	}
	if _, err := repo.ResolveReviewItem("alice", review.ID, ReviewResolution{Decision: "rejected"}); !errors.Is(err, ErrTaskReviewAlreadyResolved) {
		t.Fatalf("conflicting duplicate resolve error = %v", err)
	}

	reviewedAgain, err := repo.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "retry-plan",
		Status:     "needs_review",
		Reason:     "postcondition failed",
		At:         createdAt.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("mark needs review: %v", err)
	}
	if reviewedAgain.Status != "needs_review" || reviewedAgain.ResolvedAt != nil || reviewedAgain.Decision != "" {
		t.Fatalf("needs-review projection = %#v", reviewedAgain)
	}
	idempotentReview, err := repo.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "retry-plan",
		Status:     "needs_review",
	})
	if err != nil || idempotentReview.Status != "needs_review" {
		t.Fatalf("idempotent needs-review outcome = %#v, %v", idempotentReview, err)
	}
	if _, err := repo.FindApprovedReviewDecision("alice", review.ID); !errors.Is(err, ErrTaskStateNotFound) {
		t.Fatalf("inactive approval lookup error = %v, want not found", err)
	}

	second, err := repo.ResolveReviewItem("alice", review.ID, ReviewResolution{
		Decision:   "approved",
		Note:       "Approve corrected attempt",
		ResolvedAt: createdAt.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("approve corrected attempt: %v", err)
	}
	if second.Decision.ID == resolution.Decision.ID {
		t.Fatal("second resolution reused immutable decision id")
	}
	if second.Decision.ReviewRevision != 2 {
		t.Fatalf("second review revision = %d, want 2", second.Decision.ReviewRevision)
	}
	completed, err := repo.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "completed-plan",
		Status:     "completed",
		At:         createdAt.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	if completed.Status != "completed" || completed.TaskID != "completed-plan" || completed.ResolvedAt == nil {
		t.Fatalf("completed review item = %#v", completed)
	}
	idempotentCompletion, err := repo.MarkReviewOutcome("alice", review.ID, ReviewOutcome{
		TaskPlanID: "completed-plan",
		Status:     "completed",
	})
	if err != nil || idempotentCompletion.Status != "completed" {
		t.Fatalf("idempotent completed outcome = %#v, %v", idempotentCompletion, err)
	}
	decisions, err := repo.ListReviewDecisions("alice", review.ID, 50)
	if err != nil {
		t.Fatalf("list review decisions: %v", err)
	}
	if len(decisions) != 2 || decisions[0].ID != second.Decision.ID || decisions[1].ID != resolution.Decision.ID {
		t.Fatalf("decision history order = %#v", decisions)
	}
}

func TestMemoryTaskStateRepositoryReviewItemsAreDeterministicAndRejectMalformedJSON(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	base := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	older := taskStateTestReviewItem("alice", "older-plan", base)
	newer := taskStateTestReviewItem("alice", "newer-plan", base.Add(time.Minute))
	if _, err := repo.CreateReviewItem("alice", older); err != nil {
		t.Fatalf("create older review: %v", err)
	}
	if _, err := repo.CreateReviewItem("alice", newer); err != nil {
		t.Fatalf("create newer review: %v", err)
	}
	items, err := repo.ListReviewItems("alice", 50)
	if err != nil || len(items) != 2 || items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Fatalf("review item order = %#v, %v", items, err)
	}

	id, _ := uuid.Parse(newer.ID)
	row := repo.reviews[id]
	row.RequestJSON = `{"unexpected":true}`
	repo.reviews[id] = row
	if _, err := repo.ListReviewItems("alice", 50); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("malformed review request error = %v", err)
	}
}

func TestReviewItemAcceptsOnlyExactLegacyDigestWithoutNewProvenance(t *testing.T) {
	item := taskStateTestReviewItem("alice", "legacy-plan", time.Now().UTC())
	row, err := reviewItemToModel("alice", item)
	if err != nil {
		t.Fatalf("serialize review item: %v", err)
	}
	legacyDigest, err := reviewRequestDigestV1("alice", item.Request)
	if err != nil {
		t.Fatalf("create legacy digest: %v", err)
	}
	row.RequestDigest = legacyDigest
	if _, err := reviewItemFromModel(row, nil); err != nil {
		t.Fatalf("read exact legacy review item: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(row.RequestJSON), &payload); err != nil {
		t.Fatalf("decode review request: %v", err)
	}
	payload["mandateId"] = "new-action-provenance"
	mutated, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode mutated review request: %v", err)
	}
	row.RequestJSON = string(mutated)
	if _, err := reviewItemFromModel(row, nil); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("legacy digest accepted new provenance: %v", err)
	}
}

func TestDurableReviewQueueIgnoresUnreadableResolvedHistory(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	base := time.Now().UTC().Add(-time.Minute)
	pending := taskStateTestReviewItem("alice", "pending-plan", base)
	resolved := taskStateTestReviewItem("alice", "resolved-plan", base.Add(time.Second))
	if _, err := repo.CreateReviewItem("alice", pending); err != nil {
		t.Fatalf("create pending review: %v", err)
	}
	if _, err := repo.CreateReviewItem("alice", resolved); err != nil {
		t.Fatalf("create resolved review: %v", err)
	}
	if _, err := repo.ResolveReviewItem("alice", resolved.ID, ReviewResolution{
		Decision:   "rejected",
		ResolvedAt: base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("resolve historical review: %v", err)
	}
	resolvedID, _ := uuid.Parse(resolved.ID)
	row := repo.reviews[resolvedID]
	row.RequestJSON = `{"unreadableLegacyHistory":true}`
	repo.reviews[resolvedID] = row

	taskService := &service{stateRepository: repo}
	items, err := taskService.ReviewQueueForOwnerWithError("alice")
	if err != nil {
		t.Fatalf("read pending review queue: %v", err)
	}
	if len(items) != 1 || items[0].ID != pending.ID {
		t.Fatalf("pending review queue = %#v, want only %s", items, pending.ID)
	}
}

func TestMemoryTaskStateRepositoryRejectsForgedApprovalStateAndOwnerSpoofing(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	item := taskStateTestReviewItem("alice", "plan", time.Now().UTC())
	item.Request.OwnerIdentity = "bob"
	if _, err := repo.CreateReviewItem("alice", item); !errors.Is(err, ErrTaskReviewBindingMismatch) {
		t.Fatalf("spoofed request owner error = %v", err)
	}

	item.Request.OwnerIdentity = "alice"
	if _, err := repo.CreateReviewItem("alice", item); err != nil {
		t.Fatalf("create review: %v", err)
	}
	id, _ := uuid.Parse(item.ID)
	row := repo.reviews[id]
	row.RequestJSON = strings.TrimSuffix(row.RequestJSON, "}") + `,"humanApproved":true,"executeAllowed":true}`
	repo.reviews[id] = row
	if _, err := repo.FindReviewItem("alice", item.ID); err == nil || !strings.Contains(err.Error(), "transient approval state") {
		t.Fatalf("forged approval state error = %v", err)
	}

	foreign := taskStateTestReviewItem("bob", "foreign-plan", item.CreatedAt)
	foreign.ID = item.ID
	if _, err := repo.CreateReviewItem("bob", foreign); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("foreign duplicate UUID error = %v, want conflict", err)
	}
}

func TestMemoryTaskStateRepositoryCreateIsIdempotentAndResolveIsOneShot(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	item := taskStateTestReviewItem("alice", "plan", base)

	const workers = 24
	var createWG sync.WaitGroup
	createErrors := make(chan error, workers)
	for index := 0; index < workers; index++ {
		createWG.Add(1)
		go func() {
			defer createWG.Done()
			_, err := repo.CreateReviewItem("alice", item)
			createErrors <- err
		}()
	}
	createWG.Wait()
	close(createErrors)
	for err := range createErrors {
		if err != nil {
			t.Fatalf("concurrent idempotent create: %v", err)
		}
	}
	if len(repo.reviews) != 1 {
		t.Fatalf("stored review count = %d, want 1", len(repo.reviews))
	}

	resolvedAt := base.Add(time.Second)
	var resolveWG sync.WaitGroup
	decisionIDs := make(chan string, workers)
	resolveErrors := make(chan error, workers)
	for index := 0; index < workers; index++ {
		resolveWG.Add(1)
		go func() {
			defer resolveWG.Done()
			result, err := repo.ResolveReviewItem("alice", item.ID, ReviewResolution{
				Decision:   "approved",
				Note:       "same case approval",
				ResolvedAt: resolvedAt,
			})
			if result != nil {
				decisionIDs <- result.Decision.ID
			}
			resolveErrors <- err
		}()
	}
	resolveWG.Wait()
	close(decisionIDs)
	close(resolveErrors)
	resolveSuccesses := 0
	for err := range resolveErrors {
		if err == nil {
			resolveSuccesses++
			continue
		}
		if !errors.Is(err, ErrTaskReviewAlreadyResolved) {
			t.Fatalf("concurrent resolve error: %v", err)
		}
	}
	var expectedDecisionID string
	for id := range decisionIDs {
		if expectedDecisionID == "" {
			expectedDecisionID = id
		}
		if id != expectedDecisionID {
			t.Fatalf("concurrent resolve returned decision %s, want %s", id, expectedDecisionID)
		}
	}
	if len(repo.decisions) != 1 {
		t.Fatalf("immutable decision count = %d, want 1", len(repo.decisions))
	}
	if resolveSuccesses != 1 {
		t.Fatalf("successful concurrent resolves = %d, want exactly 1", resolveSuccesses)
	}

	var outcomeWG sync.WaitGroup
	outcomeErrors := make(chan error, workers)
	for index := 0; index < workers; index++ {
		outcomeWG.Add(1)
		go func() {
			defer outcomeWG.Done()
			_, err := repo.MarkReviewOutcome("alice", item.ID, ReviewOutcome{
				TaskPlanID: "completed-plan",
				Status:     "completed",
				At:         base.Add(2 * time.Second),
			})
			outcomeErrors <- err
		}()
	}
	outcomeWG.Wait()
	close(outcomeErrors)
	for err := range outcomeErrors {
		if err != nil {
			t.Fatalf("concurrent idempotent outcome: %v", err)
		}
	}
}

func TestMemoryTaskStateRepositorySerializesConflictingConcurrentDecisions(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	item := taskStateTestReviewItem("alice", "plan", base)
	if _, err := repo.CreateReviewItem("alice", item); err != nil {
		t.Fatalf("create review: %v", err)
	}

	start := make(chan struct{})
	type result struct {
		decision string
		err      error
	}
	results := make(chan result, 2)
	for _, decision := range []string{"approved", "rejected"} {
		decision := decision
		go func() {
			<-start
			_, err := repo.ResolveReviewItem("alice", item.ID, ReviewResolution{
				Decision:   decision,
				ResolvedAt: base.Add(time.Second),
			})
			results <- result{decision: decision, err: err}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	successes := 0
	for _, current := range []result{first, second} {
		if current.err == nil {
			successes++
			continue
		}
		if !errors.Is(current.err, ErrTaskReviewAlreadyResolved) {
			t.Fatalf("%s resolution error = %v", current.decision, current.err)
		}
	}
	if successes != 1 || len(repo.decisions) != 1 {
		t.Fatalf("conflicting resolution successes=%d decisions=%d, want 1 and 1", successes, len(repo.decisions))
	}
}

func TestReviewRequestDigestExcludesTransientApprovalStateAndSecrets(t *testing.T) {
	request := IntakeRequest{
		OwnerIdentity:   "alice",
		PursuitID:       "pursuit-1",
		WorkflowID:      "workflow-1",
		Request:         "Run with api_key=first-secret",
		ProjectKey:      "project",
		AutomationID:    "automation",
		SuccessCriteria: []string{"result verified"},
	}
	first, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	request.ExecuteAllowed = true
	request.HumanApproved = true
	request.ApprovalNote = "approved"
	request.ApprovalSourceID = "task-review:any"
	request.Request = "Run with api_key=second-secret"
	second, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first != second {
		t.Fatalf("transient approval state or secret value changed request digest: %s != %s", first, second)
	}
	request.ProjectKey = "different-project"
	third, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatalf("third digest: %v", err)
	}
	if third == first {
		t.Fatal("action-defining project change did not change request digest")
	}
	request.ProjectKey = "project"
	request.MandateID = uuid.NewString()
	fourth, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatalf("fourth digest: %v", err)
	}
	if fourth == first {
		t.Fatal("action-defining standing mandate change did not change request digest")
	}
	request.MandateID = ""
	request.CoordinationPlan = taskPlanReference()
	fifth, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatalf("fifth digest: %v", err)
	}
	if fifth == first {
		t.Fatal("accepted coordination revision did not change request digest")
	}
}

func TestStoredReviewRequestPreservesPrivateWorkflowIdentity(t *testing.T) {
	item := taskStateTestReviewItem("alice", "workflow-plan", time.Now().UTC())
	item.Request.PursuitID = "pursuit-1"
	item.Request.WorkflowID = "workflow-1"
	item.Request.MandateID = uuid.NewString()
	item.Request.CoordinationPlan = taskPlanReference()

	row, err := reviewItemToModel("alice", item)
	if err != nil {
		t.Fatalf("serialize workflow review item: %v", err)
	}
	if !strings.Contains(row.RequestJSON, `"workflowId":"workflow-1"`) {
		t.Fatalf("durable request omitted private workflow identity: %s", row.RequestJSON)
	}
	if !strings.Contains(row.RequestJSON, `"mandateId":"`+item.Request.MandateID+`"`) {
		t.Fatalf("durable request omitted mandate identity: %s", row.RequestJSON)
	}

	roundTrip, err := reviewItemFromModel(row, nil)
	if err != nil {
		t.Fatalf("decode workflow review item: %v", err)
	}
	if roundTrip.Request.WorkflowID != "workflow-1" ||
		roundTrip.Request.PursuitID != "pursuit-1" ||
		roundTrip.Request.MandateID != item.Request.MandateID ||
		roundTrip.Request.CoordinationPlan != item.Request.CoordinationPlan {
		t.Fatalf("workflow identity did not round trip: %#v", roundTrip.Request)
	}
	digest, err := ReviewRequestDigest("alice", roundTrip.Request)
	if err != nil {
		t.Fatalf("digest round-trip request: %v", err)
	}
	if digest != row.RequestDigest {
		t.Fatalf("workflow request digest drifted: %s != %s", digest, row.RequestDigest)
	}

	publicPayload, err := json.Marshal(roundTrip.Request)
	if err != nil {
		t.Fatalf("marshal public intake request: %v", err)
	}
	if strings.Contains(string(publicPayload), "workflowId") {
		t.Fatalf("private workflow identity leaked into public request JSON: %s", publicPayload)
	}
}

func TestTaskStateJSONDigestIsStableAcrossObjectOrderingAndRejectsAmbiguity(t *testing.T) {
	firstPayload := `{"short":1,"longer":{"b":2,"a":1}}`
	canonical, err := canonicalTaskStateJSONObject(firstPayload)
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	sum := sha256.Sum256(canonical)
	digest := fmt.Sprintf("%x", sum[:])
	reordered := `{"longer":{"a":1,"b":2},"short":1}`
	if err := validateStoredDigest(reordered, digest); err != nil {
		t.Fatalf("semantically identical JSONB payload rejected: %v", err)
	}
	if _, err := canonicalTaskStateJSONObject(`{"a":1,"a":1}`); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate root key error = %v", err)
	}
	if _, err := canonicalTaskStateJSONObject(`{"nested":{"a":1,"a":1}}`); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate nested key error = %v", err)
	}
}

func TestTaskStatePayloadsAndMetadataAreBounded(t *testing.T) {
	plan := taskStateTestPlan("large-plan", "alice", time.Now().UTC())
	plan.Request = strings.Repeat("x", taskStateMaximumPayloadSize+1)
	if _, err := completionPlanToModel("alice", plan); err == nil || !strings.Contains(err.Error(), "payload exceeds") {
		t.Fatalf("oversized completion payload error = %v", err)
	}

	plan = taskStateTestPlan("bounded-plan", "alice", time.Now().UTC())
	plan.Request = strings.Repeat("x", taskStateMaximumStringRunes+100)
	row, err := completionPlanToModel("alice", plan)
	if err != nil {
		t.Fatalf("serialize bounded completion payload: %v", err)
	}
	decoded, err := completionPlanFromModel(row)
	if err != nil {
		t.Fatalf("decode bounded completion payload: %v", err)
	}
	if len([]rune(decoded.Request)) != taskStateMaximumStringRunes {
		t.Fatalf("bounded request length = %d, want %d", len([]rune(decoded.Request)), taskStateMaximumStringRunes)
	}

	item := taskStateTestReviewItem("alice", "plan", time.Now().UTC())
	item.Reason = strings.Repeat("r", taskStateMaximumReasonRunes+100)
	reviewRow, err := reviewItemToModel("alice", item)
	if err != nil {
		t.Fatalf("serialize bounded review item: %v", err)
	}
	if len([]rune(reviewRow.Reason)) != taskStateMaximumReasonRunes {
		t.Fatalf("bounded reason length = %d, want %d", len([]rune(reviewRow.Reason)), taskStateMaximumReasonRunes)
	}
}

func TestMemoryTaskStateRepositoryRejectsCompletionWithoutActiveApproval(t *testing.T) {
	repo := NewMemoryTaskStateRepository()
	item := taskStateTestReviewItem("alice", "plan", time.Now().UTC())
	if _, err := repo.CreateReviewItem("alice", item); err != nil {
		t.Fatalf("create review: %v", err)
	}
	if _, err := repo.MarkReviewOutcome("alice", item.ID, ReviewOutcome{
		TaskPlanID: "plan",
		Status:     "completed",
	}); !errors.Is(err, ErrTaskReviewInvalidTransition) {
		t.Fatalf("completion without approval error = %v", err)
	}
}

func taskStateTestPlan(id, owner string, createdAt time.Time) CompletionPlan {
	return CompletionPlan{
		ID:               id,
		OwnerIdentity:    owner,
		CreatedAt:        createdAt,
		Request:          "Prepare a source-grounded result",
		RealGoal:         "Produce a verified result",
		ValidationResult: ValidationResult{Status: "not_run"},
		CompletionStatus: "planned",
	}
}

func taskStateTestReviewItem(owner, taskPlanID string, createdAt time.Time) ReviewQueueItem {
	return ReviewQueueItem{
		ID:     uuid.NewString(),
		TaskID: taskPlanID,
		Request: IntakeRequest{
			OwnerIdentity: owner,
			Request:       "Execute the reviewed action",
			ProjectKey:    "project",
			AutomationID:  "automation",
		},
		Reason:    "human approval is required",
		Priority:  "high",
		Status:    "open",
		CreatedAt: createdAt,
	}
}

func taskStatePlanIDs(plans []CompletionPlan) []string {
	result := make([]string, 0, len(plans))
	for _, plan := range plans {
		result = append(result, plan.ID)
	}
	return result
}
