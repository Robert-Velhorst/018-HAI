package automation

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestApprovalProofCannotOutliveRecordedDecision(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: "Decision-bound mutation", URLPath: "decision-bound-mutation",
		LaunchType: "api", LaunchTarget: "POST http://localhost/mutate",
	})
	svc := newTestService(repo, events.Publisher{}).(*service)
	snapshot, err := svc.InspectReviewConfiguration(id)
	if err != nil {
		t.Fatalf("InspectReviewConfiguration: %v", err)
	}
	approvedAt := time.Now().UTC().Add(-maximumApprovalDecisionAge + time.Minute).
		Truncate(approvalProofTimestampPrecision)
	sourceID := "task-review:" + uuid.NewString()
	if err := svc.RecordApprovalDecision(id, TaskApprovalDecisionRequest{
		OwnerIdentity: "alice", Task: "Perform the reviewed mutation",
		ApprovalSourceID: sourceID, ApprovalBindingDigest: strings.Repeat("d", 64),
		ReviewConfiguration: snapshot, ApprovedAt: approvedAt,
	}); err != nil {
		t.Fatalf("RecordApprovalDecision: %v", err)
	}
	deadline := approvedAt.Add(maximumApprovalDecisionAge)
	// Advance the signer independently: relative TTLs must not extend the decision.
	svc.approvalProofs = newApprovalProofTestService(t, func() time.Time {
		return deadline.Add(-30 * time.Second)
	})
	proof, err := svc.IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity: "alice", Task: "Perform the reviewed mutation",
		ApprovalSourceID: sourceID,
	})
	if err != nil {
		t.Fatalf("IssueApprovalProof: %v", err)
	}
	if proof == nil {
		t.Fatal("IssueApprovalProof returned no proof")
	}
	if !proof.ExpiresAt.Equal(deadline) {
		t.Fatalf("proof expiry = %s, want decision deadline %s", proof.ExpiresAt, deadline)
	}
}
