package temporalbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"
)

const (
	scheduleFollowUpAction        = "temporal.schedule-follow-up"
	scheduleFollowUpResourceType  = "temporal-workflow"
	temporalRuntimeID             = "temporal"
	temporalAuthorizationConsumer = "temporalbridge.schedule-follow-up"
)

// FinalEffectAuthorizer is the unified execution-authorization boundary used
// immediately before a durable Temporal workflow is scheduled.
type FinalEffectAuthorizer interface {
	AuthorizeAndConsume(
		context.Context,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, error)
}

// scheduleEffect is the complete immutable scheduling intent. Its digest binds
// owner, action, resource, workflow, task, project, approval provenance,
// scheduled time, and bounded follow-up limit.
type scheduleEffect struct {
	ContractVersion  int       `json:"contractVersion"`
	OwnerIdentity    string    `json:"ownerIdentity"`
	Action           string    `json:"action"`
	ResourceType     string    `json:"resourceType"`
	ResourceID       string    `json:"resourceId"`
	WorkflowID       string    `json:"workflowId"`
	WorkflowType     string    `json:"workflowType"`
	TaskID           string    `json:"taskId"`
	ProjectKey       string    `json:"projectKey,omitempty"`
	ApprovalSourceID string    `json:"approvalSourceId,omitempty"`
	RunAt            time.Time `json:"runAt"`
	Limit            int       `json:"limit"`
}

func buildScheduleAuthorizationRequest(
	ownerIdentity string,
	runID string,
	workflowID string,
	request FollowUpRequest,
) (executionauth.Request, string, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	runID = strings.TrimSpace(runID)
	workflowID = strings.TrimSpace(workflowID)
	taskID := strings.TrimSpace(request.TaskID)
	if taskID == "" {
		taskID = "temporal-follow-up:" + runID
	}
	effect := scheduleEffect{
		ContractVersion:  1,
		OwnerIdentity:    ownerIdentity,
		Action:           scheduleFollowUpAction,
		ResourceType:     scheduleFollowUpResourceType,
		ResourceID:       runID,
		WorkflowID:       workflowID,
		WorkflowType:     followUpWorkflowType,
		TaskID:           taskID,
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
		RunAt:            request.RunAt.UTC().Truncate(time.Microsecond),
		Limit:            normalizeLimit(request.Limit),
	}
	if ownerIdentity == "" || runID == "" || workflowID == "" {
		return executionauth.Request{}, "", fmt.Errorf(
			"temporal scheduling authorization identity is incomplete",
		)
	}
	encoded, err := json.Marshal(effect)
	if err != nil {
		return executionauth.Request{}, "", fmt.Errorf(
			"encode Temporal scheduling effect: %w",
			err,
		)
	}
	sum := sha256.Sum256(encoded)
	effectDigest := hex.EncodeToString(sum[:])
	executionTarget := temporalRuntimeID + ":" + effectDigest
	return executionauth.Request{
		OwnerIdentity:         ownerIdentity,
		IdempotencyKey:        "temporal-schedule:" + runID,
		ActorIdentity:         ownerIdentity,
		ActorKind:             executionauth.ActorHuman,
		TaskID:                taskID,
		Action:                scheduleFollowUpAction,
		Stage:                 executionauth.StageExecution,
		ResourceType:          scheduleFollowUpResourceType,
		ResourceID:            runID,
		ProjectKey:            effect.ProjectKey,
		ToolID:                followUpWorkflowType,
		RuntimeID:             temporalRuntimeID,
		RequiredAuthority:     6,
		RequestedAutonomy:     6,
		Risk:                  executionauth.RiskMedium,
		Reversible:            false,
		ApprovalSourceID:      effect.ApprovalSourceID,
		ApprovalBindingDigest: strings.TrimSpace(request.ApprovalBindingDigest),
		EffectDigest:          effectDigest,
		Facts: map[string]string{
			"workflowId":   workflowID,
			"workflowType": followUpWorkflowType,
		},
		SourceReferences: []string{"temporal-run:" + runID},
	}, executionTarget, nil
}
