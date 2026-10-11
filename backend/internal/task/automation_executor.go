package task

import (
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/executionauth"

	"github.com/google/uuid"
)

type automationLauncher interface {
	Launch(id uuid.UUID) (*automation.LaunchResult, error)
	LaunchTask(id uuid.UUID, request automation.TaskLaunchRequest) (*automation.LaunchResult, error)
}

type automationApprovalProofIssuer interface {
	IssueApprovalProof(id uuid.UUID, request automation.TaskApprovalProofRequest) (*automation.ApprovalProof, error)
}

type automationApprovalDecisionRecorder interface {
	RecordApprovalDecision(id uuid.UUID, request automation.TaskApprovalDecisionRequest) error
}

type AutomationToolExecutor struct {
	launcher automationLauncher
}

func NewAutomationToolExecutor(launcher automationLauncher) *AutomationToolExecutor {
	return &AutomationToolExecutor{launcher: launcher}
}

func (e *AutomationToolExecutor) Execute(request ToolExecutionRequest) (*ToolExecutionResult, error) {
	if e == nil || e.launcher == nil {
		return nil, fmt.Errorf("automation runtime launcher is not configured")
	}
	id, err := uuid.Parse(strings.TrimSpace(request.AutomationID))
	if err != nil {
		return nil, fmt.Errorf("automationId must be a valid UUID")
	}
	var proof *automation.ApprovalProof
	approvalSourceID := strings.TrimSpace(request.ApprovalSourceID)
	launchApprovalSourceID := approvalSourceID
	if approvalSourceID != "" {
		sourceKind, err := validateExecutionApprovalSource(approvalSourceID)
		if err != nil {
			return nil, err
		}
		if sourceKind == "task-review" {
			if request.approvalDecision == nil {
				return nil, fmt.Errorf("task review approval is not backed by a verified queued decision")
			}
			recorder, ok := e.launcher.(automationApprovalDecisionRecorder)
			if !ok {
				return nil, fmt.Errorf("automation runtime approval decision recorder is not configured")
			}
			decision := *request.approvalDecision
			if decision.ApprovalSourceID != approvalSourceID ||
				strings.TrimSpace(decision.OwnerIdentity) != strings.TrimSpace(request.OwnerIdentity) ||
				strings.TrimSpace(decision.Task) != strings.TrimSpace(request.Task) ||
				strings.TrimSpace(decision.ProjectKey) != strings.TrimSpace(request.ProjectKey) ||
				strings.TrimSpace(decision.MandateID) != strings.TrimSpace(request.MandateID) ||
				strings.TrimSpace(decision.ApprovalBindingDigest) !=
					strings.TrimSpace(request.ApprovalBindingDigest) {
				return nil, fmt.Errorf("task review decision does not match the exact execution request")
			}
			if err := recorder.RecordApprovalDecision(id, decision); err != nil {
				return nil, fmt.Errorf("record exact automation approval decision: %w", err)
			}
		} else {
			workflowID, parseErr := uuid.Parse(strings.TrimSpace(request.WorkflowID))
			if parseErr != nil || workflowID == uuid.Nil {
				return nil, fmt.Errorf("workflow approval requires a valid workflow binding")
			}
		}
		requiresActionProof := true
		if inspector, ok := e.launcher.(automation.ActionApprovalRequirementInspector); ok {
			requiresActionProof, err = inspector.ActionApprovalRequired(id)
			if err != nil {
				return nil, fmt.Errorf("inspect automation approval requirement: %w", err)
			}
		}
		if requiresActionProof {
			issuer, ok := e.launcher.(automationApprovalProofIssuer)
			if !ok {
				return nil, fmt.Errorf("automation runtime approval proof issuer is not configured")
			}
			proof, err = issuer.IssueApprovalProof(id, automation.TaskApprovalProofRequest{
				OwnerIdentity:    request.OwnerIdentity,
				Task:             request.Task,
				OriginalRequest:  request.OriginalRequest,
				ProjectKey:       request.ProjectKey,
				MandateID:        strings.TrimSpace(request.MandateID),
				WorkflowID:       request.WorkflowID,
				ApprovalSourceID: approvalSourceID,
			})
			if err != nil {
				return nil, fmt.Errorf("issue action-bound automation approval proof: %w", err)
			}
			if err := automation.ValidateIssuedApprovalProofEnvelope(
				proof,
				request.OwnerIdentity,
				id,
				approvalSourceID,
				time.Now().UTC(),
			); err != nil {
				return nil, fmt.Errorf("validate issued automation approval proof: %w", err)
			}
			// The queued review digest proves the upstream human decision. The
			// execution boundary must instead consume authority for the exact
			// current automation action that the proof issuer derived from stored
			// configuration. This also gives workflow approvals the same
			// final-effect binding without trusting caller-supplied digest material.
			request.ApprovalBindingDigest = proof.ActionDigest
		} else {
			// Read-only actions do not need a one-use final-effect proof, but a
			// valid exact workflow decision still determines the case-approved
			// autonomy level and remains part of the authorization audit.
		}
	}
	result, err := e.launcher.LaunchTask(id, automation.TaskLaunchRequest{
		OwnerIdentity:         request.OwnerIdentity,
		ActorIdentity:         "hai-task-engine",
		ActorKind:             executionauth.ActorSystem,
		TaskID:                request.TaskID,
		Task:                  request.Task,
		ProjectKey:            request.ProjectKey,
		MandateID:             strings.TrimSpace(request.MandateID),
		ApprovalSourceID:      launchApprovalSourceID,
		ApprovalBindingDigest: request.ApprovalBindingDigest,
		Governance:            request.Governance,
		ApprovalProof:         proof,
		ExecutionContext:      request.executionContext,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("automation runtime returned no launch result")
	}
	return &ToolExecutionResult{
		AutomationID:      result.AutomationID.String(),
		LaunchEventID:     uuidStringOrEmpty(result.LaunchEventID),
		RuntimeType:       result.RuntimeType,
		LaunchType:        result.LaunchType,
		Target:            result.Target,
		Status:            result.Status,
		Message:           sanitizeTaskOperationalText(result.Message, 2048),
		Output:            sanitizeTaskOperationalText(result.Output, 8192),
		RuntimeRouteTrace: copyAutomationRuntimeRouteTrace(result.RuntimeRouteTrace),
		ExitCode:          result.ExitCode,
		DurationMs:        result.DurationMs,
		RequiresApproval:  result.RequiresApproval,
		AuditEvents:       sanitizeTaskAuditEvents(result.AuditEvents),
		ExecutedAt:        result.LaunchedAt,
	}, nil
}

func validateExecutionApprovalSource(sourceID string) (string, error) {
	sourceID = strings.TrimSpace(sourceID)
	for _, prefix := range []string{"task-review:", "workflow-decision:"} {
		if !strings.HasPrefix(sourceID, prefix) {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(sourceID, prefix))
		if err != nil || id == uuid.Nil {
			return "", fmt.Errorf("approval source must identify a recorded decision UUID")
		}
		return strings.TrimSuffix(prefix, ":"), nil
	}
	return "", fmt.Errorf("approval source type is not supported")
}

func uuidStringOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
