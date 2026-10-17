package executionauth

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

func TestReceiptReferencesBindOpsControlReviewToItsDecision(t *testing.T) {
	itemID := uuid.New()
	decisionID := uuid.New()
	references, err := receiptReferencesFromEvidence(DecisionEvidence{
		Approval: ApprovalEvidence{
			SourceID:   "opscontrol-review:" + itemID.String(),
			DecisionID: decisionID.String(),
		},
	})
	if err != nil {
		t.Fatalf("resolve opscontrol review provenance: %v", err)
	}
	if got := referenceString(references.opscontrolReviewItemID); got != itemID.String() {
		t.Fatalf("opscontrol review item id = %q, want %q", got, itemID)
	}
	if got := referenceString(references.opscontrolReviewDecisionID); got != decisionID.String() {
		t.Fatalf("opscontrol review decision id = %q, want %q", got, decisionID)
	}
	if references.taskReviewDecisionID != nil || references.controlDecisionID != nil {
		t.Fatal("opscontrol review populated a different approval reference")
	}
}

func TestReceiptReferencesRejectMalformedOpsControlReviewProvenance(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		decisionID string
	}{
		{name: "malformed item", sourceID: "opscontrol-review:not-a-uuid", decisionID: uuid.NewString()},
		{name: "missing decision", sourceID: "opscontrol-review:" + uuid.NewString()},
		{name: "malformed decision", sourceID: "opscontrol-review:" + uuid.NewString(), decisionID: "not-a-uuid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := receiptReferencesFromEvidence(DecisionEvidence{
				Approval: ApprovalEvidence{
					SourceID:   test.sourceID,
					DecisionID: test.decisionID,
				},
			})
			if err == nil {
				t.Fatal("malformed opscontrol review provenance was accepted")
			}
		})
	}
}

func TestOpsControlReviewProvenanceMigrationKeepsOwnerAndDecisionBinding(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0127_opscontrol_review_receipt_provenance.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	upSQL := string(up)
	for _, required := range []string{
		"opscontrol_review_item_id uuid",
		"opscontrol_review_decision_id uuid",
		"FOREIGN KEY (",
		"UNIQUE (owner_identity, id, review_item_id)",
		"'opscontrol-review:' || opscontrol_review_item_id::text",
		"evidence_json #>> '{approval,decisionId}' = opscontrol_review_decision_id::text",
		"approval_source_id LIKE 'opscontrol-owner:%'",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}

	down, err := migrations.Files.ReadFile("pre/0127_opscontrol_review_receipt_provenance.down.sql")
	if err != nil {
		t.Fatalf("read rollback: %v", err)
	}
	downSQL := string(down)
	if !strings.Contains(downSQL, "cannot remove opscontrol review provenance") ||
		!strings.Contains(downSQL, "approval_source_id LIKE 'opscontrol-review:%'") ||
		!strings.Contains(downSQL, "approval_source_id LIKE 'opscontrol-owner:%'") {
		t.Fatal("rollback does not protect review receipts and owner approvals")
	}
}
