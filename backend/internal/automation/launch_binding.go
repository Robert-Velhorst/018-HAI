package automation

import (
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
)

func captureLaunchActionBinding(automation *models.Automation, request TaskLaunchRequest) TaskLaunchRequest {
	if request.ApprovalProof != nil {
		proof := *request.ApprovalProof
		request.ApprovalProof = &proof
	}
	request.launchActionDigest = automationActionDigest(automation, request)
	return request
}

// Approval consumption does not freeze the process policy. Recheck the captured
// intent and reviewed metadata at dispatch without attempting to consume again.
func validateLaunchActionBinding(automation *models.Automation, request TaskLaunchRequest) error {
	if automation == nil || request.launchActionDigest == "" {
		return fmt.Errorf("immutable launch action binding is unavailable")
	}
	if request.ApprovalProof == nil {
		return ErrApprovalProofRequired
	}
	proof := request.ApprovalProof
	if proof.ActionDigest != request.launchActionDigest ||
		automationActionDigest(automation, request) != request.launchActionDigest {
		return fmt.Errorf("launch action digest mismatch: configuration or execution policy changed")
	}
	if strings.ToLower(strings.TrimSpace(request.ApprovalBindingDigest)) != request.launchActionDigest {
		return fmt.Errorf("approval binding digest mismatch at launch boundary")
	}
	scope, required := approvalScopeForAutomation(automation)
	if !required || proof.Scope != scope {
		return fmt.Errorf("%w: scope mismatch", ErrApprovalProofInvalid)
	}
	if proof.AutomationID != automation.ID {
		return fmt.Errorf("%w: automation mismatch", ErrApprovalProofInvalid)
	}
	if proof.OwnerIdentity != strings.TrimSpace(request.OwnerIdentity) {
		return fmt.Errorf("%w: owner mismatch", ErrApprovalProofInvalid)
	}
	if proof.ApprovalSourceID != strings.TrimSpace(request.ApprovalSourceID) {
		return fmt.Errorf("%w: approval source mismatch", ErrApprovalProofInvalid)
	}
	if !time.Now().UTC().Before(proof.ExpiresAt) {
		return ErrApprovalProofExpired
	}
	return nil
}
