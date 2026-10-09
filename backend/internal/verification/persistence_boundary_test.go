package verification

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
)

func TestAnswerFailsClosedWhenEvidenceCannotBePersisted(t *testing.T) {
	repository := &failingVerificationRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		evidenceErr:                errors.New("evidence database unavailable"),
	}
	memoryRepository := &capturingMemoryRepository{}
	service := NewService(repository, nil, memory.NewService(memoryRepository))

	_, err := service.Answer(AnswerRequest{
		OwnerIdentity:     "robert",
		Question:          "What does the record say?",
		DraftAnswer:       "The record says the work passed.",
		Mode:              ModeGrounded,
		AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "local_record", SourceID: "record-1", SourceURI: "file:///record-1",
			SourceLabel: "record", Snippet: "The record says the work passed.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "persist verification evidence") {
		t.Fatalf("Answer error = %v, want evidence persistence failure", err)
	}
	if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
		t.Fatalf("run was finalized after failed evidence persistence: %#v", repository.runs)
	}
	if len(repository.claims) != 0 || len(memoryRepository.created) != 0 {
		t.Fatalf("failed verification created claims or memory: claims=%d memory=%d", len(repository.claims), len(memoryRepository.created))
	}
}

func TestAnswerFailsClosedWhenClaimCannotBePersisted(t *testing.T) {
	repository := &failingVerificationRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		claimErr:                   errors.New("claim database unavailable"),
	}
	service := NewService(repository, nil, nil)

	_, err := service.Answer(AnswerRequest{
		OwnerIdentity: "robert",
		Question:      "What does the record say?",
		DraftAnswer:   "The record says the work passed.",
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "local_record", SourceID: "record-1", SourceURI: "file:///record-1",
			SourceLabel: "record", Snippet: "The record says the work passed.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "persist verification claim") {
		t.Fatalf("Answer error = %v, want claim persistence failure", err)
	}
	if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
		t.Fatalf("run was finalized after failed claim persistence: %#v", repository.runs)
	}
	if len(repository.claims) != 0 {
		t.Fatalf("claim persistence failure retained a claim: %#v", repository.claims)
	}
}

func TestAtomicRepositoryRollsBackEvidenceWhenClaimCannotBePersisted(t *testing.T) {
	repository := &transactionalFailingVerificationRepository{
		failingVerificationRepository: &failingVerificationRepository{
			fakeVerificationRepository: &fakeVerificationRepository{},
			claimErr:                   errors.New("claim database unavailable"),
		},
	}
	service := NewService(repository, nil, nil)

	_, err := service.Answer(AnswerRequest{
		OwnerIdentity: "robert",
		Question:      "What does the record say?",
		DraftAnswer:   "The record says the work passed.",
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "local_record", SourceID: "record-1", SourceURI: "file:///record-1",
			SourceLabel: "record", Snippet: "The record says the work passed.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "persist verification claim") {
		t.Fatalf("Answer error = %v, want claim persistence failure", err)
	}
	if len(repository.evidence) != 0 || len(repository.claims) != 0 {
		t.Fatalf("atomic failure retained partial verification records: evidence=%d claims=%d", len(repository.evidence), len(repository.claims))
	}
	if hasVerificationAudit(repository.audits, "verification.completed") {
		t.Fatal("atomic failure retained the completion audit")
	}
	if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
		t.Fatalf("atomic failure finalized the run: %#v", repository.runs)
	}
}

func TestAnswerFailsClosedWhenCompletionAuditCannotBePersisted(t *testing.T) {
	repository := &transactionalFailingVerificationRepository{
		failingVerificationRepository: &failingVerificationRepository{
			fakeVerificationRepository: &fakeVerificationRepository{},
			auditActionErr:             "verification.completed",
			auditErr:                   errors.New("audit database unavailable"),
		},
	}
	service := NewService(repository, nil, nil)

	_, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice",
		Question:      "What does the record say?",
		DraftAnswer:   "The record says the work passed.",
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "local_record", SourceID: "record-1", SourceURI: "file:///record-1",
			SourceLabel: "record", Snippet: "The record says the work passed.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "persist verification completion audit") {
		t.Fatalf("Answer error = %v, want completion audit failure", err)
	}
	if len(repository.evidence) != 0 || len(repository.claims) != 0 {
		t.Fatalf("atomic audit failure retained a partial verification: evidence=%d claims=%d", len(repository.evidence), len(repository.claims))
	}
	if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
		t.Fatalf("audit failure finalized the run: %#v", repository.runs)
	}
	if hasVerificationAudit(repository.audits, "verification.completed") {
		t.Fatal("failed finalization retained a completion audit")
	}
	if !hasVerificationAudit(repository.audits, "verification.persistence_failed") {
		t.Fatalf("failed finalization was not recorded outside its rolled-back transaction: %#v", repository.audits)
	}
}

func TestAnswerReportsWhenVerificationFailureAuditAlsoCannotBePersisted(t *testing.T) {
	repository := &transactionalFailingVerificationRepository{
		failingVerificationRepository: &failingVerificationRepository{
			fakeVerificationRepository: &fakeVerificationRepository{},
			auditActionErrors: map[string]error{
				"verification.completed":          errors.New("completion audit unavailable"),
				"verification.persistence_failed": errors.New("failure audit unavailable"),
			},
		},
	}
	service := NewService(repository, nil, nil)
	_, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What does the record say?",
		DraftAnswer: "The record says the work passed.", Mode: ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "local_record", SourceID: "record-1", SourceURI: "file:///record-1",
			SourceLabel: "record", Snippet: "The record says the work passed.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "could not durably record the verification failure audit") {
		t.Fatalf("missing failure-audit persistence error: %v", err)
	}
	if len(repository.evidence) != 0 || len(repository.claims) != 0 || hasVerificationAudit(repository.audits, "verification.completed") {
		t.Fatalf("failure-audit error retained a finalized or partial verification: %#v", repository.fakeVerificationRepository)
	}
}

func hasVerificationAudit(audits []models.VerificationAuditLog, action string) bool {
	for _, audit := range audits {
		if audit.Action == action {
			return true
		}
	}
	return false
}

func TestAnswerBlocksMemoryPromotionWhenOwnerOptInCannotBeAudited(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record"}
	})
	repository := &failingVerificationRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		auditActionErr:             "verification.memory_update_requested",
		auditErr:                   errors.New("audit database unavailable"),
	}
	memoryRepository := &capturingMemoryRepository{}
	service := NewServiceWithAuthorityResolver(repository, nil, memory.NewService(memoryRepository), resolver)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "Is this integration result stable?",
		DraftAnswer: "The integration result is stable.", Mode: ModeGrounded, AllowMemoryUpdate: true,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "project_record", SourceID: "integration-12", SourceURI: "local://project/integration-12",
			Snippet: "The integration result is stable.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer should keep the verification available for review: %v", err)
	}
	if !result.MemoryUpdateRequested || result.MemoryUpdatesStored != 0 || result.MemoryUpdatesSkipped != 1 ||
		result.MemoryUpdateBlockedReason != "owner memory opt-in could not be durably recorded" || len(memoryRepository.created) != 0 {
		t.Fatalf("memory was promoted without durable owner opt-in evidence: result=%#v memory=%#v", result, memoryRepository.created)
	}
}

type failingVerificationRepository struct {
	*fakeVerificationRepository
	evidenceErr       error
	claimErr          error
	auditErr          error
	auditActionErr    string
	auditActionErrors map[string]error
}

func (r *failingVerificationRepository) CreateEvidence(evidence *models.VerificationEvidence) (*models.VerificationEvidence, error) {
	if r.evidenceErr != nil {
		return nil, r.evidenceErr
	}
	return r.fakeVerificationRepository.CreateEvidence(evidence)
}

func (r *failingVerificationRepository) CreateClaim(claim *models.VerificationClaim) (*models.VerificationClaim, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	return r.fakeVerificationRepository.CreateClaim(claim)
}

func (r *failingVerificationRepository) CreateAuditLog(log *models.VerificationAuditLog) (*models.VerificationAuditLog, error) {
	if err, ok := r.auditActionErrors[log.Action]; ok {
		return nil, err
	}
	if r.auditErr != nil && (r.auditActionErr == "" || log.Action == r.auditActionErr) {
		return nil, r.auditErr
	}
	return r.fakeVerificationRepository.CreateAuditLog(log)
}

type transactionalFailingVerificationRepository struct {
	*failingVerificationRepository
}

func (r *transactionalFailingVerificationRepository) WithinTransaction(action func(Repository) error) error {
	staged := &fakeVerificationRepository{
		runs:     append([]models.VerificationRun(nil), r.runs...),
		claims:   append([]models.VerificationClaim(nil), r.claims...),
		evidence: append([]models.VerificationEvidence(nil), r.evidence...),
		audits:   append([]models.VerificationAuditLog(nil), r.audits...),
	}
	transactional := &failingVerificationRepository{
		fakeVerificationRepository: staged,
		evidenceErr:                r.evidenceErr,
		claimErr:                   r.claimErr,
		auditErr:                   r.auditErr,
		auditActionErr:             r.auditActionErr,
		auditActionErrors:          r.auditActionErrors,
	}
	if err := action(transactional); err != nil {
		return err
	}
	r.runs = staged.runs
	r.claims = staged.claims
	r.evidence = staged.evidence
	r.audits = staged.audits
	return nil
}
