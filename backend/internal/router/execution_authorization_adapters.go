package router

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/opscontrol"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func authenticatedLLMEffectContext(c *gin.Context) (llm.EffectContext, error) {
	value, exists := c.Get(identity.ContextSubjectKey)
	subject, ok := value.(string)
	subject = strings.TrimSpace(subject)
	if !exists || !ok || subject == "" {
		return llm.EffectContext{}, fmt.Errorf("authenticated identity is required for model execution")
	}
	return llm.EffectContext{
		OwnerIdentity: subject,
		ActorIdentity: subject,
		ActorKind:     string(executionauth.ActorHuman),
		TaskID:        "llm-request:" + uuid.NewString(),
		ProjectKey:    "direct-llm",
	}, nil
}

const (
	llmAuthorizationConsumer       = "llm.final-effect"
	ecosystemAuthorizationConsumer = "agent-runtime.ecosystem.final-effect"
)

// llmExecutionAuthorizer adapts the cycle-free LLM boundary to the canonical
// execution-authorization service. Local, reversible inference can run under
// autonomous-safe policy. Cloud data egress, paid usage, and explicitly gated
// models require exact case approval or a bounded standing mandate.
type llmExecutionAuthorizer struct {
	service *executionauth.Service
}

func (a llmExecutionAuthorizer) AuthorizeFinalEffect(
	ctx context.Context,
	input llm.FinalEffectAuthorizationRequest,
) error {
	if a.service == nil {
		return fmt.Errorf("execution authorization service is unavailable")
	}
	stage := executionauth.StageToolUse
	risk := executionauth.RiskLow
	reversible := true
	requiredAuthority := 4
	requestedAutonomy := 8
	if !input.ProviderLocal {
		stage = executionauth.StageDataAccess
		risk = executionauth.RiskMedium
		reversible = false
		requiredAuthority = 6
		requestedAutonomy = 6
	}
	if input.Paid || input.EstimatedCostEUR > 0 {
		stage = executionauth.StageExpenditure
		risk = executionauth.RiskHigh
		reversible = false
		requiredAuthority = 8
		requestedAutonomy = 6
	}
	if input.RequiresApproval {
		requestedAutonomy = 6
		if risk == executionauth.RiskLow {
			risk = executionauth.RiskMedium
		}
	}
	if strings.TrimSpace(input.MandateID) != "" {
		requestedAutonomy = 7
	}
	actorKind, err := executionActorKind(input.ActorKind)
	if err != nil {
		return err
	}
	request := executionauth.Request{
		OwnerIdentity:         input.OwnerIdentity,
		IdempotencyKey:        "llm:" + input.EffectDigest,
		ActorIdentity:         input.ActorIdentity,
		ActorKind:             actorKind,
		TaskID:                input.TaskID,
		Action:                input.Operation,
		Stage:                 stage,
		ResourceType:          "llm-model",
		ResourceID:            input.ProviderID + "/" + input.ModelID,
		ProjectKey:            input.ProjectKey,
		ToolID:                input.ProviderID,
		RuntimeID:             input.ProviderID,
		RequiredAuthority:     requiredAuthority,
		RequestedAutonomy:     requestedAutonomy,
		Risk:                  risk,
		Reversible:            reversible,
		EstimatedCostEUR:      input.EstimatedCostEUR,
		MandateID:             input.MandateID,
		ApprovalSourceID:      input.ApprovalSourceID,
		ApprovalBindingDigest: input.ApprovalBindingDigest,
		EffectDigest:          input.EffectDigest,
		Facts: map[string]string{
			"provider":      input.ProviderID,
			"model":         input.ModelID,
			"providerLocal": fmt.Sprintf("%t", input.ProviderLocal),
			"paid":          fmt.Sprintf("%t", input.Paid),
			"endpointKey":   input.EndpointKey,
			"promptDigest":  input.PromptDigest,
			"payloadDigest": input.PayloadDigest,
		},
		SourceReferences: []string{
			"llm-provider://" + input.ProviderID + "/" + input.ModelID,
		},
	}
	receipt, err := a.service.AuthorizeAndConsume(
		ctx,
		request,
		llmAuthorizationConsumer,
		"llm:"+input.ProviderID+":"+input.ModelID+":"+input.EffectDigest,
	)
	if err != nil {
		return fmt.Errorf("LLM final effect was not authorized: %w", err)
	}
	if receipt.Outcome != executionauth.OutcomeAuthorized ||
		receipt.OwnerIdentity != request.OwnerIdentity ||
		receipt.ActorIdentity != request.ActorIdentity ||
		receipt.TaskID != request.TaskID ||
		receipt.Action != request.Action ||
		receipt.Stage != request.Stage ||
		receipt.ResourceType != request.ResourceType ||
		receipt.ResourceID != request.ResourceID ||
		receipt.ProjectKey != request.ProjectKey ||
		receipt.RuntimeID != request.RuntimeID ||
		receipt.EffectDigest != request.EffectDigest ||
		receipt.ApprovalSourceID != request.ApprovalSourceID {
		return fmt.Errorf("LLM authorization receipt does not match the final effect")
	}
	return nil
}

type ecosystemExecutionAuthorizer struct {
	service *executionauth.Service
}

// ecosystemMutationApprovalPreparer adapts the existing owner-control signer
// to the OpenClaw boundary. It prepares a signed, five-minute proof for the
// digest already derived by agentruntime; it cannot select an effect itself.
type ecosystemMutationApprovalPreparer struct {
	issuer opscontrol.OwnerControlApprovalIssuer
}

func (p ecosystemMutationApprovalPreparer) PrepareEcosystemMutationApproval(
	ownerIdentity string,
	taskID string,
	effectDigest string,
) (agentruntime.EcosystemMutationAuthorization, error) {
	if p.issuer == nil {
		return agentruntime.EcosystemMutationAuthorization{},
			fmt.Errorf("owner control approval issuer is unavailable")
	}
	approval, err := p.issuer.Prepare(ownerIdentity, effectDigest)
	if err != nil {
		return agentruntime.EcosystemMutationAuthorization{}, err
	}
	return agentruntime.EcosystemMutationAuthorization{
		IdempotencyKey:        "agent-runtime-openclaw:" + uuid.NewString(),
		TaskID:                taskID,
		ApprovalSourceID:      approval.SourceID,
		ApprovalBindingDigest: approval.BindingDigest,
	}, nil
}

func (a ecosystemExecutionAuthorizer) AuthorizeAndConsumeEcosystemMutation(
	ctx context.Context,
	input agentruntime.EcosystemMutationAuthorizationRequest,
	consumer string,
	executionTarget string,
) (agentruntime.EcosystemMutationAuthorizationReceipt, error) {
	if a.service == nil {
		return agentruntime.EcosystemMutationAuthorizationReceipt{},
			fmt.Errorf("execution authorization service is unavailable")
	}
	actorKind, err := executionActorKind(input.ActorKind)
	if err != nil {
		return agentruntime.EcosystemMutationAuthorizationReceipt{}, err
	}
	stage, err := executionStage(input.Stage)
	if err != nil {
		return agentruntime.EcosystemMutationAuthorizationReceipt{}, err
	}
	risk, err := executionRisk(input.Risk)
	if err != nil {
		return agentruntime.EcosystemMutationAuthorizationReceipt{}, err
	}
	request := executionauth.Request{
		OwnerIdentity:         input.OwnerIdentity,
		IdempotencyKey:        input.IdempotencyKey,
		ActorIdentity:         input.ActorIdentity,
		ActorKind:             actorKind,
		TaskID:                input.TaskID,
		Action:                input.Action,
		Stage:                 stage,
		ResourceType:          input.ResourceType,
		ResourceID:            input.ResourceID,
		RuntimeID:             input.RuntimeID,
		RequiredAuthority:     input.RequiredAuthority,
		RequestedAutonomy:     input.RequestedAutonomy,
		Risk:                  risk,
		Reversible:            input.Reversible,
		ApprovalSourceID:      input.ApprovalSourceID,
		ApprovalBindingDigest: input.ApprovalBindingDigest,
		EffectDigest:          input.EffectDigest,
		SourceReferences:      append([]string(nil), input.SourceReferences...),
		RequestedAt:           input.RequestedAt,
	}
	if strings.TrimSpace(consumer) == "" {
		consumer = ecosystemAuthorizationConsumer
	}
	receipt, err := a.service.AuthorizeAndConsume(
		ctx,
		request,
		consumer,
		executionTarget,
	)
	if err != nil {
		return agentruntime.EcosystemMutationAuthorizationReceipt{}, err
	}
	return agentruntime.EcosystemMutationAuthorizationReceipt{
		ReceiptID:             receipt.ID.String(),
		DecisionDigest:        receipt.DecisionDigest,
		Outcome:               string(receipt.Outcome),
		OwnerIdentity:         receipt.OwnerIdentity,
		ActorIdentity:         receipt.ActorIdentity,
		TaskID:                receipt.TaskID,
		Action:                receipt.Action,
		Stage:                 string(receipt.Stage),
		ResourceType:          receipt.ResourceType,
		ResourceID:            receipt.ResourceID,
		RuntimeID:             receipt.RuntimeID,
		ApprovalSourceID:      receipt.ApprovalSourceID,
		ApprovalBindingDigest: input.ApprovalBindingDigest,
		ApprovalDecisionID:    receipt.Evidence.Approval.DecisionID,
		ApprovedBy:            receipt.Evidence.Approval.ApprovedBy,
		ApprovedAt:            receipt.Evidence.Approval.ApprovedAt,
		ApprovalExpiresAt:     receipt.Evidence.Approval.ExpiresAt,
		EffectDigest:          receipt.EffectDigest,
		EvaluatedAt:           receipt.EvaluatedAt,
	}, nil
}

func executionActorKind(value string) (executionauth.ActorKind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(executionauth.ActorHuman):
		return executionauth.ActorHuman, nil
	case string(executionauth.ActorSystem):
		return executionauth.ActorSystem, nil
	default:
		return "", fmt.Errorf("unsupported execution actor kind %q", value)
	}
}

func executionStage(value string) (executionauth.Stage, error) {
	stage := executionauth.Stage(strings.ToLower(strings.TrimSpace(value)))
	switch stage {
	case executionauth.StageDataAccess, executionauth.StageToolUse,
		executionauth.StageExpenditure, executionauth.StageCommunication,
		executionauth.StageCommitment, executionauth.StageExecution,
		executionauth.StagePublication, executionauth.StageDeletion,
		executionauth.StagePrivilegeEscalation, executionauth.StageSelfModification:
		return stage, nil
	default:
		return "", fmt.Errorf("unsupported execution stage %q", value)
	}
}

func executionRisk(value string) (executionauth.RiskLevel, error) {
	risk := executionauth.RiskLevel(strings.ToLower(strings.TrimSpace(value)))
	switch risk {
	case executionauth.RiskLow, executionauth.RiskMedium,
		executionauth.RiskHigh, executionauth.RiskCritical:
		return risk, nil
	default:
		return "", fmt.Errorf("unsupported execution risk %q", value)
	}
}
