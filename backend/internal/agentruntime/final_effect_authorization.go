package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const runtimeExecuteTaskOperation = "agent-runtime.execute-task"

// FinalEffectAuthorizationRequest is the immutable, content-bounded request
// presented at the last boundary before a runtime adapter can cause effects.
// The prompt itself is deliberately excluded; its digest binds authorization
// without copying potentially sensitive task content into another subsystem.
type FinalEffectAuthorizationRequest struct {
	Operation        string
	RuntimeID        string
	TaskID           string
	OwnerIdentity    string
	ProjectKey       string
	PromptDigest     string
	ApprovalSourceID string
	RequiresApproval bool
	RuntimeModel     string `json:",omitempty"`
}

// FinalEffectAuthorizationProof carries references to authority already issued
// and atomically consumed upstream. It does not grant authority by itself.
// RuntimeRequestDigest binds that receipt to the exact request reconstructed by
// this final boundary; the other digests bind it to the durable receipt.
type FinalEffectAuthorizationProof struct {
	ReceiptID                  string
	AuthorizationRequestDigest string
	DecisionDigest             string
	RuntimeRequestDigest       string
	RuntimeProof               string
}

// FinalEffectProofVerifier verifies existing durable authorization; it must not
// issue or consume another authorization. A production implementation must:
//   - load ReceiptID from trusted storage rather than trust proof fields;
//   - require an allowed, already-consumed, single-use receipt;
//   - compare the stored request and decision digests to the proof;
//   - compare owner, action, runtime, task, project, and approval provenance to
//     FinalEffectAuthorizationRequest; and
//   - atomically reject replay of the same receipt/runtime binding. This is a
//     final proof exercise marker, not a second policy authorization.
//   - fail closed when receipt or emergency-stop state cannot be read.
type FinalEffectProofVerifier interface {
	VerifyFinalEffectProof(context.Context, FinalEffectAuthorizationRequest, FinalEffectAuthorizationProof) error
}

type FinalEffectProofVerifierFunc func(context.Context, FinalEffectAuthorizationRequest, FinalEffectAuthorizationProof) error

func (f FinalEffectProofVerifierFunc) VerifyFinalEffectProof(
	ctx context.Context,
	request FinalEffectAuthorizationRequest,
	proof FinalEffectAuthorizationProof,
) error {
	return f(ctx, request, proof)
}

// Keep adapter inputs identical to the scope values bound by the final-effect request.
func canonicalizeRuntimeTaskScope(task Task) Task {
	task.ID = strings.TrimSpace(task.ID)
	task.OwnerIdentity = strings.TrimSpace(task.OwnerIdentity)
	task.ProjectKey = strings.TrimSpace(task.ProjectKey)
	task.ApprovalSourceID = strings.TrimSpace(task.ApprovalSourceID)
	return task
}

func runtimeFinalEffectRequest(runtimeID string, task Task, info Info) FinalEffectAuthorizationRequest {
	sum := sha256.Sum256([]byte(task.Prompt))
	return FinalEffectAuthorizationRequest{
		Operation:        runtimeExecuteTaskOperation,
		RuntimeID:        strings.ToLower(strings.TrimSpace(runtimeID)),
		TaskID:           strings.TrimSpace(task.ID),
		OwnerIdentity:    strings.TrimSpace(task.OwnerIdentity),
		ProjectKey:       strings.TrimSpace(task.ProjectKey),
		PromptDigest:     hex.EncodeToString(sum[:]),
		ApprovalSourceID: strings.TrimSpace(task.ApprovalSourceID),
		RequiresApproval: info.RequiresApproval,
		RuntimeModel:     task.RuntimeModel,
	}
}

func finalEffectRequestDigest(request FinalEffectAuthorizationRequest) string {
	payload, err := json.Marshal(request)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func validateFinalEffectAuthorizationProof(
	request FinalEffectAuthorizationRequest,
	proof FinalEffectAuthorizationProof,
) error {
	receiptID, err := uuid.Parse(strings.TrimSpace(proof.ReceiptID))
	if err != nil || receiptID == uuid.Nil {
		return fmt.Errorf("authorization receipt is invalid")
	}
	runtimeRequestDigest := strings.ToLower(strings.TrimSpace(proof.RuntimeRequestDigest))
	if runtimeRequestDigest == "" || runtimeRequestDigest != finalEffectRequestDigest(request) {
		return fmt.Errorf("runtime request binding is invalid")
	}
	for label, digest := range map[string]string{
		"authorization request":  proof.AuthorizationRequestDigest,
		"authorization decision": proof.DecisionDigest,
	} {
		decoded, decodeErr := hex.DecodeString(strings.ToLower(strings.TrimSpace(digest)))
		if decodeErr != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("%s digest is invalid", label)
		}
	}
	return nil
}

func finalEffectDeniedResult(runtimeID string, reason string, unavailable bool) Result {
	message := "agent runtime final-effect authorization proof was rejected; no runtime adapter was invoked"
	auditEvent := "final-effect authorization proof rejected before runtime adapter access"
	if unavailable {
		message = "agent runtime final-effect authorization proof could not be verified; no runtime adapter was invoked"
		auditEvent = "final-effect proof verifier failed closed before runtime adapter access"
	}
	if strings.TrimSpace(reason) != "" {
		message += ": " + safety.RedactSecrets(strings.TrimSpace(reason))
	}
	return blockedRuntimeResult(runtimeID, message, auditEvent)
}
