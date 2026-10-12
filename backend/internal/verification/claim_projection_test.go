package verification

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
)

type capturingClaimProjector struct {
	called   int
	request  AnswerRequest
	claims   []models.VerificationClaim
	evidence []models.VerificationEvidence
	ids      []string
	err      error
}

func (p *capturingClaimProjector) ProjectClaims(
	_ context.Context,
	request AnswerRequest,
	_ models.VerificationRun,
	claims []models.VerificationClaim,
	evidence []models.VerificationEvidence,
) ([]string, error) {
	p.called++
	p.request = request
	p.claims = append([]models.VerificationClaim(nil), claims...)
	p.evidence = append([]models.VerificationEvidence(nil), evidence...)
	return append([]string(nil), p.ids...), p.err
}

func TestVerificationProjectsGroundedClaimsAndReturnsIdentifiers(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "verified_registry", Official: true, Primary: true}
	})
	memoryRepository := &capturingMemoryRepository{}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository), resolver)
	projector := &capturingClaimProjector{ids: []string{"claim-1"}}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "What is ready?",
		DraftAnswer: "The evidence boundary is ready.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "record-42", SourceURI: "local://project/record-42",
			Snippet: "The evidence boundary is ready.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projector.called != 1 || projector.request.ProjectKey != "hai" || len(projector.claims) != 1 || len(projector.evidence) != 1 {
		t.Fatalf("projector input was incomplete: %#v", projector)
	}
	if !reflect.DeepEqual(result.KnowledgeClaimIDs, []string{"claim-1"}) || result.KnowledgeError != "" || result.MemoryUpdatesStored != 1 {
		t.Fatalf("projection result = %#v", result)
	}
}

func TestVerificationProjectionFailureIsExplicitAndRedacted(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	memoryRepository := &capturingMemoryRepository{}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository), resolver)
	projector := &capturingClaimProjector{err: errors.New("postgres password=hunter2")}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "Is this record ready?",
		DraftAnswer: "The project record is ready for review.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "record-52", SourceURI: "local://project/record-52",
			Snippet: "The project record is ready for review.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.KnowledgeError != "semantic claim projection failed" || projector.called != 1 || result.MemoryUpdatesStored != 1 || len(memoryRepository.created) != 1 {
		t.Fatalf("projection failure was not surfaced: %#v", result)
	}
	payload, _ := json.Marshal(result)
	if strings.Contains(string(payload), "hunter2") || strings.Contains(string(payload), "postgres") {
		t.Fatal("projection internals leaked into result")
	}
}

func TestAnswerSurfacesMemoryPromotionAuditFailureAfterMemoryWrite(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	repository := &failingVerificationRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		auditActionErr:             "verification.memory_promoted",
		auditErr:                   errors.New("audit database unavailable"),
	}
	memoryRepository := &capturingMemoryRepository{}
	service := NewServiceWithAuthorityResolver(repository, nil, memory.NewService(memoryRepository), resolver)
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "Is this record ready?",
		DraftAnswer: "The project record is ready for review.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "record-56", SourceURI: "local://project/record-56",
			Snippet: "The project record is ready for review.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.MemoryUpdatesStored != 1 || len(memoryRepository.created) != 1 {
		t.Fatalf("source-supported memory write was unexpectedly lost: result=%#v stored=%d", result, len(memoryRepository.created))
	}
	if len(result.AuditWarnings) != 1 || result.AuditWarnings[0] != "verification.memory_promoted" {
		t.Fatalf("missing explicit audit warning: %#v", result.AuditWarnings)
	}
	if !strings.Contains(strings.Join(result.Logs, "\n"), "could not be durably recorded") {
		t.Fatalf("audit failure was not visible in result logs: %#v", result.Logs)
	}
}

func TestMemoryOptOutPreventsSemanticKnowledgeProjection(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	memoryRepository := &capturingMemoryRepository{}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository), resolver)
	projector := &capturingClaimProjector{ids: []string{"must-not-exist"}}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "Is this record ready?",
		DraftAnswer: "The project record is ready for review.", Mode: ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "record-53", SourceURI: "local://project/record-53",
			Snippet: "The project record is ready for review.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if projector.called != 0 || len(result.KnowledgeClaimIDs) != 0 || len(memoryRepository.created) != 0 || result.MemoryUpdateRequested {
		t.Fatalf("memory opt-out was bypassed by durable projection: result=%#v projector=%#v memories=%#v", result, projector, memoryRepository.created)
	}
}

func TestMemoryWriteFailurePreventsSemanticKnowledgeProjection(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Official: true, Primary: true}
	})
	memoryRepository := &capturingMemoryRepository{createErr: errors.New("memory database unavailable")}
	base := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, memory.NewService(memoryRepository), resolver)
	projector := &capturingClaimProjector{ids: []string{"must-not-exist"}}
	service, err := WithClaimProjector(base, projector)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", ProjectKey: "hai", Question: "Is this record ready?",
		DraftAnswer: "The project record is ready for review.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "record-54", SourceURI: "local://project/record-54",
			Snippet: "The project record is ready for review.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if result.MemoryUpdatesStored != 0 || result.MemoryUpdateFailures != 1 || projector.called != 0 || len(result.KnowledgeClaimIDs) != 0 {
		t.Fatalf("failed owner memory write still reached semantic storage: result=%#v projector=%#v", result, projector)
	}
}

func TestWithClaimProjectorRejectsInvalidComposition(t *testing.T) {
	if _, err := WithClaimProjector(nil, &capturingClaimProjector{}); err == nil {
		t.Fatal("nil verification service was accepted")
	}
	if _, err := WithClaimProjector(NewService(&fakeVerificationRepository{}, nil, nil), nil); err == nil {
		t.Fatal("nil projector was accepted")
	}
}
