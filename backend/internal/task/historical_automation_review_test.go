package task

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func historicalReviewSnapshot(id uuid.UUID) *automation.ReviewConfigurationSnapshot {
	return &automation.ReviewConfigurationSnapshot{
		Version: "automation-review-configuration.v1", AutomationID: id,
		Scope: automation.ApprovalScopeScript, ConfigurationDigest: strings.Repeat("a", 64),
	}
}

type historicalSnapshotExecutor struct {
	fakeToolExecutor
	snapshot        *automation.ReviewConfigurationSnapshot
	inspectionErr   error
	inspectionCalls int
}

func (e *historicalSnapshotExecutor) InspectReviewConfiguration(uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	e.inspectionCalls++
	return e.snapshot, e.inspectionErr
}

func TestHistoricalAutomationReviewSnapshotDurableRoundTrip(t *testing.T) {
	id := uuid.New()
	item := taskStateTestReviewItem("alice", "historical-plan", time.Now().UTC())
	item.Request.AutomationID = id.String()
	item.Request.automationReviewSnapshot = historicalReviewSnapshot(id)
	row, err := reviewItemToModel("alice", item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(row.RequestJSON, `"automationReviewSnapshot"`) ||
		!strings.Contains(row.RequestJSON, item.Request.automationReviewSnapshot.ConfigurationDigest) {
		t.Fatalf("durable request omitted historical fingerprint: %s", row.RequestJSON)
	}
	roundTrip, err := reviewItemFromModel(row, nil)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Request.automationReviewSnapshot == nil ||
		*roundTrip.Request.automationReviewSnapshot != *item.Request.automationReviewSnapshot {
		t.Fatalf("historical snapshot did not survive durable projection: %#v", roundTrip.Request)
	}
	digest, err := ReviewRequestDigest("alice", roundTrip.Request)
	if err != nil || digest != row.RequestDigest {
		t.Fatalf("durable digest = %q, err=%v, want %q", digest, err, row.RequestDigest)
	}
	publicRequest, err := json.Marshal(roundTrip.Request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicRequest), "configurationDigest") || strings.Contains(string(publicRequest), "automationReviewSnapshot") {
		t.Fatalf("internal evidence entered intake JSON contract: %s", publicRequest)
	}
	publicReview, err := json.Marshal(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(publicReview), `"automationConfiguration"`) ||
		!strings.Contains(string(publicReview), item.Request.automationReviewSnapshot.ConfigurationDigest) {
		t.Fatalf("review response omitted non-secret fingerprint: %s", publicReview)
	}
	roundTrip.AutomationConfiguration.ConfigurationDigest = strings.Repeat("f", 64)
	if roundTrip.Request.automationReviewSnapshot.ConfigurationDigest != item.Request.automationReviewSnapshot.ConfigurationDigest {
		t.Fatal("public review projection aliases internal historical evidence")
	}
	changed := roundTrip.Request
	snapshot := *changed.automationReviewSnapshot
	snapshot.ConfigurationDigest = strings.Repeat("b", 64)
	changed.automationReviewSnapshot = &snapshot
	changedDigest, err := ReviewRequestDigest("alice", changed)
	if err != nil || changedDigest == row.RequestDigest {
		t.Fatalf("changed configuration was not bound into review digest: %q, %v", changedDigest, err)
	}
	if _, err := reviewRequestDigestV1("alice", changed); err == nil {
		t.Fatal("legacy digest waived new historical configuration evidence")
	}
	changedJSON, err := encodeStoredReviewRequest(changed)
	if err != nil {
		t.Fatal(err)
	}
	row.RequestJSON = changedJSON
	if _, err := reviewItemFromModel(row, nil); err == nil {
		t.Fatal("changed stored snapshot survived immutable request digest verification")
	}
}

func TestHistoricalAutomationReviewSnapshotCannotComeFromClientJSON(t *testing.T) {
	id := uuid.New()
	snapshot := historicalReviewSnapshot(id)
	payload, err := json.Marshal(map[string]interface{}{
		"request": "Execute action", "automationId": id.String(),
		"automationReviewSnapshot": snapshot, "automationConfiguration": snapshot,
		"ReviewConfiguration": snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	var request IntakeRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	if request.automationReviewSnapshot != nil {
		t.Fatal("client JSON constructed historical evidence")
	}
}

func TestHistoricalAutomationReviewCapturesBeforeDecisionAndPropagatesStoredSnapshot(t *testing.T) {
	id := uuid.New()
	repo := NewMemoryTaskStateRepository()
	executor := &historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}
	svc := &service{stateRepository: repo, toolExecutor: executor}
	item := taskStateTestReviewItem("alice", "historical-plan", time.Now().UTC())
	item.Request.AutomationID = id.String()
	item.Request.automationReviewSnapshot = &automation.ReviewConfigurationSnapshot{ConfigurationDigest: "forged"}
	queued, err := svc.addReviewItem(item)
	if err != nil {
		t.Fatal(err)
	}
	if executor.inspectionCalls != 1 || queued.Request.automationReviewSnapshot == nil {
		t.Fatalf("pre-review capture calls=%d item=%#v", executor.inspectionCalls, queued)
	}
	storedID := uuid.MustParse(queued.ID)
	before := repo.reviews[storedID]
	if !strings.Contains(before.RequestJSON, executor.snapshot.ConfigurationDigest) {
		t.Fatalf("pre-decision store omitted captured fingerprint: %s", before.RequestJSON)
	}
	// Mutating the live inspector after queuing cannot replace historical data.
	executor.snapshot.ConfigurationDigest = strings.Repeat("d", 64)
	resolved, err := repo.ResolveReviewItem("alice", queued.ID, ReviewResolution{Decision: "approved", ResolvedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	request := resolved.Item.Request
	request.reviewItemID = queued.ID
	request.HumanApproved = true
	request.ExecuteAllowed = true
	request.ApprovalSourceID = resolved.Decision.ApprovalSourceID
	plan := &CompletionPlan{OwnerIdentity: "alice", Request: request.Request, RealGoal: "A distinct inferred execution goal", ProjectKey: request.ProjectKey}
	decision, err := svc.verifiedApprovalDecisionForExecution(plan, request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ReviewConfiguration == nil || decision.ReviewConfiguration.ConfigurationDigest != strings.Repeat("a", 64) ||
		decision.ApprovalBindingDigest != before.RequestDigest || decision.Task != plan.RealGoal || executor.inspectionCalls != 1 {
		t.Fatalf("verified handoff refreshed or conflated historical evidence: %#v inspections=%d", decision, executor.inspectionCalls)
	}
	if after := repo.reviews[storedID]; after.RequestJSON != before.RequestJSON || after.RequestDigest != before.RequestDigest {
		t.Fatal("approval resolution rewrote historical evidence")
	}
	now := time.Now().UTC()
	launcher := &fakeAutomationLauncher{
		proof: &automation.ApprovalProof{
			ID: uuid.NewString(), OwnerIdentity: "alice", AutomationID: id,
			ActionDigest: strings.Repeat("c", 64), Scope: automation.ApprovalScopeScript,
			ApprovalSourceID: request.ApprovalSourceID, IssuedAt: now, ExpiresAt: now.Add(time.Minute),
			Nonce: "historical-nonce", Signature: "historical-signature",
		},
		result: &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: "completed", LaunchedAt: now},
	}
	_, err = NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
		OwnerIdentity: "alice", TaskID: "historical-execution", Task: plan.RealGoal,
		OriginalRequest: request.Request, ProjectKey: request.ProjectKey, AutomationID: id.String(),
		ApprovalSourceID: request.ApprovalSourceID, ApprovalBindingDigest: decision.ApprovalBindingDigest,
		approvalDecision: decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if launcher.recordCalls != 1 || launcher.recordRequest.ReviewConfiguration == nil ||
		*launcher.recordRequest.ReviewConfiguration != *decision.ReviewConfiguration ||
		launcher.recordRequest.ReviewConfiguration == decision.ReviewConfiguration {
		t.Fatalf("executor did not propagate a private copy of historical evidence: %#v", launcher.recordRequest)
	}
}

func TestHistoricalAutomationReviewLegacyApprovalCannotAcquireSnapshotAfterDecision(t *testing.T) {
	for _, kind := range []string{"missing", "changed", "old-version"} {
		t.Run(kind, func(t *testing.T) {
			id := uuid.New()
			repo := NewMemoryTaskStateRepository()
			executor := &historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}
			svc := &service{stateRepository: repo, toolExecutor: executor}
			item := taskStateTestReviewItem("alice", "historical-plan", time.Now().UTC())
			item.Request.AutomationID = id.String()
			if kind != "missing" {
				item.Request.automationReviewSnapshot = historicalReviewSnapshot(id)
			}
			queued, err := repo.CreateReviewItem("alice", item)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := repo.ResolveReviewItem("alice", queued.ID, ReviewResolution{Decision: "approved", ResolvedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			request := resolved.Item.Request
			request.reviewItemID = queued.ID
			request.ApprovalSourceID = resolved.Decision.ApprovalSourceID
			if kind == "changed" {
				request.automationReviewSnapshot.ConfigurationDigest = strings.Repeat("b", 64)
			}
			if kind == "old-version" {
				request.automationReviewSnapshot.Version = "automation-review-configuration.v0"
			}
			plan := &CompletionPlan{OwnerIdentity: "alice", Request: request.Request, RealGoal: "Execute reviewed action", ProjectKey: request.ProjectKey}
			if decision, err := svc.verifiedApprovalDecisionForExecution(plan, request); err == nil || decision != nil {
				t.Fatalf("unsafe historical request became authority: %#v, %v", decision, err)
			}
			if executor.inspectionCalls != 0 {
				t.Fatal("approval path derived historical evidence after decision")
			}
		})
	}
}

func TestHistoricalAutomationReviewProductionInspectorFailsClosed(t *testing.T) {
	id := uuid.New()
	for _, launcher := range []automationLauncher{nil, &launchOnlyAutomationLauncher{}} {
		if snapshot, err := NewAutomationToolExecutor(launcher).InspectReviewConfiguration(id); err == nil || snapshot != nil {
			t.Fatalf("missing production inspector returned evidence: %#v, %v", snapshot, err)
		}
	}
	for _, failure := range []string{"unavailable", "missing", "old-version"} {
		t.Run(failure, func(t *testing.T) {
			executor := &historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}
			if failure == "unavailable" {
				executor.inspectionErr = errors.New("configuration unavailable")
			}
			if failure == "missing" {
				executor.snapshot = nil
			}
			if failure == "old-version" {
				executor.snapshot.Version = "automation-review-configuration.v0"
			}
			repo := NewMemoryTaskStateRepository()
			svc := &service{stateRepository: repo, toolExecutor: executor}
			item := taskStateTestReviewItem("alice", "historical-plan", time.Now().UTC())
			item.Request.AutomationID = id.String()
			queued, err := svc.addReviewItem(item)
			if err != nil || len(repo.reviews) != 1 || queued.Request.automationReviewSnapshot != nil || queued.AutomationConfiguration != nil || !strings.Contains(queued.Reason, "execution approval is disabled") {
				t.Fatalf("missing snapshot did not remain inspectable without authority: err=%v review=%#v", err, queued)
			}
			_, err = svc.ResolveReviewItemForOwner("alice", queued.ID, ApprovalDecision{Approved: true})
			if !errors.Is(err, ErrTaskReviewConfigurationUnavailable) || executor.calls != 0 || executor.inspectionCalls != 1 {
				t.Fatalf("missing snapshot acquired authority or refreshed at approval: err=%v executions=%d inspections=%d", err, executor.calls, executor.inspectionCalls)
			}
			stillOpen, err := repo.FindReviewItem("alice", queued.ID)
			if err != nil || stillOpen.Status != "open" || stillOpen.Decision != "" {
				t.Fatalf("blocked approval mutated review: err=%v item=%#v", err, stillOpen)
			}
		})
	}
}

func TestHistoricalAutomationReviewOperationRecoveryCapturesOnlyBeforeFirstCreation(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "new"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			id := uuid.New()
			repo := NewMemoryTaskStateRepository()
			executor := &historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}
			svc := &service{stateRepository: repo, toolExecutor: executor}
			operation := models.TaskOperationRecord{ID: uuid.New(), OwnerIdentity: "alice", Mode: "run", CreatedAt: time.Now().UTC()}
			request := IntakeRequest{OwnerIdentity: "alice", Request: "Execute reviewed action", ProjectKey: "project", AutomationID: id.String(), WorkflowID: "workflow-owned-context"}
			reviewID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("hai-task-operation-review:"+operation.ID.String()))
			if legacy {
				_, err := repo.CreateReviewItem("alice", ReviewQueueItem{
					ID: reviewID.String(), TaskID: "operation:" + operation.ID.String(), Request: request,
					Reason: "Existing operation review", Priority: "high", Status: "needs_review", CreatedAt: operation.CreatedAt,
				})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err := svc.ensureTaskOperationReview(request, operation, "outcome uncertain"); err != nil {
					t.Fatal(err)
				}
				if executor.inspectionCalls != 1 {
					t.Fatalf("initial capture calls=%d, want 1", executor.inspectionCalls)
				}
			}
			before := repo.reviews[reviewID]
			stored, err := repo.FindReviewItem("alice", reviewID.String())
			if err != nil {
				t.Fatal(err)
			}
			if stored.Request.WorkflowID != request.WorkflowID {
				t.Fatal("recovery review discarded workflow ownership")
			}
			if !legacy && (stored.Request.automationReviewSnapshot == nil || stored.Request.automationReviewSnapshot.ConfigurationDigest != strings.Repeat("a", 64)) {
				t.Fatalf("first operation review lacks historical evidence: %#v", stored)
			}
			executor.snapshot.ConfigurationDigest = strings.Repeat("d", 64)
			executor.inspectionErr = errors.New("live inspection must not run on replay")
			calls := executor.inspectionCalls
			if err := svc.ensureTaskOperationReview(request, operation, "a later delivery"); err != nil {
				t.Fatal(err)
			}
			after := repo.reviews[reviewID]
			if after.RequestJSON != before.RequestJSON || after.RequestDigest != before.RequestDigest || executor.inspectionCalls != calls || len(repo.reviews) != 1 {
				t.Fatal("operation review replay refreshed or duplicated historical evidence")
			}
		})
	}
}
