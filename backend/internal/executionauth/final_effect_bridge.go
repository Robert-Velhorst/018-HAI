package executionauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	AgentRuntimeExecuteAction  = "agent-runtime.execute-task"
	AgentRuntimeResourceType   = "agent-runtime-task"
	finalEffectFreshnessWindow = 30 * time.Second
)

// FinalEffectBinding contains only durable receipt references. Automation
// passes these values to agentruntime.Registry.BindConsumedAuthorizationProof
// after AuthorizeAndConsume succeeds.
type FinalEffectBinding struct {
	ReceiptID                  string
	AuthorizationRequestDigest string
	DecisionDigest             string
	RuntimeProof               string
}

// FinalEffectBridge connects the execution authorization ledger to the final
// agent-runtime proof boundary. It verifies existing authority and records one
// exercise; it never grants or consumes authority.
type FinalEffectBridge struct {
	repository Repository
	now        func() time.Time
}

var _ agentruntime.FinalEffectProofVerifier = (*FinalEffectBridge)(nil)

func NewFinalEffectBridge(
	repository Repository,
	now func() time.Time,
) (*FinalEffectBridge, error) {
	if repository == nil {
		return nil, fmt.Errorf("execution authorization repository is required")
	}
	if now == nil {
		now = time.Now
	}
	return &FinalEffectBridge{repository: repository, now: now}, nil
}

// BuildAgentRuntimeFinalEffectRequest reconstructs the exact request that the
// agent-runtime registry will verify immediately before adapter execution.
func BuildAgentRuntimeFinalEffectRequest(
	runtimeID string,
	taskID string,
	ownerIdentity string,
	projectKey string,
	prompt string,
	approvalSourceID string,
	requiresApproval bool,
) (agentruntime.FinalEffectAuthorizationRequest, error) {
	if strings.TrimSpace(prompt) == "" {
		return agentruntime.FinalEffectAuthorizationRequest{},
			fmt.Errorf("agent runtime prompt is required")
	}
	promptHash := sha256.Sum256([]byte(prompt))
	request := agentruntime.FinalEffectAuthorizationRequest{
		Operation:        AgentRuntimeExecuteAction,
		RuntimeID:        strings.ToLower(strings.TrimSpace(runtimeID)),
		TaskID:           strings.TrimSpace(taskID),
		OwnerIdentity:    strings.TrimSpace(ownerIdentity),
		ProjectKey:       strings.TrimSpace(projectKey),
		PromptDigest:     hex.EncodeToString(promptHash[:]),
		ApprovalSourceID: strings.TrimSpace(approvalSourceID),
		RequiresApproval: requiresApproval,
	}
	if _, err := FinalEffectDigest(request); err != nil {
		return agentruntime.FinalEffectAuthorizationRequest{}, err
	}
	return request, nil
}

// FinalEffectDigest returns the exact lowercase SHA-256 digest used by the
// agent-runtime final request binding.
func FinalEffectDigest(
	request agentruntime.FinalEffectAuthorizationRequest,
) (string, error) {
	if err := validateAgentRuntimeFinalRequest(request); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode agent runtime final effect: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// FinalEffectExecutionTarget is the only valid consumption target for an
// agent-runtime effect. The digest already binds runtime, task, owner, project,
// prompt, approval source, and runtime approval policy.
func FinalEffectExecutionTarget(effectDigest string) (string, error) {
	effectDigest = strings.TrimSpace(effectDigest)
	if !validDigest(effectDigest) {
		return "", fmt.Errorf("effect digest must be an exact lowercase SHA-256 digest")
	}
	return "agent-runtime:" + effectDigest, nil
}

// BindConsumedFinalEffect verifies that the receipt was authorized and
// consumed for this exact final request, then returns the references needed by
// agentruntime.Registry.BindConsumedAuthorizationProof. It does not exercise
// the proof; exercise happens only inside VerifyFinalEffectProof.
func (b *FinalEffectBridge) BindConsumedFinalEffect(
	ctx context.Context,
	request agentruntime.FinalEffectAuthorizationRequest,
	receiptID uuid.UUID,
) (FinalEffectBinding, error) {
	if err := memoryContextError(ctx); err != nil {
		return FinalEffectBinding{}, err
	}
	if b == nil || b.repository == nil {
		return FinalEffectBinding{}, ErrPolicyUnavailable
	}
	effectDigest, err := FinalEffectDigest(request)
	if err != nil {
		return FinalEffectBinding{}, err
	}
	receipt, err := b.repository.Get(ctx, request.OwnerIdentity, receiptID)
	if err != nil {
		return FinalEffectBinding{}, err
	}
	consumption, err := b.repository.GetConsumption(
		ctx,
		request.OwnerIdentity,
		receiptID,
	)
	if err != nil {
		return FinalEffectBinding{}, err
	}
	if err := ctx.Err(); err != nil {
		return FinalEffectBinding{}, err
	}
	exercise := finalEffectExercise(
		request,
		agentruntime.FinalEffectAuthorizationProof{
			ReceiptID:                  receipt.ID.String(),
			AuthorizationRequestDigest: receipt.RequestDigest,
			DecisionDigest:             receipt.DecisionDigest,
			RuntimeRequestDigest:       effectDigest,
			RuntimeProof:               effectDigest,
		},
		time.Time{},
	)
	if !finalEffectMatches(receipt, consumption, exercise) {
		return FinalEffectBinding{}, ErrFinalEffectMismatch
	}
	return FinalEffectBinding{
		ReceiptID:                  receipt.ID.String(),
		AuthorizationRequestDigest: receipt.RequestDigest,
		DecisionDigest:             receipt.DecisionDigest,
		RuntimeProof:               effectDigest,
	}, nil
}

// VerifyFinalEffectProof implements agentruntime.FinalEffectProofVerifier.
// Repository.ExerciseFinalEffect performs the authoritative atomic match and
// append, so concurrent replays cannot both reach a runtime adapter.
func (b *FinalEffectBridge) VerifyFinalEffectProof(
	ctx context.Context,
	request agentruntime.FinalEffectAuthorizationRequest,
	proof agentruntime.FinalEffectAuthorizationProof,
) error {
	if err := memoryContextError(ctx); err != nil {
		return err
	}
	if b == nil || b.repository == nil {
		return ErrPolicyUnavailable
	}
	effectDigest, err := FinalEffectDigest(request)
	if err != nil {
		return err
	}
	receiptID, err := uuid.Parse(strings.TrimSpace(proof.ReceiptID))
	if err != nil || receiptID == uuid.Nil {
		return fmt.Errorf("%w: receipt id is invalid", ErrFinalEffectMismatch)
	}
	for label, value := range map[string]string{
		"authorization request digest": proof.AuthorizationRequestDigest,
		"decision digest":              proof.DecisionDigest,
		"runtime request digest":       proof.RuntimeRequestDigest,
		"runtime proof":                proof.RuntimeProof,
	} {
		if value != strings.ToLower(strings.TrimSpace(value)) || !validDigest(value) {
			return fmt.Errorf("%w: %s is invalid", ErrFinalEffectMismatch, label)
		}
	}
	if proof.RuntimeRequestDigest != effectDigest || proof.RuntimeProof != effectDigest {
		return fmt.Errorf("%w: runtime effect digest changed", ErrFinalEffectMismatch)
	}
	return b.repository.ExerciseFinalEffect(
		ctx,
		finalEffectExercise(request, proof, monotonicNow(b.now)),
	)
}

func finalEffectExercise(
	request agentruntime.FinalEffectAuthorizationRequest,
	proof agentruntime.FinalEffectAuthorizationProof,
	exercisedAt time.Time,
) FinalEffectExercise {
	target, _ := FinalEffectExecutionTarget(proof.RuntimeRequestDigest)
	return FinalEffectExercise{
		ReceiptID:                  uuid.MustParse(strings.TrimSpace(proof.ReceiptID)),
		OwnerIdentity:              request.OwnerIdentity,
		RuntimeID:                  request.RuntimeID,
		TaskID:                     request.TaskID,
		Action:                     request.Operation,
		ResourceType:               AgentRuntimeResourceType,
		ResourceID:                 request.TaskID,
		ProjectKey:                 request.ProjectKey,
		ApprovalSourceID:           request.ApprovalSourceID,
		EffectDigest:               proof.RuntimeProof,
		AuthorizationRequestDigest: proof.AuthorizationRequestDigest,
		DecisionDigest:             proof.DecisionDigest,
		RuntimeRequestDigest:       proof.RuntimeRequestDigest,
		ConsumptionTarget:          target,
		ExercisedAt:                exercisedAt,
	}
}

func validateAgentRuntimeFinalRequest(
	request agentruntime.FinalEffectAuthorizationRequest,
) error {
	if err := safety.ValidateRuntimeModel(request.RuntimeID, request.RuntimeModel); err != nil {
		return err
	}
	if request.Operation != AgentRuntimeExecuteAction {
		return fmt.Errorf("agent runtime final effect operation is invalid")
	}
	if request.RuntimeID != strings.ToLower(strings.TrimSpace(request.RuntimeID)) {
		return fmt.Errorf("agent runtime id must be normalized lowercase")
	}
	for label, value := range map[string]string{
		"runtime id":     request.RuntimeID,
		"task id":        request.TaskID,
		"owner identity": request.OwnerIdentity,
	} {
		if err := validateIdentifier(label, value); err != nil {
			return err
		}
	}
	if request.TaskID != strings.TrimSpace(request.TaskID) ||
		request.OwnerIdentity != strings.TrimSpace(request.OwnerIdentity) ||
		request.ProjectKey != strings.TrimSpace(request.ProjectKey) ||
		request.ApprovalSourceID != strings.TrimSpace(request.ApprovalSourceID) {
		return fmt.Errorf("agent runtime final effect identifiers must be normalized")
	}
	if len(request.ProjectKey) > 256 || len(request.ApprovalSourceID) > 256 {
		return fmt.Errorf("agent runtime final effect provenance exceeds its bound")
	}
	if !validDigest(request.PromptDigest) {
		return fmt.Errorf("agent runtime prompt digest must be lowercase SHA-256")
	}
	if request.RequiresApproval && request.ApprovalSourceID == "" {
		return fmt.Errorf("approval-required runtime effect needs approval provenance")
	}
	return nil
}

func validateFinalEffectExercise(value FinalEffectExercise) error {
	if err := validateReceiptLookup(value.OwnerIdentity, value.ReceiptID); err != nil {
		return err
	}
	for label, candidate := range map[string]string{
		"runtime id":    value.RuntimeID,
		"task id":       value.TaskID,
		"action":        value.Action,
		"resource type": value.ResourceType,
		"resource id":   value.ResourceID,
	} {
		if err := validateIdentifier(label, candidate); err != nil {
			return err
		}
	}
	if value.Action != AgentRuntimeExecuteAction ||
		value.ResourceType != AgentRuntimeResourceType ||
		value.ResourceID != value.TaskID {
		return ErrFinalEffectMismatch
	}
	if len(value.ProjectKey) > 256 || len(value.ApprovalSourceID) > 256 {
		return fmt.Errorf("final effect provenance exceeds its bound")
	}
	for label, candidate := range map[string]string{
		"effect digest":                value.EffectDigest,
		"authorization request digest": value.AuthorizationRequestDigest,
		"decision digest":              value.DecisionDigest,
		"runtime request digest":       value.RuntimeRequestDigest,
	} {
		if !validDigest(candidate) {
			return fmt.Errorf("%s must be an exact lowercase SHA-256 digest", label)
		}
	}
	target, err := FinalEffectExecutionTarget(value.EffectDigest)
	if err != nil || value.ConsumptionTarget != target ||
		value.RuntimeRequestDigest != value.EffectDigest {
		return ErrFinalEffectMismatch
	}
	if value.ExercisedAt.IsZero() {
		return fmt.Errorf("final effect exercise timestamp is required")
	}
	return nil
}

func finalEffectMatches(
	receipt Receipt,
	consumption Consumption,
	exercise FinalEffectExercise,
) bool {
	if receipt.Outcome != OutcomeAuthorized ||
		receipt.Stage != StageExecution ||
		receipt.ID != exercise.ReceiptID ||
		receipt.OwnerIdentity != exercise.OwnerIdentity ||
		receipt.TaskID != exercise.TaskID ||
		receipt.Action != exercise.Action ||
		receipt.ResourceType != exercise.ResourceType ||
		receipt.ResourceID != exercise.ResourceID ||
		receipt.ProjectKey != exercise.ProjectKey ||
		receipt.RuntimeID != exercise.RuntimeID ||
		receipt.ApprovalSourceID != exercise.ApprovalSourceID ||
		receipt.EffectDigest != exercise.EffectDigest ||
		receipt.RequestDigest != exercise.AuthorizationRequestDigest ||
		receipt.DecisionDigest != exercise.DecisionDigest {
		return false
	}
	if receipt.ApprovalSourceID != receipt.Evidence.Approval.SourceID {
		return false
	}
	return consumption.ReceiptID == exercise.ReceiptID &&
		consumption.OwnerIdentity == exercise.OwnerIdentity &&
		consumption.ReceiptDigest == exercise.DecisionDigest &&
		consumption.ExecutionTarget == exercise.ConsumptionTarget
}

func finalEffectAuthorityFresh(receipt Receipt, consumption Consumption, exercisedAt time.Time) bool {
	exercisedAt = exercisedAt.UTC()
	consumedAt := consumption.ConsumedAt.UTC()
	if exercisedAt.IsZero() || consumedAt.IsZero() ||
		consumedAt.Before(receipt.EvaluatedAt) || exercisedAt.Before(consumedAt) ||
		exercisedAt.Sub(consumedAt) > finalEffectFreshnessWindow {
		return false
	}
	if receipt.ApprovalSourceID == "" {
		return true
	}
	approval := receipt.Evidence.Approval
	return approval.SourceID == receipt.ApprovalSourceID &&
		!approval.ExpiresAt.IsZero() && exercisedAt.Before(approval.ExpiresAt)
}
