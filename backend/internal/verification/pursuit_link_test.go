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

type capturingPursuitLinker struct {
	ownerIdentity  string
	pursuitID      uuid.UUID
	verificationID uuid.UUID
}

func (l *capturingPursuitLinker) LinkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error {
	l.ownerIdentity = ownerIdentity
	l.pursuitID = pursuitID
	l.verificationID = verificationID
	return nil
}

type fakeVerificationRepository struct {
	runs     []models.VerificationRun
	claims   []models.VerificationClaim
	evidence []models.VerificationEvidence
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

func (r *fakeVerificationRepository) CreateAuditLog(*models.VerificationAuditLog) (*models.VerificationAuditLog, error) {
	return nil, nil
}

func (r *fakeVerificationRepository) FindRuns() ([]models.VerificationRun, error) {
	return r.runs, nil
}

func (r *fakeVerificationRepository) FindRunsForOwner(ownerIdentity string) ([]models.VerificationRun, error) {
	result := []models.VerificationRun{}
	for _, run := range r.runs {
		if ownerIdentity == "" || run.OwnerIdentity == "" || run.OwnerIdentity == ownerIdentity {
			result = append(result, run)
		}
	}
	return result, nil
}

func (r *fakeVerificationRepository) FindRunForOwner(ownerIdentity string, id uuid.UUID) (*models.VerificationRun, error) {
	for _, run := range r.runs {
		if run.ID == id && (ownerIdentity == "" || run.OwnerIdentity == "" || run.OwnerIdentity == ownerIdentity) {
			copy := run
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("verification run not found")
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
