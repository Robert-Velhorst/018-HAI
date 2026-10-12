package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func newWorkflowCompletionAttestation(
	item models.WorkflowItem,
	result *TaskRunResult,
	completedAt time.Time,
) (*models.WorkflowCompletionAttestation, error) {
	if result == nil || item.ID == uuid.Nil ||
		strings.TrimSpace(result.PlanID) == "" || strings.TrimSpace(result.VerificationStatus) == "" {
		return nil, fmt.Errorf("complete workflow result evidence is required")
	}
	if !result.Passed || result.ReviewRequired || result.ApprovalRequired ||
		(item.RequiresApproval && item.ApprovalStatus != "approved") || strings.TrimSpace(result.FailureReason) != "" ||
		!acceptsWorkflowCompletionStatus(result.CompletionStatus) {
		return nil, fmt.Errorf("workflow completion requires a passed, validated result with no review or approval requirement")
	}
	verificationStatus := strings.ToLower(strings.TrimSpace(result.VerificationStatus))
	if !acceptsWorkflowCompletionVerification(verificationStatus) {
		return nil, fmt.Errorf("workflow completion requires verified or test-passed evidence")
	}
	resultDigest := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(result.CompletionStatus),
		strings.TrimSpace(result.VerificationStatus),
		result.Output,
		result.FailureReason,
	}, "\n")))
	runtimeURI := strings.TrimSpace(result.RuntimeEvidenceURI)
	if result.ExternalActionExecuted {
		if !isWorkflowLaunchEvidenceURI(runtimeURI) {
			return nil, fmt.Errorf("workflow external action completion requires immutable runtime launch evidence")
		}
	} else if runtimeURI != "" {
		return nil, fmt.Errorf("workflow runtime evidence cannot be attached when no external action was executed")
	}
	if len(runtimeURI) > 2048 || safety.RedactSecrets(runtimeURI) != runtimeURI {
		return nil, fmt.Errorf("workflow runtime evidence URI is invalid")
	}
	evidenceDigest := sha256.Sum256([]byte(strings.Join([]string{
		runtimeURI,
		strings.TrimSpace(result.RuntimeEvidenceLabel),
	}, "\n")))
	runtimeID := ""
	if result.RuntimeRouteTrace != nil {
		runtimeID = strings.TrimSpace(result.RuntimeRouteTrace.RuntimeID)
	}
	if runtimeID == "" {
		runtimeID = "task-engine"
	}
	attestation := &models.WorkflowCompletionAttestation{
		ID: uuid.New(), WorkflowID: item.ID, OwnerIdentity: firstNonEmpty(strings.TrimSpace(item.OwnerIdentity), "system"),
		TaskPlanID: strings.TrimSpace(result.PlanID), CompletionStatus: "completed",
		VerificationStatus: verificationStatus,
		RuntimeID:          runtimeID, RuntimeEvidenceURI: runtimeURI, RuntimeEvidenceDigest: hex.EncodeToString(evidenceDigest[:]),
		ResultDigest: hex.EncodeToString(resultDigest[:]), CompletedAt: completedAt.UTC(),
		CreatedAt: completedAt.UTC(),
	}
	digest, err := workflowCompletionAttestationDigest(attestation)
	if err != nil {
		return nil, err
	}
	attestation.RecordDigest = digest
	return attestation, nil
}

func acceptsWorkflowCompletionVerification(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "verified", "test_passed":
		return true
	default:
		return false
	}
}

func acceptsWorkflowCompletionStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "validated")
}

func isWorkflowLaunchEvidenceURI(value string) bool {
	const prefix = "automation-launch://"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	launchID, err := uuid.Parse(strings.TrimPrefix(value, prefix))
	return err == nil && launchID != uuid.Nil
}

func workflowCompletionAttestationDigest(value *models.WorkflowCompletionAttestation) (string, error) {
	if value == nil {
		return "", fmt.Errorf("workflow completion attestation is required")
	}
	payload := struct {
		AttestationID, WorkflowID, OwnerIdentity, TaskPlanID, CompletionStatus, VerificationStatus string
		RuntimeID, RuntimeEvidenceURI, RuntimeEvidenceDigest, ResultDigest                       string
		CompletedAt, CreatedAt                                                                  string
	}{
		value.ID.String(), value.WorkflowID.String(), strings.TrimSpace(value.OwnerIdentity),
		strings.TrimSpace(value.TaskPlanID), strings.TrimSpace(value.CompletionStatus),
		strings.TrimSpace(value.VerificationStatus), strings.TrimSpace(value.RuntimeID), strings.TrimSpace(value.RuntimeEvidenceURI),
		strings.TrimSpace(value.RuntimeEvidenceDigest), strings.TrimSpace(value.ResultDigest),
		value.CompletedAt.UTC().Format(time.RFC3339Nano), value.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode workflow completion attestation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateWorkflowCompletionAttestation(item models.WorkflowItem, value *models.WorkflowCompletionAttestation) error {
	if value == nil || item.ID == uuid.Nil || item.CompletedAt == nil || value.ID == uuid.Nil ||
		value.WorkflowID != item.ID || !value.CompletedAt.UTC().Equal(item.CompletedAt.UTC()) {
		return fmt.Errorf("workflow completion attestation does not match the completed item")
	}
	wantOwner := firstNonEmpty(strings.TrimSpace(item.OwnerIdentity), "system")
	if strings.TrimSpace(value.OwnerIdentity) != wantOwner || strings.TrimSpace(value.TaskPlanID) == "" ||
		!strings.EqualFold(strings.TrimSpace(value.CompletionStatus), "completed") ||
		!acceptsWorkflowCompletionVerification(value.VerificationStatus) || strings.TrimSpace(value.RuntimeID) == "" ||
		value.CreatedAt.IsZero() || !isWorkflowCompletionDigest(value.RuntimeEvidenceDigest) ||
		!isWorkflowCompletionDigest(value.ResultDigest) || !isWorkflowCompletionDigest(value.RecordDigest) {
		return fmt.Errorf("workflow completion attestation fields are incomplete or inconsistent")
	}
	runtimeURI := strings.TrimSpace(value.RuntimeEvidenceURI)
	if len(runtimeURI) > 2048 || safety.RedactSecrets(runtimeURI) != runtimeURI ||
		(runtimeURI != "" && !isWorkflowLaunchEvidenceURI(runtimeURI)) {
		return fmt.Errorf("workflow completion runtime evidence is invalid")
	}
	digest, err := workflowCompletionAttestationDigest(value)
	if err != nil {
		return err
	}
	if digest != value.RecordDigest {
		return fmt.Errorf("workflow completion attestation digest does not match its record")
	}
	return nil
}

func isWorkflowCompletionDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
