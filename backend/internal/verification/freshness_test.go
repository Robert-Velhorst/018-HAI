package verification

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"

	"github.com/google/uuid"
)

func TestStaleEvidenceCannotMarkClaimSourceSupported(t *testing.T) {
	runID := uuid.New()
	claimText := "The review meeting is scheduled for Friday."
	claims := verifyClaims(
		[]models.VerificationClaim{{RunID: runID, ClaimText: claimText}},
		[]models.VerificationEvidence{{
			RunID: runID, SourceType: "connected_source", SourceID: "meeting-notice",
			SourceURI: "https://calendar.example/meeting/42", Snippet: claimText,
			Authority: authorityConnectedProvenance, Freshness: " STALE ",
			QualityScore: 0.95, Used: true,
		}},
		AnswerRequest{},
		ModeGrounded,
	)

	if len(claims) != 1 {
		t.Fatalf("verified claims = %d, want 1", len(claims))
	}
	if claims[0].Status != StatusNeedsReview || !claims[0].NeedsReview {
		t.Fatalf("stale evidence produced claim status %q (needsReview=%t), want %q and review required", claims[0].Status, claims[0].NeedsReview, StatusNeedsReview)
	}
	if !strings.Contains(strings.ToLower(claims[0].SupportExplanation), "stale") {
		t.Fatalf("claim does not explain its stale-source review state: %q", claims[0].SupportExplanation)
	}
	if got := runStatus(claims); got != StatusNeedsReview {
		t.Fatalf("run status = %q, want %q for stale supporting evidence", got, StatusNeedsReview)
	}
}

func TestFreshEvidenceCanStillSourceSupportClaim(t *testing.T) {
	runID := uuid.New()
	claimText := "The review meeting is scheduled for Friday."
	claims := verifyClaims(
		[]models.VerificationClaim{{RunID: runID, ClaimText: claimText}},
		[]models.VerificationEvidence{{
			RunID: runID, SourceType: "connected_source", SourceID: "meeting-notice",
			SourceURI: "https://calendar.example/meeting/42", Snippet: claimText,
			Authority: authorityConnectedProvenance, Freshness: "fresh",
			QualityScore: 0.95, Used: true,
		}},
		AnswerRequest{},
		ModeGrounded,
	)

	if len(claims) != 1 || claims[0].Status != StatusSourceSupported || claims[0].NeedsReview {
		t.Fatalf("fresh evidence claim = %#v, want source_supported without review", claims)
	}
}

func TestStaleConnectedSourceCannotSupportOrPromoteClaim(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	claimText := "The review meeting is scheduled for Friday."
	extraction := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "hai",
		ContentType: "email", Text: claimText, Summary: claimText,
		SourceURI: "https://mail.example/message/42", SourceLabel: "Review meeting",
		ContentHash: strings.Repeat("a", 64), UpdatedAt: now,
	}
	snapshot := sourceevidence.Snapshot{
		OwnerIdentity: "alice", ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
		RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey,
		RawProjectKey: extraction.ProjectKey, ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
		ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
		ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
		FetchedAt:               now.Add(-31 * 24 * time.Hour), ExtractionAt: extraction.UpdatedAt,
		ConnectorKey: "gmail",
	}
	snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
	searcher := staticConnectedSourceSearcher{result: &source.SearchResult{UsedContext: []source.RankedExtraction{{
		Extraction: extraction, Score: 0.95,
	}}}}
	memoryRepository := &capturingMemoryRepository{}
	service := NewServiceWithEvidenceResolvers(
		&fakeVerificationRepository{}, searcher, memory.NewService(memoryRepository), nil,
		staticSourceEvidenceRepository{snapshot: snapshot},
	)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "When is the review meeting?",
		DraftAnswer: claimText, Mode: ModeGrounded, AllowMemoryUpdate: true,
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Freshness != "stale" {
		t.Fatalf("evidence freshness = %#v, want one stale source snapshot", result.Evidence)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusNeedsReview || !result.Claims[0].NeedsReview {
		t.Fatalf("stale connected source produced claim %#v, want needs_review", result.Claims)
	}
	if result.Claims[0].SourceRefs != extraction.SourceURI || !strings.Contains(strings.ToLower(result.Claims[0].SupportExplanation), "stale") {
		t.Fatalf("stale source provenance/review reason was lost: %#v", result.Claims[0])
	}
	if result.Run.Status != StatusNeedsReview {
		t.Fatalf("run status = %q, want %q", result.Run.Status, StatusNeedsReview)
	}
	if len(memoryRepository.created) != 0 || result.MemoryUpdatesStored != 0 {
		t.Fatalf("claim backed only by stale evidence entered durable memory: result=%#v memory=%#v", result, memoryRepository.created)
	}
}
