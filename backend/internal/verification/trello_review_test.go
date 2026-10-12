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

func TestUncertainTrelloEvidenceRemainsReviewableButCannotSupportOrPromoteMemory(t *testing.T) {
	for name, uncertain := range map[string]bool{
		"marked uncertain":               true,
		"manually corrected uncertainty": false,
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, time.September, 20, 11, 0, 0, 0, time.UTC)
			extraction := models.SourceExtraction{
				ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "018-HAI",
				ContentType: "trello_card", Text: "The board review is scheduled for Friday.",
				Summary: "The board review is scheduled for Friday.", SourceURI: "https://trello.com/c/card1234",
				SourceLabel: "Board review", ContentHash: strings.Repeat("a", 64),
				Uncertain: uncertain, UpdatedAt: now,
			}
			snapshot := sourceevidence.Snapshot{
				OwnerIdentity: "alice", ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
				RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey,
				RawProjectKey: extraction.ProjectKey, ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
				ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
				ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
				FetchedAt:               now.Add(-time.Hour), ExtractionAt: extraction.UpdatedAt, ConnectorKey: "trello",
			}
			snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
			searcher := staticConnectedSourceSearcher{result: &source.SearchResult{UsedContext: []source.RankedExtraction{{
				Extraction: extraction, Score: 0.95, RequiresReview: uncertain,
			}}}}
			memoryRepo := &capturingMemoryRepository{}
			service := NewServiceWithEvidenceResolvers(
				&fakeVerificationRepository{}, searcher, memory.NewService(memoryRepo), nil,
				staticSourceEvidenceRepository{snapshot: snapshot},
			)

			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice", ProjectKey: extraction.ProjectKey,
				Question: "What is the board review date?", DraftAnswer: extraction.Summary,
				Mode: ModeGrounded, AllowMemoryUpdate: true,
				ExternalEvidence: []EvidenceInput{{
					SourceType: "connected_source", SourceID: extraction.ID.String(),
					SourceURI: extraction.SourceURI, SourceLabel: extraction.SourceLabel + " (review required)",
					Snippet: extraction.Summary,
				}},
			})
			if err != nil {
				t.Fatalf("Answer: %v", err)
			}
			if len(result.Evidence) != 1 {
				t.Fatalf("evidence = %#v, want the Trello record retained for inspection", result.Evidence)
			}
			evidence := result.Evidence[0]
			if !evidence.Rejected || evidence.Used || evidence.Authority != authorityConnectedReviewRequired {
				t.Fatalf("Trello evidence was accepted as verification evidence: %#v", evidence)
			}
			if evidence.SourceURI != extraction.SourceURI || evidence.SourceLabel != extraction.SourceLabel || evidence.Snippet == "" || !strings.Contains(evidence.RejectReason, "owner review") {
				t.Fatalf("reviewable Trello provenance/context was lost: %#v", evidence)
			}
			if len(result.Claims) != 1 || !result.Claims[0].NeedsReview || result.Claims[0].Status == StatusSourceSupported || result.Claims[0].Status == StatusVerified {
				t.Fatalf("Trello-supported claim was not held for review: %#v", result.Claims)
			}
			if result.Run.Status != StatusNeedsReview {
				t.Fatalf("verification run status = %q, want %q", result.Run.Status, StatusNeedsReview)
			}
			if len(memoryRepo.created) != 0 {
				t.Fatalf("uncertain Trello claim was promoted to memory despite AllowMemoryUpdate: %#v", memoryRepo.created)
			}
		})
	}
}

func TestMemoryPromotionSkipsReviewOnlyTrelloClaimWithoutBlockingOtherEvidence(t *testing.T) {
	memoryRepo := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepo)).(*service)
	runID := uuid.New()
	request := AnswerRequest{OwnerIdentity: "alice", ProjectKey: "018-HAI"}
	run := &models.VerificationRun{ID: runID, OwnerIdentity: "alice"}
	evidence := []models.VerificationEvidence{
		{
			ID:         uuid.New(),
			RunID:      runID,
			SourceType: "connected_source", SourceID: "trello-extraction",
			SourceURI: "https://trello.com/c/card1234", SourceLabel: "Trello card",
			Snippet:   "review the board card",
			Authority: authorityConnectedReviewRequired, Rejected: true,
		},
		{
			ID:         uuid.New(),
			RunID:      runID,
			SourceType: "connected_source", SourceID: "mail-extraction",
			SourceURI: "https://mail.example/message/42", SourceLabel: "Project email",
			Snippet:   "Email claim",
			Authority: authorityConnectedProvenance, QualityScore: 0.9, Used: true,
		},
	}
	claims := []models.VerificationClaim{
		{ID: uuid.New(), RunID: runID, ClaimText: "Trello claim", Status: StatusSourceSupported, SourceRefs: "https://trello.com/c/card1234", Confidence: 0.9},
		{ID: uuid.New(), RunID: runID, ClaimText: "Email claim", Status: StatusSourceSupported, SourceRefs: "https://mail.example/message/42", Confidence: 0.9},
	}

	outcome := service.storeVerifiedMemory(request, run, claims, evidence)
	if outcome.Stored != 1 || outcome.Skipped != 1 || len(memoryRepo.created) != 1 || memoryRepo.created[0].Content != "Email claim" || memoryRepo.created[0].SourceURI != "https://mail.example/message/42" {
		t.Fatalf("review-only evidence blocked unrelated memory or promoted Trello: outcome=%#v memory=%#v", outcome, memoryRepo.created)
	}
}
