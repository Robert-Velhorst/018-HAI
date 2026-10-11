package task

import (
	"context"
	"errors"
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
	result, err := executor.Execute(ToolExecutionRequest{OwnerIdentity: "alice", AutomationID: id.String(), Task: "Run tests"})
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
	if launcher.request.ApprovalProof != nil || launcher.issueCalls != 0 {
		t.Fatalf("unapproved request received an approval proof: request=%#v issues=%d", launcher.request, launcher.issueCalls)
	}
}

func TestAutomationToolExecutorPassesTaskExecutionContext(t *testing.T) {
	id := uuid.New()
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
		AutomationID:  id,
		LaunchEventID: uuid.New(),
		Status:        "completed",
		LaunchedAt:    time.Now().UTC(),
	}}
	executor := NewAutomationToolExecutor(launcher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := executor.Execute(ToolExecutionRequest{
		OwnerIdentity:    "alice",
		AutomationID:     id.String(),
		Task:             "Run tests",
		executionContext: ctx,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if launcher.request.ExecutionContext == nil || !errors.Is(launcher.request.ExecutionContext.Err(), context.Canceled) {
		t.Fatalf("launch context = %v, want canceled task context", launcher.request.ExecutionContext)
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
