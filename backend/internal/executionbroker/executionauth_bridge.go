package executionbroker

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

const (
	localSafeWorkerActor = "system:phase2-safe-worker"
	localSafeWorkerTool  = "phase2-local-safe-worker"
)

// ExecutionAuthorizationService is the narrow durable executionauth contract
// used by the local safe worker. Both authorization and consumption remain in
// executionauth's owner-scoped repository.
type ExecutionAuthorizationService interface {
	Authorize(context.Context, executionauth.Request) (executionauth.Receipt, error)
	AuthorizeAndConsume(
		context.Context,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, error)
	Get(context.Context, string, uuid.UUID) (executionauth.Receipt, error)
}

// DurableAuthorizationBridge mints and consumes local-safe-worker authority.
// The owner, task, action, target, and effect are derived here; an incoming
// SafeWorkerInput.Authorization value is never trusted.
type DurableAuthorizationBridge struct {
	service     ExecutionAuthorizationService
	owner       string
	workspaceID string
}

var _ AuthorizationVerifier = (*DurableAuthorizationBridge)(nil)

func NewDurableAuthorizationBridge(
	service ExecutionAuthorizationService,
	owner string,
	workspaceID string,
) (*DurableAuthorizationBridge, error) {
	owner = strings.TrimSpace(owner)
	workspaceID = strings.TrimSpace(workspaceID)
	if service == nil {
		return nil, fmt.Errorf("executionbroker: execution authorization service is required")
	}
	if owner == "" {
		return nil, fmt.Errorf("executionbroker: owner identity is required")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("executionbroker: workspace identity is required")
	}
	return &DurableAuthorizationBridge{
		service:     service,
		owner:       owner,
		workspaceID: workspaceID,
	}, nil
}

// Issue derives and persists authorization for one exact bounded file effect.
// It replaces, rather than validates, any caller-provided binding.
func (b *DurableAuthorizationBridge) Issue(
	ctx context.Context,
	workspaceRoot string,
	input SafeWorkerInput,
) (SafeWorkerInput, error) {
	ctx, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return SafeWorkerInput{}, err
	}
	defer finish()
	prepared, effect, err := b.prepareInput(workspaceRoot, input)
	if err != nil {
		return SafeWorkerInput{}, err
	}
	if scope, active := operations.CurrentSafeEffectScope(ctx); active {
		if scope.OwnerUserID != b.owner || scope.WorkspaceID != b.workspaceID {
			return SafeWorkerInput{}, ErrAuthorizationMismatch
		}
		if err := validateScopedSafeWorkerInput(scope, prepared); err != nil {
			return SafeWorkerInput{}, err
		}
		prepared.Authorization.OperationScope = &scope
		prepared.Authorization.EffectDigest, err = BindLocalSafeWorkerEffect(workspaceRoot, prepared)
		if err != nil {
			return SafeWorkerInput{}, err
		}
		effect.OperationScope = &scope
		effect.ContractVersion = effectContractVersion(&scope)
		effect.EffectDigest = prepared.Authorization.EffectDigest
	} else if managedSafeWorkerInput(prepared) {
		// Direct runtime callers cannot impersonate the managed ledger path.
		return SafeWorkerInput{}, ErrAuthorizationRequired
	}
	request := b.authorizationRequest(effect)
	receipt, err := b.service.Authorize(ctx, request)
	if err != nil {
		return SafeWorkerInput{}, fmt.Errorf("executionbroker: authorize final effect: %w", err)
	}
	if receipt.Outcome != executionauth.OutcomeAuthorized {
		return SafeWorkerInput{}, fmt.Errorf(
			"%w: %s",
			ErrAuthorizationDenied,
			receipt.Reason,
		)
	}
	if err := b.verifyReceipt(receipt, effect); err != nil {
		return SafeWorkerInput{}, err
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return SafeWorkerInput{}, err
	}
	prepared.Authorization.ReceiptID = receipt.ID.String()
	prepared.Authorization.ReceiptDigest = receipt.DecisionDigest
	return prepared, nil
}

// VerifyAndConsume reconstructs the executionauth request from the canonical
// final effect, rechecks mutable policy, and atomically consumes the receipt.
// It is called by LocalSafeWorker immediately before its first filesystem
// mutation.
func (b *DurableAuthorizationBridge) VerifyAndConsume(
	ctx context.Context,
	verification AuthorizationVerification,
) (VerifiedAuthorization, error) {
	ctx, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return VerifiedAuthorization{}, err
	}
	defer finish()
	effect, err := b.verifyFinalBoundary(verification)
	if err != nil {
		return VerifiedAuthorization{}, err
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return VerifiedAuthorization{}, err
	}
	receiptID, err := uuid.Parse(strings.TrimSpace(verification.Binding.ReceiptID))
	if err != nil || receiptID == uuid.Nil {
		return VerifiedAuthorization{}, fmt.Errorf(
			"%w: invalid receipt identity",
			ErrAuthorizationRequired,
		)
	}
	persisted, err := b.service.Get(ctx, b.owner, receiptID)
	if err != nil {
		return VerifiedAuthorization{}, fmt.Errorf(
			"%w: resolve durable receipt: %v",
			ErrAuthorizationDenied,
			err,
		)
	}
	if err := b.verifyReceipt(persisted, effect); err != nil {
		return VerifiedAuthorization{}, err
	}
	if !equalText(persisted.DecisionDigest, verification.Binding.ReceiptDigest) {
		return VerifiedAuthorization{}, ErrAuthorizationMismatch
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return VerifiedAuthorization{}, err
	}

	receipt, err := b.service.AuthorizeAndConsume(
		ctx,
		b.authorizationRequest(effect),
		LocalSafeWorkerID,
		consumptionTarget(effect),
	)
	if err != nil {
		return VerifiedAuthorization{}, fmt.Errorf("%w: %v", ErrAuthorizationDenied, err)
	}
	if receipt.ID != persisted.ID ||
		!equalText(receipt.DecisionDigest, persisted.DecisionDigest) {
		return VerifiedAuthorization{}, ErrAuthorizationMismatch
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return VerifiedAuthorization{}, err
	}
	return VerifiedAuthorization{
		OwnerIdentity: receipt.OwnerIdentity,
		TaskID:        receipt.TaskID,
		Action:        receipt.Action,
		ReceiptID:     receipt.ID.String(),
		ReceiptDigest: receipt.DecisionDigest,
		EffectDigest:  receipt.EffectDigest,
	}, nil
}

func (b *DurableAuthorizationBridge) prepareInput(
	workspaceRoot string,
	input SafeWorkerInput,
) (SafeWorkerInput, FinalEffect, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return SafeWorkerInput{}, FinalEffect{}, fmt.Errorf(
			"executionbroker: workspace root is required",
		)
	}
	if strings.TrimSpace(input.Marker) == "" {
		return SafeWorkerInput{}, FinalEffect{}, fmt.Errorf("safe worker: marker required")
	}
	if err := validateArtifactName(input.ArtifactName); err != nil {
		return SafeWorkerInput{}, FinalEffect{}, err
	}
	root, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return SafeWorkerInput{}, FinalEffect{}, fmt.Errorf(
			"executionbroker: canonicalize workspace: %w",
			err,
		)
	}
	root = filepath.Clean(root)
	payloadHash := sha256.Sum256([]byte(input.Marker))
	taskID := deriveTaskID(root, input.ArtifactName, hex.EncodeToString(payloadHash[:]))

	// Discard all inbound receipt data. Authority is minted only from the
	// server-side owner and the canonical effect derived above.
	input.Authorization = AuthorizationBinding{
		OwnerIdentity: b.owner,
		TaskID:        taskID,
		Action:        LocalSafeWorkerAction,
	}
	effectDigest, err := BindLocalSafeWorkerEffect(root, input)
	if err != nil {
		return SafeWorkerInput{}, FinalEffect{}, err
	}
	input.Authorization.EffectDigest = effectDigest
	effect := FinalEffect{
		ContractVersion: authorizationContractVersion,
		RuntimeID:       LocalSafeWorkerID,
		Action:          LocalSafeWorkerAction,
		ResourceType:    LocalSafeWorkerResourceType,
		OwnerIdentity:   b.owner,
		TaskID:          taskID,
		WorkspaceRoot:   root,
		ArtifactName:    input.ArtifactName,
		PayloadDigest:   hex.EncodeToString(payloadHash[:]),
		EffectDigest:    effectDigest,
	}
	return input, effect, nil
}

func (b *DurableAuthorizationBridge) verifyFinalBoundary(
	verification AuthorizationVerification,
) (FinalEffect, error) {
	effect := verification.Effect
	if effect.OperationScope == nil && strings.HasPrefix(effect.ArtifactName, "operation-") {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	if effect.ContractVersion != effectContractVersion(effect.OperationScope) ||
		effect.RuntimeID != LocalSafeWorkerID ||
		effect.Action != LocalSafeWorkerAction ||
		effect.ResourceType != LocalSafeWorkerResourceType ||
		effect.OwnerIdentity != b.owner ||
		verification.Consumer != LocalSafeWorkerID {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	if err := validateScopedSafeWorkerEffect(effect); err != nil {
		return FinalEffect{}, err
	}
	if err := validateArtifactName(effect.ArtifactName); err != nil {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	root, err := filepath.Abs(strings.TrimSpace(effect.WorkspaceRoot))
	if err != nil || filepath.Clean(root) != effect.WorkspaceRoot {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	expectedTask := deriveTaskID(root, effect.ArtifactName, effect.PayloadDigest)
	if effect.TaskID != expectedTask {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	expectedDigest, err := digestFinalEffect(effect)
	if err != nil || !equalText(expectedDigest, effect.EffectDigest) {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	expectedPath := filepath.Join(root, effect.ArtifactName)
	if filepath.Clean(verification.ExecutionTarget) != expectedPath {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	binding := verification.Binding
	if binding.OwnerIdentity != b.owner ||
		binding.TaskID != effect.TaskID ||
		binding.Action != LocalSafeWorkerAction ||
		!equalText(binding.EffectDigest, effect.EffectDigest) {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	if !sameOperationScope(binding.OperationScope, effect.OperationScope) {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	return effect, nil
}

func (b *DurableAuthorizationBridge) authorizationRequest(
	effect FinalEffect,
) executionauth.Request {
	references := []string{"phase2:operation-ledger"}
	if scope := effect.OperationScope; scope != nil {
		references = append(references,
			fmt.Sprintf("operation:%s:version:%d", scope.OperationID, scope.Version),
			fmt.Sprintf("claim:%s:generation:%d", scope.ClaimOwner, scope.ClaimGeneration))
	}
	return executionauth.Request{
		OwnerIdentity:     b.owner,
		IdempotencyKey:    "safe-worker:" + effect.EffectDigest,
		ActorIdentity:     localSafeWorkerActor,
		ActorKind:         executionauth.ActorSystem,
		TaskID:            effect.TaskID,
		Action:            LocalSafeWorkerAction,
		Stage:             executionauth.StageExecution,
		ResourceType:      LocalSafeWorkerResourceType,
		ResourceID:        effect.EffectDigest,
		ProjectKey:        b.workspaceID,
		Domain:            "local_operations",
		ToolID:            localSafeWorkerTool,
		RuntimeID:         LocalSafeWorkerID,
		FolderPaths:       []string{effect.WorkspaceRoot},
		RequiredAuthority: 1,
		RequestedAutonomy: 8,
		Risk:              executionauth.RiskLow,
		Reversible:        true,
		EstimatedCostEUR:  0,
		EffectDigest:      effect.EffectDigest,
		SourceReferences:  references,
	}
}

func (b *DurableAuthorizationBridge) verifyReceipt(
	receipt executionauth.Receipt,
	effect FinalEffect,
) error {
	if receipt.Outcome != executionauth.OutcomeAuthorized ||
		receipt.OwnerIdentity != b.owner ||
		receipt.IdempotencyKey != "safe-worker:"+effect.EffectDigest ||
		receipt.ActorIdentity != localSafeWorkerActor ||
		receipt.ActorKind != executionauth.ActorSystem ||
		receipt.TaskID != effect.TaskID ||
		receipt.Action != LocalSafeWorkerAction ||
		receipt.Stage != executionauth.StageExecution ||
		receipt.ResourceType != LocalSafeWorkerResourceType ||
		receipt.ResourceID != effect.EffectDigest ||
		receipt.ProjectKey != b.workspaceID ||
		receipt.RuntimeID != LocalSafeWorkerID ||
		receipt.RequiredAuthority != 1 ||
		receipt.RequestedAutonomy != 8 ||
		receipt.Risk != executionauth.RiskLow ||
		!receipt.Reversible ||
		receipt.EstimatedCostEUR != 0 ||
		!equalText(receipt.EffectDigest, effect.EffectDigest) {
		return ErrAuthorizationMismatch
	}
	return nil
}

func deriveTaskID(root, artifactName, payloadDigest string) string {
	sum := sha256.Sum256([]byte(
		filepath.Clean(root) + "\x00" +
			artifactName + "\x00" +
			strings.ToLower(strings.TrimSpace(payloadDigest)),
	))
	return "phase2-safe-worker:" + hex.EncodeToString(sum[:])
}

func consumptionTarget(effect FinalEffect) string {
	sum := sha256.Sum256([]byte(
		filepath.Join(effect.WorkspaceRoot, effect.ArtifactName),
	))
	return "safe-worker-target:" + hex.EncodeToString(sum[:])
}

func equalText(left, right string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
