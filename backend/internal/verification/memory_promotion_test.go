package verification

import (
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"

	"github.com/google/uuid"
)

func TestDeterministicallyVerifiedCalculationIsNotPromotedWithoutSource(t *testing.T) {
	memoryRepository := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository))

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What is two plus two?", DraftAnswer: "2 + 2 = 4",
		Mode: ModeGrounded, AllowMemoryUpdate: true,
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusVerified {
		t.Fatalf("deterministic calculation status = %#v, want verified answer result", result.Claims)
	}
	if len(memoryRepository.created) != 0 {
		t.Fatalf("source-less deterministic result entered durable memory: %#v", memoryRepository.created)
	}
	if result.MemoryUpdatesStored != 0 || result.MemoryUpdatesSkipped != 1 || result.MemoryUpdateBlockedReason != "" {
		t.Fatalf("memory promotion outcome = %#v, want one skipped claim and no write", result)
	}
}

func TestMemoryPromotionRequiresOwnerBoundRunAndStrongSourceProof(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AnswerRequest, *models.VerificationRun, *models.VerificationClaim, *models.VerificationEvidence)
	}{
		{name: "deterministic verified status is not source grounded", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.Status = StatusVerified
		}},
		{name: "missing source reference", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.SourceRefs = ""
		}},
		{name: "label alone is not a resolvable source reference", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.SourceRefs = "Project record"
		}},
		{name: "reference does not match evidence", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.SourceRefs = "https://records.example/other"
		}},
		{name: "claim belongs to a different run", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.RunID = uuid.New()
		}},
		{name: "evidence belongs to a different run", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, evidence *models.VerificationEvidence) {
			evidence.RunID = uuid.New()
		}},
		{name: "evidence was rejected", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, evidence *models.VerificationEvidence) {
			evidence.Rejected = true
		}},
		{name: "evidence was not used", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, evidence *models.VerificationEvidence) {
			evidence.Used = false
		}},
		{name: "authority is not authenticated", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, evidence *models.VerificationEvidence) {
			evidence.Authority = authorityExternalUntrusted
		}},
		{name: "evidence quality is too low", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, evidence *models.VerificationEvidence) {
			evidence.QualityScore = minimumMemoryEvidenceQuality - 0.01
		}},
		{name: "claim confidence is too low", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.Confidence = minimumMemoryPromotionConfidence - 0.01
		}},
		{name: "claim still needs review", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.NeedsReview = true
		}},
		{name: "run belongs to another owner", mutate: func(_ *AnswerRequest, run *models.VerificationRun, _ *models.VerificationClaim, _ *models.VerificationEvidence) {
			run.OwnerIdentity = "bob"
		}},
		{name: "owner identity is absent", mutate: func(request *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, _ *models.VerificationEvidence) {
			request.OwnerIdentity = " "
		}},
		{name: "high-risk claim requires approval", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.HighRisk = true
		}},
		{name: "action mode cannot promote without a validated approval path", mutate: func(request *AnswerRequest, _ *models.VerificationRun, _ *models.VerificationClaim, _ *models.VerificationEvidence) {
			request.Mode = ModeAction
		}},
		{name: "human-approved label is not a validated approval record", mutate: func(_ *AnswerRequest, _ *models.VerificationRun, claim *models.VerificationClaim, _ *models.VerificationEvidence) {
			claim.HighRisk = true
			claim.Status = StatusHumanApproved
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, run, claim, evidence := memoryPromotionFixture()
			test.mutate(&request, &run, &claim, &evidence)
			memoryRepository := &capturingMemoryRepository{}
			service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository)).(*service)

			outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
			if len(memoryRepository.created) != 0 || outcome.Stored != 0 {
				t.Fatalf("ineligible claim entered memory: outcome=%#v memory=%#v", outcome, memoryRepository.created)
			}
			if outcome.BlockedReason == "" && outcome.Skipped != 1 {
				t.Fatalf("ineligible claim was not reported as skipped: %#v", outcome)
			}
		})
	}
}

func TestMemoryPromotionStoresOnlyOwnerScopedSourceSupportedFactsWithRunProvenance(t *testing.T) {
	request, run, claim, evidence := memoryPromotionFixture()
	memoryRepository := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository)).(*service)

	outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
	if outcome.Stored != 1 || outcome.Failed != 0 || outcome.Skipped != 0 || len(memoryRepository.created) != 1 {
		t.Fatalf("eligible source-grounded claim was not stored: outcome=%#v memory=%#v", outcome, memoryRepository.created)
	}
	created := memoryRepository.created[0]
	if created.OwnerIdentity != request.OwnerIdentity || created.Kind != "source_supported_fact" ||
		created.SourceURI != evidence.SourceURI || created.SourceLabel != "verification-run:"+run.ID.String() {
		t.Fatalf("stored memory lost owner or source/run provenance: %#v", created)
	}
	if created.Tags == "verified,source_supported" || created.Confidence < minimumMemoryPromotionConfidence {
		t.Fatalf("memory mislabels or weakens source-supported status: %#v", created)
	}
}

func TestMemoryPromotionDoesNotStoreParaphrasesNotPresentAsEvidenceSentence(t *testing.T) {
	request, run, claim, evidence := memoryPromotionFixture()
	claim.ClaimText = "The meeting was cancelled and moved to Thursday."
	claim.Confidence = 0.95
	evidence.Snippet = "The meeting remains scheduled for Friday."
	memoryRepository := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository)).(*service)

	outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
	if outcome.Stored != 0 || outcome.Skipped != 1 || len(memoryRepository.created) != 0 {
		t.Fatalf("non-extractive claim entered durable memory: outcome=%#v memory=%#v", outcome, memoryRepository.created)
	}
}

func TestAnswerMarksConflictingWeekdayAndNeverPromotesIt(t *testing.T) {
	const sourceURI = "https://calendar.example/events/review"
	memoryRepository := &capturingMemoryRepository{}
	resolver := EvidenceAuthorityResolverFunc(func(_ AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution {
		if evidence.SourceID == "authenticated-calendar-record" {
			return EvidenceAuthorityResolution{Trusted: true, Authority: "calendar_record", Official: true, Primary: true}
		}
		return EvidenceAuthorityResolution{}
	})
	service := NewServiceWithAuthorityResolver(
		&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository), resolver,
	)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "When is the review meeting?",
		DraftAnswer: "The review meeting is scheduled for Thursday.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "calendar_record", SourceID: "authenticated-calendar-record", SourceURI: sourceURI,
			Snippet: "The review meeting is scheduled for Friday.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusConflicting || !result.Claims[0].NeedsReview {
		t.Fatalf("weekday contradicted by its source was not marked conflicting: %#v", result.Claims)
	}
	if result.Claims[0].SupportExplanation != "the claim conflicts with its supporting evidence; human review is required" {
		t.Fatalf("conflict explanation = %q", result.Claims[0].SupportExplanation)
	}
	if result.Run.Status != StatusNeedsReview {
		t.Fatalf("run status = %q, want review for conflicting weekday", result.Run.Status)
	}
	if result.MemoryUpdatesStored != 0 || result.MemoryUpdatesSkipped != 1 || len(memoryRepository.created) != 0 {
		t.Fatalf("unsupported date entered durable memory: result=%#v memory=%#v", result, memoryRepository.created)
	}
}

func TestHasConflictingSingleWeekday(t *testing.T) {
	tests := []struct {
		name     string
		claim    string
		evidence string
		want     bool
	}{
		{
			name:     "different single weekdays conflict",
			claim:    "The review meeting is scheduled for Thursday.",
			evidence: "The review meeting is scheduled for Friday.",
			want:     true,
		},
		{
			name:     "same weekday agrees",
			claim:    "The review meeting is scheduled for Friday.",
			evidence: "The review meeting is scheduled for Friday.",
			want:     false,
		},
		{
			name:     "multiple source weekdays are ambiguous",
			claim:    "The review meeting is scheduled for Friday.",
			evidence: "The review meeting is scheduled for Friday, with a reminder on Thursday.",
			want:     false,
		},
		{
			name:     "no weekday in claim is not a date contradiction",
			claim:    "The review meeting is scheduled.",
			evidence: "The review meeting is scheduled for Friday.",
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasConflictingSingleWeekday(test.claim, test.evidence); got != test.want {
				t.Fatalf("hasConflictingSingleWeekday() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMatchesEvidenceSentenceRequiresWholeNormalizedSentence(t *testing.T) {
	tests := []struct {
		name    string
		claim   string
		snippet string
		want    bool
	}{
		{name: "same sentence with case and punctuation differences", claim: "The review is scheduled for Friday", snippet: "Intro. THE review is scheduled for Friday! More text.", want: true},
		{name: "decimal and URL punctuation are not sentence boundaries", claim: "Version 2.5 is at https://example.com/releases", snippet: "Version 2.5 is at https://example.com/releases. Contact the team.", want: true},
		{name: "substring is not a complete sentence", claim: "The review is scheduled", snippet: "The review is scheduled for Friday.", want: false},
		{name: "contradictory details do not match", claim: "The meeting was cancelled and moved to Thursday", snippet: "The meeting remains scheduled for Friday.", want: false},
		{name: "empty claim is rejected", claim: "  ", snippet: "Some source sentence.", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesEvidenceSentence(test.claim, test.snippet); got != test.want {
				t.Fatalf("matchesEvidenceSentence(%q, %q) = %v, want %v", test.claim, test.snippet, got, test.want)
			}
		})
	}
}

func TestHighRiskMemoryPromotionStaysBlockedWithoutApprovalRecordValidator(t *testing.T) {
	request, run, claim, evidence := memoryPromotionFixture()
	request.HumanApproved = true
	request.HumanApprovalReference = "approval-" + uuid.NewString()
	claim.HighRisk = true
	claim.Status = StatusHumanApproved
	memoryRepository := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository)).(*service)

	outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
	if outcome.Stored != 0 || outcome.Skipped != 1 || len(memoryRepository.created) != 0 {
		t.Fatalf("approval-shaped status entered memory without a validated approval record: outcome=%#v memory=%#v", outcome, memoryRepository.created)
	}
}

func TestLegacyApprovalHintsDoNotApproveHighRiskClaims(t *testing.T) {
	request := AnswerRequest{
		OwnerIdentity: "alice", Question: "Should I send this legal email?",
		DraftAnswer: "The legal email confirms the hearing is Friday.", Mode: ModeGrounded,
		HumanApproved: true, HumanApprovalReference: "approval-" + uuid.NewString(),
	}
	claims := decomposeClaims(uuid.New(), request.DraftAnswer, ModeGrounded, request)
	verified := verifyClaims(claims, []models.VerificationEvidence{{
		RunID: claims[0].RunID, SourceType: "legal_record", SourceURI: "local://legal/12",
		Snippet: "The legal email confirms the hearing is Friday.", Authority: trustedExternalPrefix + "legal_record",
		QualityScore: 0.95, Used: true,
	}}, request, ModeGrounded)
	if len(verified) != 1 || verified[0].Status != StatusNeedsReview || !verified[0].NeedsReview {
		t.Fatalf("legacy approval hints were treated as a verified approval record: %#v", verified)
	}
}

func TestConnectedSourceSnapshotMustMatchAuthenticatedOwnerAndDigest(t *testing.T) {
	now := time.Date(2026, time.September, 24, 8, 0, 0, 0, time.UTC)
	extraction := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "hai",
		ContentType: "email", Text: "The review meeting is scheduled for Friday.",
		Summary: "The review meeting is scheduled for Friday.", SourceURI: "https://mail.example/message/123",
		SourceLabel: "Review meeting", ContentHash: "abc123", UpdatedAt: now,
	}
	searcher := staticConnectedSourceSearcher{result: &source.SearchResult{UsedContext: []source.RankedExtraction{{
		Extraction: extraction, Score: 0.95,
	}}}}

	for _, test := range []struct {
		name   string
		mutate func(*sourceevidence.Snapshot)
	}{
		{name: "another owner", mutate: func(snapshot *sourceevidence.Snapshot) {
			snapshot.OwnerIdentity = "bob"
			snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(*snapshot)
		}},
		{name: "tampered snapshot digest", mutate: func(snapshot *sourceevidence.Snapshot) { snapshot.SnapshotDigest = "invalid" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := sourceevidence.Snapshot{
				OwnerIdentity: "alice", ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
				RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey,
				RawProjectKey: extraction.ProjectKey, ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
				ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
				ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
				FetchedAt:               now.Add(-time.Hour), ExtractionAt: now, ConnectorKey: "mail",
			}
			snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
			test.mutate(&snapshot)
			memoryRepository := &capturingMemoryRepository{}
			service := NewServiceWithEvidenceResolvers(
				&fakeVerificationRepository{}, searcher, memory.NewService(memoryRepository), nil,
				staticSourceEvidenceRepository{snapshot: snapshot},
			)

			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice", ProjectKey: "hai", Question: "When is the review meeting?",
				DraftAnswer: "The review meeting is scheduled for Friday.", Mode: ModeGrounded, AllowMemoryUpdate: true,
			})
			if err != nil {
				t.Fatalf("Answer returned error: %v", err)
			}
			if len(result.Evidence) != 1 || !result.Evidence[0].Rejected || result.Evidence[0].Used ||
				result.Evidence[0].Authority != authorityConnectedUnverified {
				t.Fatalf("invalid snapshot was accepted: %#v", result.Evidence)
			}
			if len(memoryRepository.created) != 0 || result.MemoryUpdatesStored != 0 {
				t.Fatalf("cross-owner or tampered source was promoted: %#v", memoryRepository.created)
			}
		})
	}
}

func memoryPromotionFixture() (AnswerRequest, models.VerificationRun, models.VerificationClaim, models.VerificationEvidence) {
	runID := uuid.New()
	request := AnswerRequest{OwnerIdentity: "alice", ProjectKey: "hai"}
	run := models.VerificationRun{ID: runID, OwnerIdentity: "alice", Status: StatusSourceSupported}
	claim := models.VerificationClaim{
		ID: uuid.New(), RunID: runID, ClaimText: "The review is scheduled for Friday.", Status: StatusSourceSupported,
		SourceRefs: "https://records.example/review/12", Confidence: 0.91,
	}
	evidence := models.VerificationEvidence{
		ID: uuid.New(), RunID: runID, SourceType: "project_record", SourceID: "review-12",
		SourceURI: claim.SourceRefs, SourceLabel: "Project review record",
		Snippet: claim.ClaimText, Authority: trustedExternalPrefix + "project_record",
		QualityScore: 0.9, Used: true,
	}
	return request, run, claim, evidence
}
