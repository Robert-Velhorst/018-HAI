package verification

import (
	"testing"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type incompleteVerifiedMemoryWriter struct {
	memory.Service
	result *models.ContextMemory
}

func (w incompleteVerifiedMemoryWriter) CreateVerifiedFactForOwner(string, memory.CreateRequest, memory.VerifiedFactProvenance) (*models.ContextMemory, error) {
	return w.result, nil
}

func TestAnswerRejectsMissingDurableMemoryReceipt(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *models.ContextMemory
	}{
		{name: "nil record"},
		{name: "record without identifier", result: &models.ContextMemory{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
				return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
			})
			base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, incompleteVerifiedMemoryWriter{result: test.result}, resolver)
			projector := &capturingClaimProjector{ids: []string{"must-not-exist"}}
			service, err := WithClaimProjector(base, projector)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice", ProjectKey: "hai", Question: "What is ready?",
				DraftAnswer: "The evidence boundary is ready.", Mode: ModeGrounded, AllowMemoryUpdate: true,
				ExternalEvidence: []EvidenceInput{{SourceType: "project_record", SourceID: "record-42", SourceURI: "local://project/record-42", Snippet: "The evidence boundary is ready."}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.MemoryUpdatesStored != 0 || result.MemoryUpdateFailures != 1 || projector.called != 0 || len(result.KnowledgeClaimIDs) != 0 {
				t.Fatalf("missing durable receipt reported success or projected knowledge: stored=%d failed=%d calls=%d", result.MemoryUpdatesStored, result.MemoryUpdateFailures, projector.called)
			}
		})
	}
}

func TestAnswerProjectsOnlyClaimsActuallyPromotedToMemory(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	repository := &capturingMemoryRepository{}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(repository), resolver)
	projector := &capturingClaimProjector{ids: []string{"stored-claim"}}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "What is ready?",
		DraftAnswer: "The evidence boundary is ready. The evidence boundary is ready for launch.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{SourceType: "project_record", SourceID: "record-42", SourceURI: "local://project/record-42", Snippet: "The evidence boundary is ready."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 2 || result.Claims[1].Status != StatusSourceSupported || result.MemoryUpdatesStored != 1 || result.MemoryUpdatesSkipped != 1 || len(repository.created) != 1 {
		t.Fatalf("mixed extractive/non-extractive fixture did not reach intended boundary: %#v", result)
	}
	if projector.called != 1 || len(projector.claims) != 1 || projector.claims[0].ID == uuid.Nil || projector.claims[0].ID != result.Claims[0].ID {
		t.Fatalf("a skipped claim reached semantic projection: %#v", projector.claims)
	}
}

func TestVerifyClaimsChecksWeekdayConflictBeyondBestEvidence(t *testing.T) {
	claim := "The review meeting is scheduled for Thursday."
	for _, test := range []struct {
		name                     string
		snippet                  string
		used, rejected, conflict bool
	}{
		{name: "relevant accepted conflict", snippet: "The review meeting is scheduled for Friday.", used: true, conflict: true},
		{name: "rejected conflict", snippet: "The review meeting is scheduled for Friday.", used: true, rejected: true},
		{name: "unused conflict", snippet: "The review meeting is scheduled for Friday."},
		{name: "unrelated weekday", snippet: "The swimming pool closes Friday.", used: true},
		{name: "matching weekday", snippet: claim, used: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := []models.VerificationEvidence{
				{Snippet: claim, Authority: authorityConnectedProvenance, QualityScore: 1, Used: true, SourceURI: "local://calendar/thursday"},
				{Snippet: test.snippet, Authority: authorityConnectedProvenance, QualityScore: .9, Used: test.used, Rejected: test.rejected, SourceURI: "local://calendar/other"},
			}
			claims := verifyClaims([]models.VerificationClaim{{ClaimText: claim}}, evidence, AnswerRequest{}, ModeGrounded)
			if (claims[0].Status == StatusConflicting) != test.conflict || claims[0].NeedsReview != test.conflict {
				t.Fatalf("conflict=%t, claim=%#v", test.conflict, claims[0])
			}
		})
	}
}

func TestAnswerConflictingAcceptedSourcesCannotPromoteMemory(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	repository := &capturingMemoryRepository{}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(repository), resolver)
	projector := &capturingClaimProjector{}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "When is the review meeting?",
		DraftAnswer: "The review meeting is scheduled for Thursday.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{
			{SourceType: "project_record", SourceID: "record-42", SourceURI: "local://calendar/thursday", Snippet: "The review meeting is scheduled for Thursday."},
			{SourceType: "project_record", SourceID: "record-43", SourceURI: "local://calendar/friday", Snippet: "The review meeting is scheduled for Friday."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusConflicting || result.Run.Status != StatusNeedsReview || result.MemoryUpdatesSkipped != 1 {
		t.Fatalf("conflicting source evidence did not require review: %#v", result)
	}
	if result.MemoryUpdatesStored != 0 || len(repository.created) != 0 || projector.called != 0 {
		t.Fatal("conflicting evidence entered durable memory or semantic knowledge")
	}
}
