package workflow

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestNewWorkflowCompletionAttestationRejectsUnverifiedOrModelOnlyResults(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "robert"}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	validResult := func() *TaskRunResult {
		return &TaskRunResult{
			PlanID: "plan-verified", CompletionStatus: "validated",
			VerificationStatus: "verified", Output: "The checked result.", Passed: true,
		}
	}

	tests := []struct {
		name   string
		mutate func(*TaskRunResult)
	}{
		{name: "validation did not pass", mutate: func(result *TaskRunResult) { result.Passed = false }},
		{name: "completion is not validated", mutate: func(result *TaskRunResult) { result.CompletionStatus = "review_required" }},
		{name: "review remains required", mutate: func(result *TaskRunResult) { result.ReviewRequired = true }},
		{name: "failure reason remains", mutate: func(result *TaskRunResult) { result.FailureReason = "completion validation failed" }},
		{name: "verification needs review", mutate: func(result *TaskRunResult) { result.VerificationStatus = "needs_review" }},
		{name: "verification is uncertain", mutate: func(result *TaskRunResult) { result.VerificationStatus = "uncertain" }},
		{name: "model-only status", mutate: func(result *TaskRunResult) {
			result.VerificationStatus = "model_only"
			result.Output = "The model says this is complete, without independent verification."
		}},
		{name: "source supported is not completion verification", mutate: func(result *TaskRunResult) {
			result.VerificationStatus = "source_supported"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validResult()
			test.mutate(result)
			if attestation, err := newWorkflowCompletionAttestation(item, result, now); err == nil {
				t.Fatalf("created attestation from ineligible result: %#v", attestation)
			}
		})
	}
}

func TestNewWorkflowCompletionAttestationDoesNotInventRuntimeEvidence(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "robert"}
	result := &TaskRunResult{
		PlanID: "plan-with-verified-output", CompletionStatus: "validated",
		VerificationStatus: "test_passed", Output: "Deterministic checks passed.", Passed: true,
	}

	attestation, err := newWorkflowCompletionAttestation(item, result, time.Now())
	if err != nil {
		t.Fatalf("newWorkflowCompletionAttestation: %v", err)
	}
	if attestation.RuntimeEvidenceURI != "" {
		t.Fatalf("runtime evidence URI = %q, want no fabricated URI when no runtime action occurred", attestation.RuntimeEvidenceURI)
	}
	if attestation.RuntimeEvidenceDigest == "" || attestation.ResultDigest == "" || attestation.RecordDigest == "" {
		t.Fatalf("attestation digests must remain populated: %#v", attestation)
	}
	if err := validateWorkflowCompletionAttestation(models.WorkflowItem{
		ID: item.ID, OwnerIdentity: item.OwnerIdentity, CurrentState: StateCompleted, CompletedAt: &attestation.CompletedAt,
	}, attestation); err != nil {
		t.Fatalf("valid completion attestation rejected: %v", err)
	}
}

func TestValidateWorkflowCompletionAttestationRejectsOwnerAndDigestTampering(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "robert"}
	completedAt := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	result := &TaskRunResult{
		PlanID: "plan-verified", CompletionStatus: "validated",
		VerificationStatus: "verified", Output: "The checked result.", Passed: true,
	}
	attestation, err := newWorkflowCompletionAttestation(item, result, completedAt)
	if err != nil {
		t.Fatalf("newWorkflowCompletionAttestation: %v", err)
	}
	item.CurrentState = StateCompleted
	item.CompletedAt = &completedAt

	for _, test := range []struct {
		name   string
		mutate func(*models.WorkflowItem, *models.WorkflowCompletionAttestation)
	}{
		{name: "owner mismatch", mutate: func(_ *models.WorkflowItem, record *models.WorkflowCompletionAttestation) { record.OwnerIdentity = "another-owner" }},
		{name: "workflow mismatch", mutate: func(_ *models.WorkflowItem, record *models.WorkflowCompletionAttestation) { record.WorkflowID = uuid.New() }},
		{name: "attestation id changed after digest", mutate: func(_ *models.WorkflowItem, record *models.WorkflowCompletionAttestation) { record.ID = uuid.New() }},
		{name: "attested time changed after digest", mutate: func(_ *models.WorkflowItem, record *models.WorkflowCompletionAttestation) { record.CreatedAt = record.CreatedAt.Add(time.Second) }},
		{name: "result digest malformed", mutate: func(_ *models.WorkflowItem, record *models.WorkflowCompletionAttestation) { record.ResultDigest = "not-a-digest" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copyItem := item
			copyAttestation := *attestation
			test.mutate(&copyItem, &copyAttestation)
			if err := validateWorkflowCompletionAttestation(copyItem, &copyAttestation); err == nil {
				t.Fatal("tampered completion attestation was accepted")
			}
		})
	}
}

func TestNewWorkflowCompletionAttestationRequiresImmutableEvidenceForExternalAction(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "robert"}
	result := &TaskRunResult{
		PlanID: "plan-external", CompletionStatus: "validated",
		VerificationStatus: "verified", Output: "Action completed.", Passed: true,
		ExternalActionExecuted: true,
	}

	if attestation, err := newWorkflowCompletionAttestation(item, result, time.Now()); err == nil {
		t.Fatalf("created attestation for external action without runtime event evidence: %#v", attestation)
	}

	result.RuntimeEvidenceURI = "model-output://invented"
	if attestation, err := newWorkflowCompletionAttestation(item, result, time.Now()); err == nil {
		t.Fatalf("created attestation from non-launch runtime evidence: %#v", attestation)
	}

	result.RuntimeEvidenceURI = "automation-launch://" + uuid.NewString()
	attestation, err := newWorkflowCompletionAttestation(item, result, time.Now())
	if err != nil {
		t.Fatalf("valid immutable launch event rejected: %v", err)
	}
	if attestation.RuntimeEvidenceURI != result.RuntimeEvidenceURI {
		t.Fatalf("runtime evidence URI = %q, want %q", attestation.RuntimeEvidenceURI, result.RuntimeEvidenceURI)
	}
}
