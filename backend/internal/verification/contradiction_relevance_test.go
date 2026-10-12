package verification

import (
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestContradictionDetectionRequiresEvidenceRelevantToClaim(t *testing.T) {
	runID := uuid.New()
	claimText := "The bridge replacement project was approved and enabled."
	evidence := []models.VerificationEvidence{
		{
			RunID: runID, SourceType: "connected_source", SourceID: "bridge-approval",
			SourceURI: "https://example.test/bridge-approval", Snippet: claimText,
			Authority: authorityConnectedProvenance, QualityScore: 0.95, Used: true,
		},
		{
			RunID: runID, SourceType: "connected_source", SourceID: "mailbox-migration",
			SourceURI: "https://example.test/mailbox-migration",
			Snippet:   "The mailbox migration request was rejected and disabled.",
			Authority: authorityConnectedProvenance, QualityScore: 0.95, Used: true,
		},
	}

	claims := verifyClaims(
		[]models.VerificationClaim{{RunID: runID, ClaimText: claimText}},
		evidence,
		AnswerRequest{Question: "What happened to the bridge replacement project?"},
		ModeGrounded,
	)
	if len(claims) != 1 {
		t.Fatalf("verified claims = %d, want 1", len(claims))
	}
	if claims[0].Status != StatusSourceSupported || claims[0].NeedsReview {
		t.Fatalf("unrelated opposite-polarity evidence changed claim status: %#v", claims[0])
	}

	conflicting := append([]models.VerificationEvidence(nil), evidence...)
	conflicting[1].SourceID = "bridge-rejection"
	conflicting[1].SourceURI = "https://example.test/bridge-rejection"
	conflicting[1].Snippet = "The bridge replacement project was rejected and disabled."
	claims = verifyClaims(
		[]models.VerificationClaim{{RunID: runID, ClaimText: claimText}},
		conflicting,
		AnswerRequest{Question: "What happened to the bridge replacement project?"},
		ModeGrounded,
	)
	if len(claims) != 1 || claims[0].Status != StatusConflicting || !claims[0].NeedsReview {
		t.Fatalf("relevant opposite-polarity evidence did not produce a reviewable conflict: %#v", claims)
	}
}
