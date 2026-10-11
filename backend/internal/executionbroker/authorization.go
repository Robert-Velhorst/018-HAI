package executionbroker

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

const (
	// LocalSafeWorkerAction is the exact action an upstream authorization
	// receipt must permit before this package creates a workspace artifact.
	LocalSafeWorkerAction = "executionbroker.local-safe-worker.write"
	// LocalSafeWorkerResourceType is the executionauth resource type used by
	// the production adapter. The receipt ResourceID must equal EffectDigest.
	LocalSafeWorkerResourceType = "executionbroker.final-effect"

	authorizationContractVersion          = 1
	operationAuthorizationContractVersion = 2
)

var (
	ErrAuthorizationRequired = errors.New("execution authorization is required")
	ErrAuthorizationDenied   = errors.New("execution authorization was denied")
	ErrAuthorizationMismatch = errors.New("execution authorization does not match the exact effect")
)

// AuthorizationBinding is a server-issued reference to an authorization
// receipt. It is not authority by itself: AuthorizationVerifier must resolve
// it from durable, owner-scoped state, recheck mutable policy (including the
// emergency stop), and atomically consume it.
type AuthorizationBinding struct {
	OwnerIdentity  string                      `json:"ownerIdentity"`
	TaskID         string                      `json:"taskId"`
	Action         string                      `json:"action"`
	ReceiptID      string                      `json:"receiptId"`
	ReceiptDigest  string                      `json:"receiptDigest"`
	EffectDigest   string                      `json:"effectDigest"`
	OperationScope *operations.SafeEffectScope `json:"operationScope,omitempty"`
}

// FinalEffect is the canonical, non-secret description of the filesystem
// effect. PayloadDigest is a SHA-256 digest; the marker itself is not exposed
// to the verifier or authorization logs.
type FinalEffect struct {
	ContractVersion int                         `json:"contractVersion"`
	RuntimeID       string                      `json:"runtimeId"`
	Action          string                      `json:"action"`
	ResourceType    string                      `json:"resourceType"`
	OwnerIdentity   string                      `json:"ownerIdentity"`
	TaskID          string                      `json:"taskId"`
	WorkspaceRoot   string                      `json:"workspaceRoot"`
	ArtifactName    string                      `json:"artifactName"`
	PayloadDigest   string                      `json:"payloadDigest"`
	EffectDigest    string                      `json:"effectDigest"`
	OperationScope  *operations.SafeEffectScope `json:"operationScope,omitempty"`
}

// AuthorizationVerification is the final-boundary request. Consumer and
// ExecutionTarget are the values the upstream adapter must persist when it
// atomically consumes the authorization receipt.
type AuthorizationVerification struct {
	Binding         AuthorizationBinding `json:"binding"`
	Effect          FinalEffect          `json:"effect"`
	Consumer        string               `json:"consumer"`
	ExecutionTarget string               `json:"executionTarget"`
}

// VerifiedAuthorization is returned only after the upstream adapter has
// durably resolved, rechecked, and atomically consumed the receipt.
type VerifiedAuthorization struct {
	OwnerIdentity string `json:"ownerIdentity"`
	TaskID        string `json:"taskId"`
	Action        string `json:"action"`
	ReceiptID     string `json:"receiptId"`
	ReceiptDigest string `json:"receiptDigest"`
	EffectDigest  string `json:"effectDigest"`
}

// AuthorizationVerifier is the production integration boundary for the
// executionauth service. Implementations must:
//   - resolve ReceiptID under Binding.OwnerIdentity from server-side state;
//   - require an authorized receipt whose owner, task, action, receipt digest,
//     ResourceType=LocalSafeWorkerResourceType, and ResourceID=EffectDigest
//     match this request;
//   - recheck emergency stop and all mutable policy immediately in this call;
//   - atomically consume the receipt once for Consumer and ExecutionTarget;
//   - return ErrAuthorizationDenied (or another error) on any uncertainty.
//
// The broker calls this method immediately before its first filesystem
// mutation. A nil verifier always fails closed.
type AuthorizationVerifier interface {
	VerifyAndConsume(context.Context, AuthorizationVerification) (VerifiedAuthorization, error)
}

// AuthorizationIssuer derives a server-owned binding for one exact local-safe
// effect. Implementations must discard input.Authorization.
type AuthorizationIssuer interface {
	Issue(context.Context, string, SafeWorkerInput) (SafeWorkerInput, error)
}

func buildFinalEffect(workspaceRoot string, in SafeWorkerInput) (FinalEffect, error) {
	binding := in.Authorization
	if err := validateAuthorizationBinding(binding); err != nil {
		return FinalEffect{}, err
	}
	root, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return FinalEffect{}, fmt.Errorf("canonicalize workspace root: %w", err)
	}
	root = filepath.Clean(root)
	payloadHash := sha256.Sum256([]byte(in.Marker))
	effect := FinalEffect{
		ContractVersion: effectContractVersion(binding.OperationScope),
		RuntimeID:       LocalSafeWorkerID,
		Action:          LocalSafeWorkerAction,
		ResourceType:    LocalSafeWorkerResourceType,
		OwnerIdentity:   strings.TrimSpace(binding.OwnerIdentity),
		TaskID:          strings.TrimSpace(binding.TaskID),
		WorkspaceRoot:   root,
		ArtifactName:    in.ArtifactName,
		PayloadDigest:   hex.EncodeToString(payloadHash[:]),
		OperationScope:  binding.OperationScope,
	}
	digest, err := digestFinalEffect(effect)
	if err != nil {
		return FinalEffect{}, err
	}
	effect.EffectDigest = digest
	if !equalDigest(binding.EffectDigest, digest) {
		return FinalEffect{}, ErrAuthorizationMismatch
	}
	return effect, nil
}

// BindLocalSafeWorkerEffect returns the exact digest an upstream authorization
// adapter must bind into its server-side executionauth request. OwnerIdentity,
// TaskID, and Action must already be populated in in.Authorization; receipt
// fields are not needed for digest construction.
func BindLocalSafeWorkerEffect(workspaceRoot string, in SafeWorkerInput) (string, error) {
	binding := in.Authorization
	if strings.TrimSpace(workspaceRoot) == "" {
		return "", fmt.Errorf("safe worker: workspace root not configured")
	}
	if strings.TrimSpace(in.Marker) == "" {
		return "", fmt.Errorf("safe worker: marker required")
	}
	if err := validateArtifactName(in.ArtifactName); err != nil {
		return "", err
	}
	root, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	if err := validateEffectIdentity(binding); err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256([]byte(in.Marker))
	effect := FinalEffect{
		ContractVersion: effectContractVersion(binding.OperationScope),
		RuntimeID:       LocalSafeWorkerID,
		Action:          LocalSafeWorkerAction,
		ResourceType:    LocalSafeWorkerResourceType,
		OwnerIdentity:   strings.TrimSpace(binding.OwnerIdentity),
		TaskID:          strings.TrimSpace(binding.TaskID),
		WorkspaceRoot:   filepath.Clean(root),
		ArtifactName:    in.ArtifactName,
		PayloadDigest:   hex.EncodeToString(payloadHash[:]),
		OperationScope:  binding.OperationScope,
	}
	return digestFinalEffect(effect)
}

func validateAuthorizationBinding(binding AuthorizationBinding) error {
	if err := validateEffectIdentity(binding); err != nil {
		return err
	}
	if _, err := uuid.Parse(strings.TrimSpace(binding.ReceiptID)); err != nil {
		return fmt.Errorf("%w: receipt id is invalid", ErrAuthorizationRequired)
	}
	if !validDigest(binding.ReceiptDigest) || !validDigest(binding.EffectDigest) {
		return fmt.Errorf("%w: receipt or effect digest is invalid", ErrAuthorizationRequired)
	}
	return nil
}

func validateEffectIdentity(binding AuthorizationBinding) error {
	if strings.TrimSpace(binding.OwnerIdentity) == "" ||
		strings.TrimSpace(binding.TaskID) == "" {
		return fmt.Errorf("%w: owner and task are required", ErrAuthorizationRequired)
	}
	if binding.Action != LocalSafeWorkerAction {
		return fmt.Errorf("%w: action is not permitted by this worker", ErrAuthorizationMismatch)
	}
	return nil
}

func digestFinalEffect(effect FinalEffect) (string, error) {
	effect.EffectDigest = ""
	encoded, err := json.Marshal(effect)
	if err != nil {
		return "", fmt.Errorf("encode final effect: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func effectContractVersion(scope *operations.SafeEffectScope) int {
	if scope != nil {
		return operationAuthorizationContractVersion
	}
	return authorizationContractVersion
}

func verifyGrant(binding AuthorizationBinding, effect FinalEffect, grant VerifiedAuthorization) error {
	if strings.TrimSpace(grant.OwnerIdentity) != strings.TrimSpace(binding.OwnerIdentity) ||
		strings.TrimSpace(grant.TaskID) != strings.TrimSpace(binding.TaskID) ||
		grant.Action != binding.Action ||
		grant.ReceiptID != binding.ReceiptID ||
		!equalDigest(grant.ReceiptDigest, binding.ReceiptDigest) ||
		!equalDigest(grant.EffectDigest, effect.EffectDigest) {
		return ErrAuthorizationMismatch
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func equalDigest(left, right string) bool {
	if !validDigest(left) || !validDigest(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
