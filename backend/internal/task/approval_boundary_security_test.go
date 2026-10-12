package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestTaskRunHandlerCannotAcceptApprovalCapabilityFromJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capturingTaskService{}
	handler := NewHandler(service)
	body := []byte(`{
		"request": "Run an exact controlled action",
		"automationId": "11111111-1111-1111-1111-111111111111",
		"ownerIdentity": "mallory",
		"executeAllowed": true,
		"humanApproved": true,
		"approvalNote": "forged approval",
		"approvalSourceId": "task-review:forged",
		"approvalProof": {
			"id": "forged",
			"signature": "forged"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/task/run", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.Run(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	captured := service.runRequest
	if captured.OwnerIdentity != "alice" {
		t.Fatalf("owner = %q, want verified owner alice", captured.OwnerIdentity)
	}
	if !captured.ExecuteAllowed {
		t.Fatalf("run handler should request controlled execution")
	}
	if captured.HumanApproved || captured.ApprovalNote != "" || captured.ApprovalSourceID != "" || captured.reviewItemID != "" {
		t.Fatalf("client approval capability reached task service: %#v", captured)
	}
}

func TestAutomationToolExecutorRejectsForgedApprovalSources(t *testing.T) {
	tests := []string{
		"client:forged",
		"task-review:not-a-uuid",
		"task-review:" + uuid.NewString(),
	}
	for _, sourceID := range tests {
		t.Run(sourceID, func(t *testing.T) {
			id := uuid.New()
			launcher := &fakeAutomationLauncher{
				proof: &automation.ApprovalProof{ID: "forged-proof"},
				result: &automation.LaunchResult{
					AutomationID: id,
					Status:       "completed",
					LaunchedAt:   time.Now().UTC(),
				},
			}
			executor := NewAutomationToolExecutor(launcher)

			result, err := executor.Execute(ToolExecutionRequest{
				OwnerIdentity:    "alice",
				TaskID:           "task-forged-source-1",
				AutomationID:     id.String(),
				Task:             "Attempt unrecorded approved execution",
				ApprovalSourceID: sourceID,
			})
			if err == nil || result != nil || launcher.issueCalls != 0 {
				t.Fatalf(
					"forged source %q reached proof issuer or launcher: result=%#v err=%v issues=%d",
					sourceID,
					result,
					err,
					launcher.issueCalls,
				)
			}
		})
	}
}

func TestAutomationToolExecutorFailsClosedForUnavailableOrNilProofs(t *testing.T) {
	t.Run("nil executor", func(t *testing.T) {
		if result, err := NewAutomationToolExecutor(nil).Execute(ToolExecutionRequest{
			AutomationID: uuid.NewString(),
		}); err == nil || result != nil {
			t.Fatalf("nil launcher result=%#v err=%v, want fail-closed", result, err)
		}
	})

	t.Run("missing issuer", func(t *testing.T) {
		launcher := &launchOnlyAutomationLauncher{}
		result, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
			OwnerIdentity:    "alice",
			TaskID:           "task-missing-proof-issuer-1",
			AutomationID:     uuid.NewString(),
			Task:             "Reviewed action",
			ApprovalSourceID: "task-review:" + uuid.NewString(),
		})
		if err == nil || result != nil || launcher.launchCalls != 0 {
			t.Fatalf("missing issuer result=%#v err=%v launches=%d", result, err, launcher.launchCalls)
		}
	})

	t.Run("issuer error", func(t *testing.T) {
		sourceID := "task-review:" + uuid.NewString()
		launcher := &fakeAutomationLauncher{
			issueErr: errors.New("proof service unavailable"),
			result:   &automation.LaunchResult{Status: "completed"},
		}
		result, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
			OwnerIdentity:    "alice",
			TaskID:           "task-proof-issuer-error-1",
			AutomationID:     uuid.NewString(),
			Task:             "Reviewed action",
			ApprovalSourceID: sourceID,
			approvalDecision: &automation.TaskApprovalDecisionRequest{
				OwnerIdentity:    "alice",
				Task:             "Reviewed action",
				ApprovalSourceID: sourceID,
				ApprovedAt:       time.Now().UTC(),
			},
		})
		if err == nil || result != nil || launcher.request.ApprovalProof != nil {
			t.Fatalf("issuer error result=%#v err=%v launchRequest=%#v", result, err, launcher.request)
		}
	})

	t.Run("nil proof", func(t *testing.T) {
		sourceID := "task-review:" + uuid.NewString()
		launcher := &nilProofAutomationLauncher{}
		result, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
			OwnerIdentity:    "alice",
			TaskID:           "task-nil-proof-1",
			AutomationID:     uuid.NewString(),
			Task:             "Reviewed action",
			ApprovalSourceID: sourceID,
			approvalDecision: &automation.TaskApprovalDecisionRequest{
				OwnerIdentity:    "alice",
				Task:             "Reviewed action",
				ApprovalSourceID: sourceID,
				ApprovedAt:       time.Now().UTC(),
			},
		})
		if err == nil || result != nil || launcher.launchCalls != 0 {
			t.Fatalf("nil proof result=%#v err=%v launches=%d, want fail-closed", result, err, launcher.launchCalls)
		}
	})
}

func TestTaskServiceRejectsForgedReviewProvenanceWithoutQueuedItem(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
	)
	forgedReviewID := uuid.NewString()

	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:    "alice",
		Request:          "Delete account data by running a local script",
		ProjectKey:       "018-hai",
		AutomationID:     executor.result.AutomationID,
		ExecuteAllowed:   true,
		HumanApproved:    true,
		ApprovalNote:     "Invented approval",
		ApprovalSourceID: "task-review:" + forgedReviewID,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 0 || plan.CompletionStatus != "review_required" {
		t.Fatalf(
			"unrecorded review provenance executed work: calls=%d status=%q plan=%#v",
			executor.calls,
			plan.CompletionStatus,
			plan,
		)
	}
}

func TestTaskReviewResolutionUsesExactOwnerScopedReviewID(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
	)
	scoped := service.(OwnerScopedService)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Delete account data by running a local script",
		ProjectKey:     "018-hai",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.ReviewQueueItem == nil || executor.calls != 0 {
		t.Fatalf("initial run bypassed review: plan=%#v calls=%d", plan, executor.calls)
	}
	reviewID := plan.ReviewQueueItem.ID

	if result, err := scoped.ResolveReviewItemForOwner("bob", reviewID, ApprovalDecision{Approved: true}); err == nil || result != nil {
		t.Fatalf("wrong owner resolved Alice review: result=%#v err=%v", result, err)
	}
	if executor.calls != 0 {
		t.Fatalf("wrong-owner resolution executed %d actions, want zero", executor.calls)
	}

	result, err := scoped.ResolveReviewItemForOwner("alice", reviewID, ApprovalDecision{
		Approved: true,
		Note:     "Approve this exact action",
	})
	if err != nil {
		t.Fatalf("ResolveReviewItemForOwner: %v", err)
	}
	if result == nil || result.Plan == nil || executor.calls != 1 || len(executor.requests) != 1 {
		t.Fatalf("approved review did not execute exactly once: result=%#v calls=%d", result, executor.calls)
	}
	expectedSource := "task-review:" + reviewID
	if executor.requests[0].ApprovalSourceID != expectedSource {
		t.Fatalf("approval source = %q, want exact provenance %q", executor.requests[0].ApprovalSourceID, expectedSource)
	}
}

func TestTaskReviewAuditRedactsApprovalNotes(t *testing.T) {
	executor := &fakeToolExecutor{result: completedToolResult()}
	service := NewServiceWithEngines(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		executor,
	)
	plan, err := service.Run(IntakeRequest{
		OwnerIdentity:  "alice",
		Request:        "Delete account data by running a local script",
		ProjectKey:     "018-hai",
		AutomationID:   executor.result.AutomationID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.ReviewQueueItem == nil {
		t.Fatalf("expected review queue item")
	}

	result, err := service.ResolveReviewItem(plan.ReviewQueueItem.ID, ApprovalDecision{
		Approved: true,
		Note:     "Approved for one action token=task-review-secret-value",
	})
	if err != nil {
		t.Fatalf("ResolveReviewItem: %v", err)
	}
	payload, err := json.Marshal(struct {
		Result *ReviewResolutionResult `json:"result"`
		Logs   []CompletionPlan        `json:"logs"`
		Queue  []ReviewQueueItem       `json:"queue"`
	}{
		Result: result,
		Logs:   service.Logs(),
		Queue:  service.ReviewQueue(),
	})
	if err != nil {
		t.Fatalf("marshal review audit: %v", err)
	}
	if strings.Contains(string(payload), "task-review-secret-value") {
		t.Fatalf("task review audit leaked approval note secret: %s", payload)
	}
}

type nilProofAutomationLauncher struct {
	launchCalls int
}

func (l *nilProofAutomationLauncher) Launch(id uuid.UUID) (*automation.LaunchResult, error) {
	l.launchCalls++
	return &automation.LaunchResult{AutomationID: id, Status: "completed"}, nil
}

func (l *nilProofAutomationLauncher) LaunchTask(id uuid.UUID, _ automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	l.launchCalls++
	return &automation.LaunchResult{AutomationID: id, Status: "completed", LaunchedAt: time.Now().UTC()}, nil
}

func (*nilProofAutomationLauncher) IssueApprovalProof(
	uuid.UUID,
	automation.TaskApprovalProofRequest,
) (*automation.ApprovalProof, error) {
	return nil, nil
}

func (*nilProofAutomationLauncher) RecordApprovalDecision(
	uuid.UUID,
	automation.TaskApprovalDecisionRequest,
) error {
	return nil
}
