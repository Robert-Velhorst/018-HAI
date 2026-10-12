package task

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestAutomationToolExecutorMapsControlledLaunchResult(t *testing.T) {
	id := uuid.New()
	launchEventID := uuid.New()
	launchedAt := time.Now().UTC()
	launcher := &fakeAutomationLauncher{
		result: &automation.LaunchResult{
			AutomationID:  id,
			LaunchEventID: launchEventID,
			RuntimeType:   "script",
			LaunchType:    "script",
			Target:        "verify-project.sh",
			Status:        "completed",
			Message:       "script completed",
			Output:        "tests passed",
			RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
				RuntimeID:         "openclaw",
				Intent:            "code_review",
				ExecutionMode:     "read_only",
				RiskLevel:         "medium",
				RecommendedSkills: []string{"autoreview", "gitcrawl"},
				BlockedSurfaces:   []string{"external_message_sending"},
			},
			ExitCode:    0,
			DurationMs:  42,
			AuditEvents: []string{"script executed without shell"},
			LaunchedAt:  launchedAt,
		},
	}
	executor := NewAutomationToolExecutor(launcher)
	result, err := executor.Execute(ToolExecutionRequest{
		OwnerIdentity: "alice",
		TaskID:        "task-run-tests-1",
		AutomationID:  id.String(),
		Task:          "Run tests",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "completed" || result.Output != "tests passed" {
		t.Fatalf("result = %#v, want completed runtime evidence", result)
	}
	if result.AutomationID != id.String() || result.LaunchEventID != launchEventID.String() || !result.ExecutedAt.Equal(launchedAt) {
		t.Fatalf("runtime identity was not preserved: %#v", result)
	}
	if result.RuntimeRouteTrace == nil || result.RuntimeRouteTrace.RuntimeID != "openclaw" || len(result.RuntimeRouteTrace.RecommendedSkills) != 2 {
		t.Fatalf("runtime route trace was not preserved: %#v", result.RuntimeRouteTrace)
	}
	if launcher.request.OwnerIdentity != "alice" {
		t.Fatalf("launch request owner = %q, want alice", launcher.request.OwnerIdentity)
	}
	if launcher.request.IdempotencyKey == "" {
		t.Fatal("launch request omitted the task-scoped idempotency key")
	}
	if launcher.request.ApprovalProof != nil || launcher.issueCalls != 0 {
		t.Fatalf("unapproved request received an approval proof: request=%#v issues=%d", launcher.request, launcher.issueCalls)
	}
}

func TestAutomationToolExecutorForwardsReadOnlyWorkflowDecisionWithoutMintingProof(t *testing.T) {
	id := uuid.New()
	approvalSourceID := "workflow-decision:" + uuid.NewString()
	approvalDigest := strings.Repeat("d", 64)
	requiresActionApproval := false
	launcher := &fakeAutomationLauncher{
		actionApprovalRequired: &requiresActionApproval,
		result: &automation.LaunchResult{
			AutomationID:  id,
			LaunchEventID: uuid.New(),
			LaunchType:    "api",
			Status:        "completed",
			LaunchedAt:    time.Now().UTC(),
		},
	}

	result, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
		OwnerIdentity:         "alice",
		TaskID:                "task-readonly-health-1",
		WorkflowID:            uuid.NewString(),
		AutomationID:          id.String(),
		Task:                  "Run the reviewed read-only health probe.",
		ApprovalSourceID:      approvalSourceID,
		ApprovalBindingDigest: approvalDigest,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
	if launcher.approvalInspectionCalls != 1 || launcher.issueCalls != 0 {
		t.Fatalf("approval inspection/proof calls = %d/%d, want 1/0", launcher.approvalInspectionCalls, launcher.issueCalls)
	}
	if launcher.request.ApprovalSourceID != approvalSourceID || launcher.request.ApprovalBindingDigest != approvalDigest || launcher.request.ApprovalProof != nil {
		t.Fatalf("launch request = %#v, want exact workflow decision without a final-effect proof", launcher.request)
	}
}

func TestAutomationToolExecutorForwardsImmutableGovernanceEvidence(t *testing.T) {
	id := uuid.New()
	mandateID := uuid.NewString()
	digest := strings.Repeat("a", 64)
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
		AutomationID: id,
		Status:       "completed",
		LaunchedAt:   time.Now().UTC(),
	}}
	governance := executionauth.GovernanceEvidence{
		TaskPlanID:                       "plan-1",
		TaskPlanDigest:                   digest,
		FrameworkSelectionID:             "selection-1",
		FrameworkCatalogVersion:          "catalog-v1",
		FrameworkCatalogDigest:           digest,
		FrameworkPreferenceDigest:        digest,
		FrameworkConstitutionDigest:      digest,
		FrameworkOperatingContractDigest: digest,
		EvidenceReferences:               []string{"task-plan://plan-1"},
	}

	_, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
		OwnerIdentity: "alice",
		TaskID:        "task-governed-1",
		AutomationID:  id.String(),
		Task:          "Run governed task",
		MandateID:     mandateID,
		Governance:    governance,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if launcher.request.Governance.TaskPlanDigest != digest ||
		launcher.request.Governance.FrameworkSelectionID != "selection-1" ||
		launcher.request.MandateID != mandateID {
		t.Fatalf("governance evidence was not forwarded: %#v", launcher.request.Governance)
	}
}

func TestAutomationToolExecutorIssuesActionBoundProofForRecordedReview(t *testing.T) {
	id := uuid.New()
	sourceID := "task-review:" + uuid.NewString()
	issuedAt := time.Now().UTC()
	proof := &automation.ApprovalProof{
		ID:               "proof-1",
		OwnerIdentity:    "alice",
		AutomationID:     id,
		ActionDigest:     strings.Repeat("a", 64),
		Scope:            automation.ApprovalScopeScript,
		ApprovalSourceID: sourceID,
		IssuedAt:         issuedAt,
		ExpiresAt:        issuedAt.Add(time.Minute),
		Nonce:            "proof-nonce",
		Signature:        "proof-signature",
	}
	launcher := &fakeAutomationLauncher{
		proof: proof,
		result: &automation.LaunchResult{
			AutomationID: id,
			Status:       "completed",
			LaunchedAt:   time.Now().UTC(),
		},
	}
	executor := NewAutomationToolExecutor(launcher)
	reviewDigest := strings.Repeat("b", 64)
	result, err := executor.Execute(ToolExecutionRequest{
		OwnerIdentity:         "alice",
		TaskID:                "task-reviewed-1",
		AutomationID:          id.String(),
		Task:                  "Run the exact reviewed action.",
		ProjectKey:            "018-hai",
		ApprovalSourceID:      sourceID,
		ApprovalBindingDigest: reviewDigest,
		approvalDecision: &automation.TaskApprovalDecisionRequest{
			OwnerIdentity:         "alice",
			Task:                  "Run the exact reviewed action.",
			ProjectKey:            "018-hai",
			ApprovalSourceID:      sourceID,
			ApprovalBindingDigest: reviewDigest,
			ApprovedAt:            issuedAt.Add(-time.Second),
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || result.Status != "completed" {
		t.Fatalf("result = %#v, want completed", result)
	}
	if launcher.issueCalls != 1 || launcher.issueID != id {
		t.Fatalf("proof issuance = calls %d id %s, want one for %s", launcher.issueCalls, launcher.issueID, id)
	}
	if launcher.issueRequest.OwnerIdentity != "alice" ||
		launcher.issueRequest.Task != "Run the exact reviewed action." ||
		launcher.issueRequest.ProjectKey != "018-hai" ||
		launcher.issueRequest.ApprovalSourceID != sourceID {
		t.Fatalf("proof issue request was not action-bound: %#v", launcher.issueRequest)
	}
	if launcher.recordCalls != 1 || launcher.recordRequest.ApprovalSourceID != sourceID {
		t.Fatalf("approval decision was not registered exactly once: %#v", launcher.recordRequest)
	}
	if launcher.request.ApprovalProof != proof || launcher.request.ApprovalSourceID != sourceID {
		t.Fatalf("launch request did not carry issued proof: %#v", launcher.request)
	}
	if launcher.request.ApprovalBindingDigest != proof.ActionDigest {
		t.Fatalf(
			"launch binding digest = %q, want exact proof action digest %q",
			launcher.request.ApprovalBindingDigest,
			proof.ActionDigest,
		)
	}
}

func TestAutomationToolExecutorFailsClosedWhenProofIssuerIsUnavailable(t *testing.T) {
	launcher := &launchOnlyAutomationLauncher{}
	executor := NewAutomationToolExecutor(launcher)
	sourceID := "task-review:" + uuid.NewString()
	_, err := executor.Execute(ToolExecutionRequest{
		OwnerIdentity:    "alice",
		TaskID:           "task-missing-issuer-1",
		AutomationID:     uuid.NewString(),
		Task:             "Run reviewed action.",
		ApprovalSourceID: sourceID,
		approvalDecision: &automation.TaskApprovalDecisionRequest{
			OwnerIdentity:    "alice",
			Task:             "Run reviewed action.",
			ApprovalSourceID: sourceID,
			ApprovedAt:       time.Now().UTC(),
		},
	})
	if err == nil || launcher.launchCalls != 0 {
		t.Fatalf("missing proof issuer did not fail closed: err=%v launches=%d", err, launcher.launchCalls)
	}
}

func TestAutomationToolExecutorUsesStableTaskScopedIdempotencyKey(t *testing.T) {
	automationID := uuid.New()
	launcher := &fakeAutomationLauncher{
		result: &automation.LaunchResult{AutomationID: automationID, Status: "completed"},
	}
	executor := NewAutomationToolExecutor(launcher)
	request := ToolExecutionRequest{
		OwnerIdentity: "alice",
		TaskID:        "durable-task-1",
		AutomationID:  automationID.String(),
		Task:          "Run the approved automation",
	}

	if _, err := executor.Execute(request); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	firstKey := launcher.request.IdempotencyKey
	if len(firstKey) == 0 || len(firstKey) > 256 {
		t.Fatalf("idempotency key length = %d, want 1..256: %q", len(firstKey), firstKey)
	}
	for _, character := range firstKey {
		if character < 0x21 || character > 0x7e {
			t.Fatalf("idempotency key contains non-visible-ASCII character %q", character)
		}
	}

	if _, err := executor.Execute(request); err != nil {
		t.Fatalf("retry Execute: %v", err)
	}
	if retryKey := launcher.request.IdempotencyKey; retryKey != firstKey {
		t.Fatalf("retry key = %q, want stable key %q", retryKey, firstKey)
	}

	request.TaskID = "durable-task-2"
	if _, err := executor.Execute(request); err != nil {
		t.Fatalf("new task Execute: %v", err)
	}
	if newTaskKey := launcher.request.IdempotencyKey; newTaskKey == firstKey {
		t.Fatalf("new task reused idempotency key %q", newTaskKey)
	}

	request.TaskID = "durable-task-1"
	request.OwnerIdentity = "bob"
	if _, err := executor.Execute(request); err != nil {
		t.Fatalf("different owner Execute: %v", err)
	}
	if ownerKey := launcher.request.IdempotencyKey; ownerKey == firstKey {
		t.Fatalf("different owner reused idempotency key %q", ownerKey)
	}

	request.OwnerIdentity = "alice"
	request.AutomationID = uuid.NewString()
	if _, err := executor.Execute(request); err != nil {
		t.Fatalf("different automation Execute: %v", err)
	}
	if automationKey := launcher.request.IdempotencyKey; automationKey == firstKey {
		t.Fatalf("different automation reused idempotency key %q", automationKey)
	}
}

func TestAutomationToolExecutorFailsClosedWithoutDurableTaskID(t *testing.T) {
	for _, taskID := range []string{"", " \t\n "} {
		t.Run(fmt.Sprintf("task-id-%q", taskID), func(t *testing.T) {
			sourceID := "task-review:" + uuid.NewString()
			launcher := &fakeAutomationLauncher{
				result: &automation.LaunchResult{Status: "completed"},
			}
			_, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
				OwnerIdentity:    "alice",
				TaskID:           taskID,
				AutomationID:     uuid.NewString(),
				Task:             "Run reviewed action",
				ApprovalSourceID: sourceID,
				approvalDecision: &automation.TaskApprovalDecisionRequest{
					OwnerIdentity:    "alice",
					Task:             "Run reviewed action",
					ApprovalSourceID: sourceID,
					ApprovedAt:       time.Now().UTC(),
				},
			})
			if err == nil || !strings.Contains(err.Error(), "durable task ID is required") {
				t.Fatalf("Execute error = %v, want missing durable task ID", err)
			}
			if launcher.launchCalls != 0 || launcher.issueCalls != 0 || launcher.recordCalls != 0 {
				t.Fatalf("missing task ID performed side effects: launches=%d proofs=%d decisions=%d",
					launcher.launchCalls, launcher.issueCalls, launcher.recordCalls)
			}
		})
	}
}

func TestAutomationToolExecutorUsesIssuedActionBindingForWorkflowApproval(t *testing.T) {
	automationID := uuid.New()
	workflowID := uuid.NewString()
	sourceID := "workflow-decision:" + uuid.NewString()
	now := time.Now().UTC()
	proof := &automation.ApprovalProof{
		ID:               "workflow-proof",
		OwnerIdentity:    "alice",
		AutomationID:     automationID,
		ActionDigest:     strings.Repeat("c", 64),
		Scope:            automation.ApprovalScopeScript,
		ApprovalSourceID: sourceID,
		IssuedAt:         now,
		ExpiresAt:        now.Add(time.Minute),
		Nonce:            "workflow-proof-nonce",
		Signature:        "workflow-proof-signature",
	}
	launcher := &fakeAutomationLauncher{
		proof: proof,
		result: &automation.LaunchResult{
			AutomationID: automationID,
			Status:       "completed",
			LaunchedAt:   now,
		},
	}

	_, err := NewAutomationToolExecutor(launcher).Execute(ToolExecutionRequest{
		OwnerIdentity:    "alice",
		TaskID:           "task-1",
		AutomationID:     automationID.String(),
		Task:             "Execute the approved workflow step.",
		OriginalRequest:  "Complete the reviewed workflow.",
		ProjectKey:       "018-hai",
		WorkflowID:       workflowID,
		ApprovalSourceID: sourceID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if launcher.recordCalls != 0 {
		t.Fatalf("workflow approval must not be rewritten as a task review")
	}
	if launcher.issueRequest.WorkflowID != workflowID {
		t.Fatalf(
			"proof workflow binding = %q, want %q",
			launcher.issueRequest.WorkflowID,
			workflowID,
		)
	}
	if launcher.request.ApprovalBindingDigest != proof.ActionDigest {
		t.Fatalf(
			"launch binding digest = %q, want proof action digest %q",
			launcher.request.ApprovalBindingDigest,
			proof.ActionDigest,
		)
	}
}

func TestAutomationToolExecutorRejectsInvalidAutomationID(t *testing.T) {
	executor := NewAutomationToolExecutor(&fakeAutomationLauncher{})
	if _, err := executor.Execute(ToolExecutionRequest{AutomationID: "not-a-uuid"}); err == nil {
		t.Fatalf("expected invalid automation ID to be rejected")
	}
}

type fakeAutomationLauncher struct {
	launchCalls             int
	result                  *automation.LaunchResult
	err                     error
	request                 automation.TaskLaunchRequest
	proof                   *automation.ApprovalProof
	issueErr                error
	issueID                 uuid.UUID
	issueRequest            automation.TaskApprovalProofRequest
	issueCalls              int
	recordRequest           automation.TaskApprovalDecisionRequest
	recordCalls             int
	actionApprovalRequired  *bool
	approvalInspectionErr   error
	approvalInspectionCalls int
}

func (f *fakeAutomationLauncher) Launch(id uuid.UUID) (*automation.LaunchResult, error) {
	return f.result, f.err
}

func (f *fakeAutomationLauncher) LaunchTask(id uuid.UUID, request automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	f.launchCalls++
	f.request = request
	return f.result, f.err
}

func (f *fakeAutomationLauncher) IssueApprovalProof(id uuid.UUID, request automation.TaskApprovalProofRequest) (*automation.ApprovalProof, error) {
	f.issueCalls++
	f.issueID = id
	f.issueRequest = request
	return f.proof, f.issueErr
}

func (f *fakeAutomationLauncher) RecordApprovalDecision(id uuid.UUID, request automation.TaskApprovalDecisionRequest) error {
	f.recordCalls++
	f.issueID = id
	f.recordRequest = request
	return nil
}

func (f *fakeAutomationLauncher) ActionApprovalRequired(id uuid.UUID) (bool, error) {
	f.approvalInspectionCalls++
	if f.approvalInspectionErr != nil {
		return false, f.approvalInspectionErr
	}
	if f.actionApprovalRequired != nil {
		return *f.actionApprovalRequired, nil
	}
	return true, nil
}

type launchOnlyAutomationLauncher struct {
	launchCalls int
}

func (f *launchOnlyAutomationLauncher) Launch(id uuid.UUID) (*automation.LaunchResult, error) {
	f.launchCalls++
	return &automation.LaunchResult{AutomationID: id}, nil
}

func (f *launchOnlyAutomationLauncher) LaunchTask(id uuid.UUID, request automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	f.launchCalls++
	return &automation.LaunchResult{AutomationID: id}, nil
}

func (f *launchOnlyAutomationLauncher) RecordApprovalDecision(uuid.UUID, automation.TaskApprovalDecisionRequest) error {
	return nil
}
