package verification

import (
	"fmt"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestRequestedPursuitID(t *testing.T) {
	valid := uuid.New()

	parsed, err := requestedPursuitID(valid.String())
	if err != nil || parsed != valid {
		t.Fatalf("requestedPursuitID(valid) = %v, %v", parsed, err)
	}

	parsed, err = requestedPursuitID("  ")
	if err != nil || parsed != uuid.Nil {
		t.Fatalf("requestedPursuitID(empty) = %v, %v", parsed, err)
	}

	if _, err := requestedPursuitID("not-a-uuid"); err == nil {
		t.Fatal("requestedPursuitID accepted an invalid id")
	}
}

func TestAnswerLinksExplicitPursuitEvidence(t *testing.T) {
	repo := &fakeVerificationRepository{}
	linker := &capturingPursuitLinker{}
	pursuitID := uuid.New()
	service := NewService(repo, nil, nil, linker)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice",
		Question:      "What does the supplied legal record establish?",
		PursuitID:     pursuitID.String(),
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType:  "legal_record",
			SourceURI:   "local://vivare/record-1",
			SourceLabel: "Legal record",
			Snippet:     "The hearing date is 9 September.",
			Authority:   "primary_document",
			Primary:     true,
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if !result.PursuitLinked || result.PursuitID != pursuitID.String() || result.PursuitLinkError != "" {
		t.Fatalf("pursuit link result = %#v", result)
	}
	if linker.ownerIdentity != "alice" || linker.pursuitID != pursuitID || linker.verificationID != result.Run.ID {
		t.Fatalf("linker received pursuit=%s verification=%s; want pursuit=%s verification=%s", linker.pursuitID, linker.verificationID, pursuitID, result.Run.ID)
	}
	if !hasVerificationAudit(repo.audits, "verification.completed") || !hasVerificationAudit(repo.audits, "verification.pursuit_linked") {
		t.Fatalf("successful verification and pursuit link were not both audited: %#v", repo.audits)
	}
}

func TestAnswerReturnsPursuitLinkAuditFailures(t *testing.T) {
	linkFailure := fmt.Errorf("pursuit storage unavailable")
	tests := []struct {
		name        string
		linkErr     error
		linked      bool
		auditAction string
	}{
		{name: "successful link", linked: true, auditAction: "verification.pursuit_linked"},
		{name: "failed link", linkErr: linkFailure, auditAction: "verification.pursuit_link_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &failingVerificationRepository{
				fakeVerificationRepository: &fakeVerificationRepository{},
				auditActionErrors: map[string]error{
					test.auditAction: fmt.Errorf("audit storage unavailable"),
				},
			}
			service := NewService(repository, nil, nil, &capturingPursuitLinker{linkErr: test.linkErr})
			result, err := service.Answer(AnswerRequest{
				OwnerIdentity: "alice",
				Question:      "What does the supplied record establish?",
				PursuitID:     uuid.NewString(),
				ExternalEvidence: []EvidenceInput{{
					SourceType: "project_record",
					Snippet:    "The supplied record establishes the result.",
				}},
			})
			if err != nil {
				t.Fatalf("Answer returned error: %v", err)
			}
			if result.PursuitLinked != test.linked {
				t.Fatalf("PursuitLinked = %t, want %t", result.PursuitLinked, test.linked)
			}
			if (result.PursuitLinkError != "") != (test.linkErr != nil) {
				t.Fatalf("PursuitLinkError = %q, link error = %v", result.PursuitLinkError, test.linkErr)
			}
			if len(result.AuditWarnings) != 1 || result.AuditWarnings[0] != test.auditAction {
				t.Fatalf("AuditWarnings = %#v, want [%q]", result.AuditWarnings, test.auditAction)
			}
		})
	}
}

func TestVerificationRunsAndDetailsAreScopedToOwner(t *testing.T) {
	repo := &fakeVerificationRepository{}
	service := NewService(repo, nil, nil)

	alice, err := service.Answer(AnswerRequest{
		OwnerIdentity:    "alice",
		Question:         "What does Alice's document establish?",
		ExternalEvidence: []EvidenceInput{{SourceType: "document", Snippet: "Alice evidence", Primary: true}},
	})
	if err != nil {
		t.Fatalf("create Alice verification: %v", err)
	}
	bob, err := service.Answer(AnswerRequest{
		OwnerIdentity:    "bob",
		Question:         "What does Bob's document establish?",
		ExternalEvidence: []EvidenceInput{{SourceType: "document", Snippet: "Bob evidence", Primary: true}},
	})
	if err != nil {
		t.Fatalf("create Bob verification: %v", err)
	}

	runs, err := service.RunsForOwner("alice")
	if err != nil {
		t.Fatalf("RunsForOwner: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != alice.Run.ID || runs[0].OwnerIdentity != "alice" {
		t.Fatalf("Alice-visible runs = %#v", runs)
	}
	if _, err := service.RunDetailsForOwner("alice", bob.Run.ID); err == nil {
		t.Fatal("Alice could load Bob's verification run")
	}
	detail, err := service.RunDetailsForOwner("alice", alice.Run.ID)
	if err != nil {
		t.Fatalf("Alice RunDetailsForOwner: %v", err)
	}
	if detail.Run.ID != alice.Run.ID || detail.Run.OwnerIdentity != "alice" {
		t.Fatalf("Alice detail run = %#v", detail.Run)
	}
}

func TestAuthenticatedVerificationReadsRejectMissingAndLegacyOwners(t *testing.T) {
	legacy := models.VerificationRun{ID: uuid.New(), Question: "Private legacy question", Status: StatusUncertain}
	repository := &fakeVerificationRepository{runs: []models.VerificationRun{legacy}}
	service := NewService(repository, nil, nil)

	if _, err := service.RunsForOwner("  "); err == nil {
		t.Fatal("owner-scoped run listing accepted a missing identity")
	}
	if _, err := service.RunDetailsForOwner("", legacy.ID); err == nil {
		t.Fatal("owner-scoped run details accepted a missing identity")
	}
	if runs, err := service.RunsForOwner("alice"); err != nil || len(runs) != 0 {
		t.Fatalf("authenticated owner saw an ownerless legacy run: runs=%#v err=%v", runs, err)
	}
	if _, err := service.RunDetailsForOwner("alice", legacy.ID); err == nil {
		t.Fatal("authenticated owner loaded an ownerless legacy run")
	}
}

func TestVerificationDetailsUseDirectOwnerScopedRepositoryLookupWhenAvailable(t *testing.T) {
	repo := &ownerScopedVerificationRepository{fakeVerificationRepository: &fakeVerificationRepository{}}
	service := NewService(repo, nil, nil)
	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice", Question: "What does the record establish?",
		ExternalEvidence: []EvidenceInput{{SourceType: "document", Snippet: "The record supports the request."}},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if _, err := service.RunDetailsForOwner("alice", result.Run.ID); err != nil {
		t.Fatalf("RunDetailsForOwner: %v", err)
	}
	if repo.directLookups != 1 || repo.ownerListCalls != 0 {
		t.Fatalf("detail lookup used history scan: direct=%d list=%d", repo.directLookups, repo.ownerListCalls)
	}
}

type capturingPursuitLinker struct {
	ownerIdentity  string
	pursuitID      uuid.UUID
	verificationID uuid.UUID
	linkErr        error
}

func (l *capturingPursuitLinker) LinkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error {
	l.ownerIdentity = ownerIdentity
	l.pursuitID = pursuitID
	l.verificationID = verificationID
	return l.linkErr
}

type fakeVerificationRepository struct {
	runs     []models.VerificationRun
	claims   []models.VerificationClaim
	evidence []models.VerificationEvidence
	audits   []models.VerificationAuditLog
}

type ownerScopedVerificationRepository struct {
	*fakeVerificationRepository
	directLookups  int
	ownerListCalls int
}

func (r *ownerScopedVerificationRepository) FindRunsForOwner(ownerIdentity string) ([]models.VerificationRun, error) {
	r.ownerListCalls++
	return r.fakeVerificationRepository.FindRunsForOwner(ownerIdentity)
}

func (r *ownerScopedVerificationRepository) FindRunForOwner(ownerIdentity string, id uuid.UUID) (*models.VerificationRun, error) {
	r.directLookups++
	runs, err := r.fakeVerificationRepository.FindRunsForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	for index := range runs {
		if runs[index].ID == id {
			copy := runs[index]
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("verification run not found")
}

func (r *fakeVerificationRepository) CreateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	r.runs = append(r.runs, *run)
	return run, nil
}

func (r *fakeVerificationRepository) UpdateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	for index := range r.runs {
		if r.runs[index].ID == run.ID {
			r.runs[index] = *run
		}
	}
	return run, nil
}

func (r *fakeVerificationRepository) CreateEvidence(evidence *models.VerificationEvidence) (*models.VerificationEvidence, error) {
	if evidence.ID == uuid.Nil {
		evidence.ID = uuid.New()
	}
	r.evidence = append(r.evidence, *evidence)
	return evidence, nil
}

func (r *fakeVerificationRepository) CreateClaim(claim *models.VerificationClaim) (*models.VerificationClaim, error) {
	if claim.ID == uuid.Nil {
		claim.ID = uuid.New()
	}
	r.claims = append(r.claims, *claim)
	return claim, nil
}

func (r *fakeVerificationRepository) CreateAuditLog(log *models.VerificationAuditLog) (*models.VerificationAuditLog, error) {
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	r.audits = append(r.audits, *log)
	return log, nil
}

func (r *fakeVerificationRepository) FindRuns() ([]models.VerificationRun, error) {
	return r.runs, nil
}

func (r *fakeVerificationRepository) FindRunsForOwner(ownerIdentity string) ([]models.VerificationRun, error) {
	result := []models.VerificationRun{}
	for _, run := range r.runs {
		if ownerIdentity != "" && run.OwnerIdentity == ownerIdentity {
			result = append(result, run)
		}
	}
	return result, nil
}

func (r *fakeVerificationRepository) FindClaims(runID uuid.UUID) ([]models.VerificationClaim, error) {
	claims := []models.VerificationClaim{}
	for _, claim := range r.claims {
		if claim.RunID == runID {
			claims = append(claims, claim)
		}
	}
	return claims, nil
}

func (r *fakeVerificationRepository) FindEvidence(runID uuid.UUID) ([]models.VerificationEvidence, error) {
	evidence := []models.VerificationEvidence{}
	for _, item := range r.evidence {
		if item.RunID == runID {
			evidence = append(evidence, item)
		}
	}
	return evidence, nil
}
