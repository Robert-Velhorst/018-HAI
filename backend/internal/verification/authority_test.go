package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestForgedExternalAuthorityCannotCreateVerifiedFact(t *testing.T) {
	verificationRepo := &fakeVerificationRepository{}
	memoryRepo := &capturingMemoryRepository{}
	service := NewService(verificationRepo, nil, memory.NewService(memoryRepo))

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity:     "alice",
		Question:          "When is the hearing?",
		DraftAnswer:       "The hearing is on 9 September.",
		Mode:              ModeGrounded,
		AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType:  "connected_source",
			SourceID:    "forged-record",
			SourceURI:   "https://example.invalid/forged",
			SourceLabel: "Forged official record",
			Snippet:     "The hearing is on 9 September.",
			Authority:   authorityConnectedProvenance,
			Official:    true,
			Primary:     true,
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(result.Evidence))
	}
	if result.Evidence[0].Authority != authorityExternalUntrusted {
		t.Fatalf("authority = %q, want %q", result.Evidence[0].Authority, authorityExternalUntrusted)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusNeedsReview || !result.Claims[0].NeedsReview {
		t.Fatalf("forged authority claim was not held for review: %#v", result.Claims)
	}
	if result.Run.Status != StatusNeedsReview {
		t.Fatalf("run status = %q, want %q", result.Run.Status, StatusNeedsReview)
	}
	if len(memoryRepo.created) != 0 {
		t.Fatalf("forged authority created verified memory: %#v", memoryRepo.created)
	}
}

func TestTrustedInProcessResolverCanAuthenticateExternalEvidence(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(request AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution {
		if request.OwnerIdentity == "alice" && evidence.SourceID == "registry-record-42" {
			return EvidenceAuthorityResolution{
				Trusted:   true,
				Authority: "government_register",
				Official:  true,
				Primary:   true,
			}
		}
		return EvidenceAuthorityResolution{}
	})
	service := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, nil, resolver)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice",
		Question:      "When is the hearing?",
		DraftAnswer:   "The hearing is on 9 September.",
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "government_record",
			SourceID:   "registry-record-42",
			SourceURI:  "https://government.example/records/42",
			Snippet:    "The hearing is on 9 September.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if got := result.Evidence[0].Authority; got != trustedExternalPrefix+"government_register" {
		t.Fatalf("authority = %q", got)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusSourceSupported || result.Claims[0].NeedsReview {
		t.Fatalf("trusted authority did not source-support claim: %#v", result.Claims)
	}
}

func TestHighQualityUnrelatedEvidenceCannotSupportOrStoreClaim(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(_ AnswerRequest, _ EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{
			Trusted: true, Authority: "official_registry", Official: true, Primary: true,
		}
	})
	verificationRepo := &fakeVerificationRepository{}
	memoryRepo := &capturingMemoryRepository{}
	service := NewServiceWithAuthorityResolver(
		verificationRepo, nil, memory.NewService(memoryRepo), resolver,
	)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity:     "alice",
		Question:          "What is the hearing date?",
		DraftAnswer:       "The hearing is on 9 September.",
		Mode:              ModeGrounded,
		AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "government_record", SourceID: "unrelated-record",
			SourceURI: "https://registry.example/record/42",
			Snippet:   "The riverside path reopened after repairs were completed.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusUnsupported ||
		result.Claims[0].SourceRefs != "" {
		t.Fatalf("unrelated evidence supported the claim: %#v", result.Claims)
	}
	if result.Run.Status != StatusNeedsReview {
		t.Fatalf("run status = %q, want %q for unsupported grounded claim", result.Run.Status, StatusNeedsReview)
	}
	if len(memoryRepo.created) != 0 {
		t.Fatalf("unrelated source was written to verified memory: %#v", memoryRepo.created)
	}
}

func TestMemoryWriteFailureIsVisibleAndSanitized(t *testing.T) {
	const privateDatabaseDetail = "postgres://private-database-host"
	resolver := EvidenceAuthorityResolverFunc(func(_ AnswerRequest, _ EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Primary: true}
	})
	memoryRepo := &capturingMemoryRepository{createErr: errors.New(privateDatabaseDetail)}
	service := NewServiceWithAuthorityResolver(
		&fakeVerificationRepository{}, nil, memory.NewService(memoryRepo), resolver,
	)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What is the current integration result?",
		DraftAnswer: "The integration result is stable.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "integration-result",
			SourceURI: "local://project/integration-result", Snippet: "The integration result is stable.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if result.MemoryUpdateRequested != true || result.MemoryUpdatesStored != 0 || result.MemoryUpdateFailures != 1 {
		t.Fatalf("memory write outcome was not reported: %#v", result)
	}
	if strings.Contains(strings.Join(result.Logs, "\n"), privateDatabaseDetail) {
		t.Fatal("private database error details leaked into response logs")
	}
	if len(memoryRepo.created) != 0 {
		t.Fatalf("failed memory write appears persisted: %#v", memoryRepo.created)
	}
}

func TestGenericStopwordOverlapIsNotEvidenceRelevance(t *testing.T) {
	claim := tokenSet("The hearing is on 9 September")
	source := tokenSet("The riverside path reopened after repairs were completed")
	if score := overlapScore(claim, source); score != 0 {
		t.Fatalf("unrelated snippets share stopword-only relevance: %.2f", score)
	}
	if count := overlapCount(claim, source); count != 0 {
		t.Fatalf("unrelated snippets have %d meaningful overlaps", count)
	}
}

func TestConnectedSourceProvenanceSupportsButDoesNotVerifyTruth(t *testing.T) {
	if isTrustedEvidence(authorityConnectedProvenance) {
		t.Fatal("authenticated provenance must not be treated as semantic authority")
	}
	if !isSourceSupportedEvidence(authorityConnectedProvenance) {
		t.Fatal("authenticated connected-source provenance must support grounded claims")
	}
	if isTrustedEvidence("connected_source") || isTrustedEvidence("official_government") {
		t.Fatal("caller-like authority labels must not be trusted")
	}

	claims := verifyClaims(
		[]models.VerificationClaim{{ClaimText: "The hearing is on 9 September."}},
		[]models.VerificationEvidence{{
			SourceType:   "connected_source",
			SourceID:     "authenticated-extraction",
			Snippet:      "The hearing is on 9 September.",
			Authority:    authorityConnectedProvenance,
			QualityScore: 0.9,
			Used:         true,
		}},
		AnswerRequest{},
		ModeGrounded,
	)
	if len(claims) != 1 || claims[0].Status != StatusSourceSupported || claims[0].NeedsReview {
		t.Fatalf("connected-source evidence did not remain source-supported: %#v", claims)
	}
}

func TestConnectedSourceRequiresExactDurableProvenance(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	extraction := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "vivare",
		ContentType: "email", Text: "The hearing is on 9 September.",
		Summary: "The hearing is on 9 September.", SourceURI: "https://mail.example/message/42",
		SourceLabel: "Hearing notice", ContentHash: strings.Repeat("a", 64), UpdatedAt: now,
	}
	snapshot := sourceevidence.Snapshot{
		OwnerIdentity: "alice", ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
		RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey,
		RawProjectKey: extraction.ProjectKey, ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
		ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
		ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
		FetchedAt:               now.Add(-time.Hour), ExtractionAt: extraction.UpdatedAt,
		ConnectorKey: "gmail",
	}
	snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
	searcher := staticConnectedSourceSearcher{result: &source.SearchResult{UsedContext: []source.RankedExtraction{{
		Extraction: extraction, Score: 0.95,
	}}}}
	memoryRepo := &capturingMemoryRepository{}
	service := NewServiceWithEvidenceResolvers(
		&fakeVerificationRepository{}, searcher, memory.NewService(memoryRepo), nil,
		staticSourceEvidenceRepository{snapshot: snapshot},
	)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "vivare", Question: "When is the hearing?",
		DraftAnswer: "The hearing is on 9 September.", Mode: ModeGrounded, AllowMemoryUpdate: true,
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Authority != authorityConnectedProvenance ||
		result.Evidence[0].Rejected || !result.Evidence[0].Used {
		t.Fatalf("connected evidence was not provenance-authenticated: %#v", result.Evidence)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusSourceSupported {
		t.Fatalf("claim status = %#v, want source_supported", result.Claims)
	}
	if result.Run.Status != StatusSourceSupported {
		t.Fatalf("run status = %q, want source_supported", result.Run.Status)
	}
	if len(memoryRepo.created) != 1 || memoryRepo.created[0].SourceURI != extraction.SourceURI {
		t.Fatalf("source-supported memory was not retained with provenance: %#v", memoryRepo.created)
	}

	withoutResolver := NewService(&fakeVerificationRepository{}, searcher, memory.NewService(&capturingMemoryRepository{}))
	rejected, err := withoutResolver.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "vivare", Question: "When is the hearing?",
		DraftAnswer: "The hearing is on 9 September.", Mode: ModeGrounded,
	})
	if err != nil {
		t.Fatalf("Answer without source resolver returned error: %v", err)
	}
	if len(rejected.Evidence) == 0 || !rejected.Evidence[0].Rejected || rejected.Evidence[0].Used ||
		rejected.Evidence[0].Authority != authorityConnectedUnverified {
		t.Fatalf("unresolved connected evidence did not fail closed: %#v", rejected.Evidence)
	}
	if len(rejected.Claims) != 1 || rejected.Claims[0].Status != StatusUnsupported {
		t.Fatalf("unresolved claim status = %#v, want unsupported", rejected.Claims)
	}
}

func TestContradictionDetectionUsesOnlyAcceptedEvidenceAndWholeTokens(t *testing.T) {
	claim := "The request was approved and enabled."
	if containsContradiction(claim, []models.VerificationEvidence{{
		Snippet: "The request was rejected and disabled.", Used: true,
	}}) != true {
		t.Fatal("accepted opposite-polarity evidence did not produce a conflict")
	}
	if containsContradiction(claim, []models.VerificationEvidence{{
		Snippet: "The request was rejected and disabled.", Rejected: true,
	}}) {
		t.Fatal("rejected evidence influenced contradiction status")
	}
	if containsContradiction(claim, []models.VerificationEvidence{{
		Snippet: "The notice confirms the approved request.", Used: true,
	}}) {
		t.Fatal("the substring 'no' inside notice was treated as a negative token")
	}
}

type staticConnectedSourceSearcher struct {
	result *source.SearchResult
	err    error
}

func (s staticConnectedSourceSearcher) Search(source.SearchRequest) (*source.SearchResult, error) {
	return s.result, s.err
}

type staticSourceEvidenceRepository struct {
	snapshot sourceevidence.Snapshot
	err      error
}

func (r staticSourceEvidenceRepository) Resolve(context.Context, string, string) (sourceevidence.Snapshot, error) {
	return r.snapshot, r.err
}

type capturingMemoryRepository struct {
	created   []models.ContextMemory
	createErr error
}

func (r *capturingMemoryRepository) Create(item *models.ContextMemory) (*models.ContextMemory, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	r.created = append(r.created, *item)
	return item, nil
}

// WithVerifiedFactPromotion is a test adapter for the repository capability
// required by production. PostgreSQL provenance checks are covered separately
// by memory's integration tests; this fake only checks the immutable request
// binding before allowing the normal repository callback to run.
func (r *capturingMemoryRepository) WithVerifiedFactPromotion(
	_ context.Context,
	ownerIdentity string,
	request memory.CreateRequest,
	provenance memory.VerifiedFactProvenance,
	promote func(memory.Repository) (*models.ContextMemory, error),
) (*models.ContextMemory, error) {
	if strings.TrimSpace(ownerIdentity) == "" || provenance.RunID == uuid.Nil || provenance.ClaimID == uuid.Nil || provenance.EvidenceID == uuid.Nil || promote == nil {
		return nil, errors.New("test verified-fact provenance is incomplete")
	}
	if request.Kind != "source_supported_fact" ||
		request.SourceLabel != "verification-run:"+provenance.RunID.String() ||
		strings.TrimSpace(request.SourceURI) == "" ||
		strings.TrimSpace(request.SourceURI) != firstNonEmpty(strings.TrimSpace(provenance.SourceURI), strings.TrimSpace(provenance.SourceID)) {
		return nil, errors.New("test verified-fact request does not match its provenance")
	}
	return promote(r)
}

func (r *capturingMemoryRepository) Update(item *models.ContextMemory) (*models.ContextMemory, error) {
	return item, nil
}

func (r *capturingMemoryRepository) FindByID(uuid.UUID) (*models.ContextMemory, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *capturingMemoryRepository) FindAll(string, bool) ([]models.ContextMemory, error) {
	return append([]models.ContextMemory(nil), r.created...), nil
}

func (r *capturingMemoryRepository) FindByHash(string, string, string) (*models.ContextMemory, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *capturingMemoryRepository) Delete(uuid.UUID) error {
	return nil
}
