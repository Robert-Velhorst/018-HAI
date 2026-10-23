package verification

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
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
	runAck            func(*models.VerificationRun) *models.VerificationRun
	evidenceAck       func(*models.VerificationEvidence) *models.VerificationEvidence
	claimAck          func(*models.VerificationClaim) *models.VerificationClaim
}

type creationAcknowledgementRepository struct {
	*fakeVerificationRepository
	acknowledge func(*models.VerificationRun) *models.VerificationRun
}

func (r *creationAcknowledgementRepository) CreateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	stored, err := r.fakeVerificationRepository.CreateRun(run)
	if err != nil {
		return nil, err
	}
	return r.acknowledge(stored), nil
}

func TestAnswerRejectsUnconfirmedOrMismatchedCreation(t *testing.T) {
	for _, test := range []struct {
		name string
		ack  func(*models.VerificationRun) *models.VerificationRun
	}{
		{"missing acknowledgement", func(*models.VerificationRun) *models.VerificationRun { return nil }},
		{"missing identity", func(run *models.VerificationRun) *models.VerificationRun { run.ID = uuid.Nil; return run }},
		{"foreign owner", func(run *models.VerificationRun) *models.VerificationRun { run.OwnerIdentity = "bob"; return run }},
		{"different question", func(run *models.VerificationRun) *models.VerificationRun {
			run.Question = "A different question"
			return run
		}},
		{"premature finalization", func(run *models.VerificationRun) *models.VerificationRun { run.Status = StatusNeedsReview; return run }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &creationAcknowledgementRepository{
				fakeVerificationRepository: &fakeVerificationRepository{}, acknowledge: test.ack,
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("creation acknowledgement caused a panic: %v", recovered)
					}
				}()
				result, err := NewService(repository, nil, nil).Answer(AnswerRequest{
					OwnerIdentity: "alice", Question: "What is the result?", DraftAnswer: "The work passed.",
					Mode: ModeGrounded, AllowMemoryUpdate: true,
				})
				if err == nil || result != nil || !strings.Contains(err.Error(), "create verification run") {
					t.Errorf("unconfirmed creation returned a result: result=%#v err=%v", result, err)
				}
			}()
			if len(repository.audits) != 0 || len(repository.evidence) != 0 || len(repository.claims) != 0 {
				t.Fatal("unconfirmed creation continued into verification persistence")
			}
		})
	}
}

func TestAnswerAcceptsDatabaseManagedCreationMetadata(t *testing.T) {
	repository := &creationAcknowledgementRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		acknowledge: func(run *models.VerificationRun) *models.VerificationRun {
			run.CreatedAt = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			run.UpdatedAt = run.CreatedAt
			return run
		},
	}
	result, err := NewService(repository, nil, nil).Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What is the result?", DraftAnswer: "The work passed.", Mode: ModeGrounded,
	})
	if err != nil || result == nil || result.Run.ID == uuid.Nil || result.Run.CreatedAt.IsZero() {
		t.Fatalf("database-managed creation metadata was rejected: result=%#v err=%v", result, err)
	}
}

func (r *failingVerificationRepository) UpdateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	if r.runAck != nil {
		return r.runAck(run), nil
	}
	return r.fakeVerificationRepository.UpdateRun(run)
}

func (r *failingVerificationRepository) CreateEvidence(evidence *models.VerificationEvidence) (*models.VerificationEvidence, error) {
	if r.evidenceErr != nil {
		return nil, r.evidenceErr
	}
	saved, err := r.fakeVerificationRepository.CreateEvidence(evidence)
	if err == nil && r.evidenceAck != nil {
		return r.evidenceAck(saved), nil
	}
	return saved, err
}

func (r *failingVerificationRepository) CreateClaim(claim *models.VerificationClaim) (*models.VerificationClaim, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	saved, err := r.fakeVerificationRepository.CreateClaim(claim)
	if err == nil && r.claimAck != nil {
		return r.claimAck(saved), nil
	}
	return saved, err
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
		runAck:                     r.runAck,
		evidenceAck:                r.evidenceAck,
		claimAck:                   r.claimAck,
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

func TestAnswerRejectsMismatchedEvidenceAndClaimAcknowledgements(t *testing.T) {
	for _, test := range []struct {
		name        string
		evidenceAck func(*models.VerificationEvidence) *models.VerificationEvidence
		claimAck    func(*models.VerificationClaim) *models.VerificationClaim
	}{
		{name: "foreign evidence run", evidenceAck: func(record *models.VerificationEvidence) *models.VerificationEvidence {
			record.RunID = uuid.New()
			return record
		}},
		{name: "different evidence", evidenceAck: func(record *models.VerificationEvidence) *models.VerificationEvidence {
			record.Snippet = "Different evidence."
			return record
		}},
		{name: "changed evidence usage", evidenceAck: func(record *models.VerificationEvidence) *models.VerificationEvidence {
			record.Used = !record.Used
			return record
		}},
		{name: "foreign claim run", claimAck: func(record *models.VerificationClaim) *models.VerificationClaim {
			record.RunID = uuid.New()
			return record
		}},
		{name: "different claim", claimAck: func(record *models.VerificationClaim) *models.VerificationClaim {
			record.ClaimText = "A different claim."
			return record
		}},
		{name: "changed claim status", claimAck: func(record *models.VerificationClaim) *models.VerificationClaim {
			record.Status = "unsupported"
			return record
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &transactionalFailingVerificationRepository{failingVerificationRepository: &failingVerificationRepository{
				fakeVerificationRepository: &fakeVerificationRepository{}, evidenceAck: test.evidenceAck, claimAck: test.claimAck,
			}}
			memoryRepository := &capturingMemoryRepository{}
			resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
				return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Primary: true}
			})
			service := NewServiceWithAuthorityResolver(repository, nil, memory.NewService(memoryRepository), resolver)
			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice", Question: "What is the integration result?", Mode: ModeGrounded,
				DraftAnswer: "The integration result is stable.", AllowMemoryUpdate: true,
				ExternalEvidence: []EvidenceInput{{SourceType: "project_record", SourceID: "integration-1",
					SourceURI: "local://project/integration-1", Snippet: "The integration result is stable."}},
			})
			if err == nil || result != nil || !strings.Contains(err.Error(), "acknowledgement") {
				t.Errorf("mismatched record acknowledgement was accepted: resultPresent=%t err=%v", result != nil, err)
			}
			if len(memoryRepository.created) != 0 || len(repository.evidence) != 0 || len(repository.claims) != 0 || hasVerificationAudit(repository.audits, "verification.completed") {
				t.Fatal("mismatched record acknowledgement retained memory or finalized records")
			}
			if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
				t.Fatal("mismatched record acknowledgement finalized the run")
			}
		})
	}
}

func TestFinalizationRejectsForeignRecordInputsBeforeWriting(t *testing.T) {
	for _, kind := range []string{"evidence", "claim"} {
		t.Run(kind, func(t *testing.T) {
			repository := &transactionalFailingVerificationRepository{failingVerificationRepository: &failingVerificationRepository{
				fakeVerificationRepository: &fakeVerificationRepository{},
			}}
			run, err := repository.CreateRun(&models.VerificationRun{OwnerIdentity: "alice", Status: StatusUncertain})
			if err != nil {
				t.Fatal(err)
			}
			var evidence []models.VerificationEvidence
			var claims []models.VerificationClaim
			if kind == "evidence" {
				evidence = []models.VerificationEvidence{{RunID: uuid.New(), Snippet: "Foreign evidence."}}
			} else {
				claims = []models.VerificationClaim{{RunID: uuid.New(), ClaimText: "Foreign claim."}}
			}
			err = NewService(repository, nil, nil).(*service).persistCompletedVerification(run, evidence, claims)
			if err == nil || !strings.Contains(err.Error(), "run identity") {
				t.Fatalf("foreign input was not rejected: %v", err)
			}
			if len(repository.evidence) != 0 || len(repository.claims) != 0 || len(repository.audits) != 0 {
				t.Fatal("foreign input was persisted")
			}
		})
	}
}

func TestAnswerAcceptsDatabaseManagedEvidenceAndClaimMetadata(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	repository := &failingVerificationRepository{
		fakeVerificationRepository: &fakeVerificationRepository{},
		evidenceAck: func(record *models.VerificationEvidence) *models.VerificationEvidence {
			record.CreatedAt, record.UpdatedAt = now, now
			return record
		},
		claimAck: func(record *models.VerificationClaim) *models.VerificationClaim {
			record.CreatedAt, record.UpdatedAt = now, now
			return record
		},
	}
	result, err := NewService(repository, nil, nil).Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What is the result?", DraftAnswer: "The work passed.", Mode: ModeGrounded,
		ExternalEvidence: []EvidenceInput{{SourceType: "project_record", SourceID: "record-1", Snippet: "The work passed."}},
	})
	if err != nil || result == nil || len(result.Evidence) == 0 || len(result.Claims) == 0 {
		t.Fatalf("database-managed record metadata was rejected: resultPresent=%t err=%v", result != nil, err)
	}
	if result.Evidence[0].ID == uuid.Nil || result.Claims[0].ID == uuid.Nil ||
		!result.Evidence[0].CreatedAt.Equal(now) || !result.Claims[0].CreatedAt.Equal(now) {
		t.Fatal("database-assigned identifiers and timestamps were not retained")
	}
}

func TestAnswerRejectsUnconfirmedOrMismatchedFinalization(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*models.VerificationRun) *models.VerificationRun
	}{
		{name: "missing acknowledgement", mutate: func(*models.VerificationRun) *models.VerificationRun { return nil }},
		{name: "foreign run", mutate: func(run *models.VerificationRun) *models.VerificationRun { run.ID = uuid.New(); return run }},
		{name: "foreign owner", mutate: func(run *models.VerificationRun) *models.VerificationRun { run.OwnerIdentity = "bob"; return run }},
		{name: "unfinalized status", mutate: func(run *models.VerificationRun) *models.VerificationRun { run.Status = StatusUncertain; return run }},
		{name: "different answer", mutate: func(run *models.VerificationRun) *models.VerificationRun {
			run.Answer = "A different answer."
			return run
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &transactionalFailingVerificationRepository{failingVerificationRepository: &failingVerificationRepository{
				fakeVerificationRepository: &fakeVerificationRepository{},
				runAck: func(run *models.VerificationRun) *models.VerificationRun {
					copy := *run
					return test.mutate(&copy)
				},
			}}
			memoryRepository := &capturingMemoryRepository{}
			resolver := EvidenceAuthorityResolverFunc(func(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
				return EvidenceAuthorityResolution{Trusted: true, Authority: "project_record", Primary: true}
			})
			service := NewServiceWithAuthorityResolver(repository, nil, memory.NewService(memoryRepository), resolver)
			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice", Question: "What is the integration result?", Mode: ModeGrounded,
				DraftAnswer: "The integration result is stable.", AllowMemoryUpdate: true,
				ExternalEvidence: []EvidenceInput{{SourceType: "project_record", SourceID: "integration-1",
					SourceURI: "local://project/integration-1", Snippet: "The integration result is stable."}},
			})
			if err == nil || result != nil || !strings.Contains(err.Error(), "persist completed verification run") {
				t.Fatalf("unconfirmed finalization returned a result: result=%#v err=%v", result, err)
			}
			if len(memoryRepository.created) != 0 || len(repository.evidence) != 0 || len(repository.claims) != 0 || hasVerificationAudit(repository.audits, "verification.completed") {
				t.Fatal("unconfirmed finalization retained memory or committed verification records")
			}
			if len(repository.runs) != 1 || repository.runs[0].Status != StatusUncertain {
				t.Fatal("unconfirmed finalization changed the initial run status")
			}
		})
	}
}
