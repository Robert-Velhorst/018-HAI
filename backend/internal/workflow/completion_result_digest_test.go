package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestCompletionResultDigestPreservesFieldBoundariesAndOutputBytes(t *testing.T) {
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice"}
	now := time.Now().UTC()
	digest := func(output, failure string) string {
		t.Helper()
		record, err := newWorkflowCompletionAttestation(item, &TaskRunResult{
			PlanID: "test-plan", Passed: true, CompletionStatus: "validated",
			VerificationStatus: "verified", Output: output, FailureReason: failure,
		}, now)
		if err != nil {
			t.Fatal(err)
		}
		return record.ResultDigest
	}
	if digest("result\n", "") == digest("result", "\n") {
		t.Error("moving a newline between result fields produced the same digest")
	}
	if digest("result\xff", "") == digest("result\xfe", "") {
		t.Fatal("different opaque output bytes produced the same digest")
	}
	if digest("unchanged", "") != digest("unchanged", "") {
		t.Fatal("identical result content produced unstable digests")
	}
}

func TestCompletionAttestationStillValidatesLegacyResultDigest(t *testing.T) {
	now := time.Now().UTC()
	item := models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice", CompletedAt: &now}
	record, err := newWorkflowCompletionAttestation(item, &TaskRunResult{
		PlanID: "legacy-plan", Passed: true, CompletionStatus: "validated",
		VerificationStatus: "verified", Output: "old output",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	legacyResultDigest := sha256.Sum256([]byte("validated\nverified\nold output\n"))
	record.ResultDigest = hex.EncodeToString(legacyResultDigest[:])
	record.RecordDigest, err = workflowCompletionAttestationDigest(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWorkflowCompletionAttestation(item, record); err != nil {
		t.Fatalf("existing attestation format was invalidated: %v", err)
	}
}
