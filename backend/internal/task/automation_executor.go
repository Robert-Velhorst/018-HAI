package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func (e *AutomationToolExecutor) InspectReviewConfiguration(id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	if e == nil || e.launcher == nil {
		return nil, fmt.Errorf("automation runtime launcher is not configured")
	}
	inspector, ok := e.launcher.(automation.ReviewConfigurationInspector)
	if !ok {
		return nil, fmt.Errorf("automation runtime review configuration inspector is not configured")
	}
	return inspector.InspectReviewConfiguration(id)
}

func (e *AutomationToolExecutor) InspectReviewConfigurationContext(ctx context.Context, id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	if ctx == nil {
		return nil, automation.ErrReviewConfigurationContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, taskOperationStorageTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.launcher == nil {
		return nil, automation.ErrReviewConfigurationContextUnavailable
	}
	inspector, ok := e.launcher.(automation.ContextualReviewConfigurationInspector)
	if !ok {
		return nil, automation.ErrReviewConfigurationContextUnavailable
	}
	snapshot, err := inspector.InspectReviewConfigurationContext(ctx, id)
	if joined := errors.Join(err, ctx.Err()); joined != nil {
		return nil, joined
	}
	return snapshot, nil
}

func reviewConfigurationInspectionRequired(executor ToolExecutor) bool {
	_, legacy := executor.(automation.ReviewConfigurationInspector)
	_, contextual := executor.(automation.ContextualReviewConfigurationInspector)
	return legacy || contextual
}

func (e *AutomationToolExecutor) Execute(request ToolExecutionRequest) (executed *ToolExecutionResult, executeErr error) {
	launchAttempted := false
	defer func() {
		if executeErr == nil {
			return
		}
		executeErr = &automationExecutionFailure{err: executeErr}
		if !launchAttempted {
			executeErr = MarkToolFailureBeforeDispatch(executeErr)
		}
	}()
	ctx := request.ExecutionContext
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.launcher == nil {
		return nil, fmt.Errorf("automation runtime launcher is not configured")
	}
	id, err := uuid.Parse(strings.TrimSpace(request.AutomationID))
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("automationId must be a valid UUID")
	}
	idempotencyKey, err := taskLaunchIdempotencyKey(request.TaskID, request.OwnerIdentity, id)
	if err != nil {
		return nil, err
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
			decision := *request.approvalDecision
			if decision.ReviewConfiguration != nil {
				if err := automation.ValidateReviewConfigurationSnapshot(decision.ReviewConfiguration, id); err != nil {
					return nil, err
				}
				snapshot := *decision.ReviewConfiguration
				decision.ReviewConfiguration = &snapshot
			}
			if decision.ApprovalSourceID != approvalSourceID ||
				strings.TrimSpace(decision.OwnerIdentity) != strings.TrimSpace(request.OwnerIdentity) ||
				strings.TrimSpace(decision.Task) != strings.TrimSpace(request.Task) ||
				strings.TrimSpace(decision.ProjectKey) != strings.TrimSpace(request.ProjectKey) ||
				strings.TrimSpace(decision.MandateID) != strings.TrimSpace(request.MandateID) ||
				strings.TrimSpace(decision.ApprovalBindingDigest) !=
					strings.TrimSpace(request.ApprovalBindingDigest) {
				return nil, fmt.Errorf("task review decision does not match the exact execution request")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var recordErr error
			if request.ExecutionContext != nil {
				recorder, ok := e.launcher.(automation.ContextualApprovalDecisionRecorder)
				if !ok {
					return nil, automation.ErrApprovalRegistrationContextUnavailable
				}
				registrationCtx, cancelRegistration := context.WithTimeout(ctx, taskOperationStorageTimeout)
				recordErr = recorder.RecordApprovalDecisionContext(registrationCtx, id, decision)
				recordErr = errors.Join(recordErr, registrationCtx.Err())
				cancelRegistration()
			} else {
				recorder, ok := e.launcher.(automationApprovalDecisionRecorder)
				if !ok {
					return nil, fmt.Errorf("automation runtime approval decision recorder is not configured")
				}
				recordErr = recorder.RecordApprovalDecision(id, decision)
			}
			if err := errors.Join(recordErr, ctx.Err()); err != nil {
				return nil, fmt.Errorf("record exact automation approval decision: %w", err)
			}
		} else {
			workflowID, parseErr := uuid.Parse(strings.TrimSpace(request.WorkflowID))
			if parseErr != nil || workflowID == uuid.Nil {
				return nil, fmt.Errorf("workflow approval requires a valid workflow binding")
			}
		}
		requiresActionProof := true
		if request.ExecutionContext != nil {
			inspector, ok := e.launcher.(automation.ContextualActionApprovalRequirementInspector)
			if !ok {
				return nil, automation.ErrApprovalRequirementContextUnavailable
			}
			requirementCtx, cancelRequirement := context.WithTimeout(ctx, taskOperationStorageTimeout)
			requiresActionProof, err = inspector.ActionApprovalRequiredContext(requirementCtx, id)
			if contextErr := requirementCtx.Err(); contextErr != nil {
				err = errors.Join(err, contextErr)
			}
			cancelRequirement()
		} else if inspector, ok := e.launcher.(automation.ActionApprovalRequirementInspector); ok {
			requiresActionProof, err = inspector.ActionApprovalRequired(id)
		}
		if contextErr := ctx.Err(); contextErr != nil {
			err = errors.Join(err, contextErr)
		}
		if err != nil {
			return nil, fmt.Errorf("inspect automation approval requirement: %w", err)
		}
		if requiresActionProof {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			proofRequest := automation.TaskApprovalProofRequest{
				OwnerIdentity:    request.OwnerIdentity,
				Task:             request.Task,
				OriginalRequest:  request.OriginalRequest,
				ProjectKey:       request.ProjectKey,
				MandateID:        strings.TrimSpace(request.MandateID),
				WorkflowID:       request.WorkflowID,
				ApprovalSourceID: approvalSourceID,
			}
			if request.ExecutionContext != nil {
				issuer, ok := e.launcher.(automation.ContextualApprovalProofIssuer)
				if !ok {
					return nil, automation.ErrApprovalIssuanceContextUnavailable
				}
				proofCtx, cancelProof := context.WithTimeout(ctx, taskOperationStorageTimeout)
				proof, err = issuer.IssueApprovalProofContext(proofCtx, id, proofRequest)
				if contextErr := proofCtx.Err(); contextErr != nil {
					err = errors.Join(err, contextErr)
				}
				cancelProof()
			} else {
				issuer, ok := e.launcher.(automationApprovalProofIssuer)
				if !ok {
					return nil, fmt.Errorf("automation runtime approval proof issuer is not configured")
				}
				proof, err = issuer.IssueApprovalProof(id, proofRequest)
			}
			if contextErr := ctx.Err(); contextErr != nil {
				err = errors.Join(err, contextErr)
			}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	launchAttempted = true
	result, err := e.launcher.LaunchTask(id, automation.TaskLaunchRequest{
		ExecutionContext:      ctx,
		IdempotencyKey:        idempotencyKey,
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
	})
	if result == nil {
		// LaunchTask can fail after dispatch or durable outcome persistence.
		// Missing evidence must not invent an intent or imply a safe retry.
		if err == nil {
			err = fmt.Errorf("automation runtime returned no launch result; outcome requires reconciliation")
		}
		return &ToolExecutionResult{
			AutomationID:     id.String(),
			Status:           "indeterminate",
			Message:          "automation launch outcome is unknown; reconcile possible effects before authorizing another attempt",
			ExitCode:         -1,
			OutcomeUncertain: true,
		}, err
	}
	executed = &ToolExecutionResult{
		AutomationID:       uuidStringOrEmpty(result.AutomationID),
		LaunchEventID:      uuidStringOrEmpty(result.LaunchEventID),
		RuntimeTaskID:      sanitizeTaskOperationalText(result.RuntimeTaskID, 512),
		ExecutionReference: sanitizeTaskOperationalText(result.ExecutionReference, 2048),
		RuntimeType:        sanitizeTaskOperationalText(result.RuntimeType, 128),
		LaunchType:         sanitizeTaskOperationalText(result.LaunchType, 128),
		Target:             sanitizeTaskOperationalText(result.Target, 2048),
		Status:             sanitizeTaskOperationalText(result.Status, 128),
		Message:            sanitizeTaskOperationalText(result.Message, 2048),
		Output:             sanitizeTaskOperationalText(result.Output, 8192),
		RuntimeRouteTrace:  copyAutomationRuntimeRouteTrace(result.RuntimeRouteTrace),
		ExitCode:           result.ExitCode,
		DurationMs:         result.DurationMs,
		RequiresApproval:   result.RequiresApproval,
		AuditEvents:        sanitizeTaskAuditEvents(result.AuditEvents),
		ExecutedAt:         result.LaunchedAt,
	}
	if trace := executed.RuntimeRouteTrace; trace != nil {
		for _, field := range []*string{&trace.RuntimeID, &trace.Intent, &trace.ExecutionMode, &trace.RiskLevel} {
			*field = sanitizeTaskOperationalText(*field, 128)
		}
		for _, values := range []*[]string{
			&trace.RecommendedSkills, &trace.VisibleProviders, &trace.VisibleTools,
			&trace.RelevantMaps, &trace.BlockedSurfaces, &trace.RequiredControls, &trace.ValidationChecklist,
		} {
			*values = sanitizeTaskAuditEvents(*values)
		}
	}
	executed.OutcomeUncertain = err != nil || result.AutomationID != id ||
		result.Status != strings.ToLower(strings.TrimSpace(result.Status)) ||
		(result.Status == "completed" && (result.LaunchEventID == uuid.Nil || !automationCompletedReceiptExitValid(result))) ||
		ToolExecutionOutcomeUncertain(executed)
	return executed, err
}

func automationCompletedReceiptExitValid(result *automation.LaunchResult) bool {
	switch strings.ToLower(strings.TrimSpace(result.LaunchType)) {
	case "api":
		// API receipts use HTTP status, not process exit status. The launcher's
		// configured expected-status check determines whether it completed.
		return result.ExitCode >= 100 && result.ExitCode <= 599
	case "docker_service":
		return result.ExitCode == http.StatusNoContent || result.ExitCode == http.StatusNotModified
	default:
		return result.ExitCode == 0
	}
}

// Keep error identity for internal classification, but redact its display text.
type automationExecutionFailure struct{ err error }

func (e *automationExecutionFailure) Error() string {
	return sanitizeTaskOperationalText(e.err.Error(), 2048)
}

func (e *automationExecutionFailure) Unwrap() error { return e.err }

func taskLaunchIdempotencyKey(taskID, ownerIdentity string, automationID uuid.UUID) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("durable task ID is required for automation launch")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return "", fmt.Errorf("owner identity is required for idempotent automation launch")
	}
	identity, err := json.Marshal(struct {
		Version       string `json:"version"`
		TaskID        string `json:"taskId"`
		OwnerIdentity string `json:"ownerIdentity"`
		AutomationID  string `json:"automationId"`
	}{
		Version:       "task-launch-v1",
		TaskID:        taskID,
		OwnerIdentity: ownerIdentity,
		AutomationID:  automationID.String(),
	})
	if err != nil {
		return "", fmt.Errorf("encode task launch idempotency identity: %w", err)
	}
	digest := sha256.Sum256(identity)
	return "task-launch:v1:" + hex.EncodeToString(digest[:]), nil
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
