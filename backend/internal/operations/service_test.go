package operations

import (
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// Service tests use the real DB-free repository, including atomic mutation guards.
func newFakeRepo() *MemoryRepository { return NewMemoryRepository() }

func TestIngestAllowsTheSameDedupeKeyForDifferentOwners(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo)
	first := sampleInput()
	second := sampleInput()
	second.OwnerUserID = "user-2"
	if _, err := svc.Ingest(first); err != nil {
		t.Fatalf("first owner ingest: %v", err)
	}
	result, err := svc.Ingest(second)
	if err != nil {
		t.Fatalf("second owner ingest: %v", err)
	}
	if !result.Created {
		t.Fatal("same source item for another owner must create that owner's operation")
	}
}

func sampleInput() NewOperationInput {
	return NewOperationInput{
		OwnerUserID:   "user-1",
		WorkspaceID:   "local",
		Title:         "Lawyer email about hearing",
		OperationType: "email",
		SourceType:    "gmail",
		DedupeKey:     "dedupe-abc",
	}
}

func TestIngestCreatesOperationAndEvent(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	res, err := svc.Ingest(sampleInput())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !res.Created || res.Operation.Status != string(StatusNew) {
		t.Fatalf("expected a created new operation, got %+v", res)
	}
	if len(repo.ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(repo.ops))
	}
	if len(repo.events) != 1 || repo.events[0].EventType != "created" {
		t.Fatalf("expected a 'created' event, got %+v", repo.events)
	}
}

func TestIngestDuplicateIsIdempotent(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	if _, err := svc.Ingest(sampleInput()); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	res, err := svc.Ingest(sampleInput()) // same dedupe key
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if res.Created {
		t.Fatalf("duplicate ingest must not create a second operation")
	}
	if len(repo.ops) != 1 {
		t.Fatalf("expected exactly 1 operation after duplicate sync, got %d", len(repo.ops))
	}
}

func TestIngestDoesNotRefreshWhenJSONBReserializesStoredEvidence(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	first := sampleInput()
	first.EvidenceJSON = `{"messageId":"m-1","metadata":{"source":"local","revision":2}}`
	created, err := svc.Ingest(first)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// PostgreSQL jsonb may return a different serialization than was sent. The
	// persisted raw-input digest must still identify the exact original bytes.
	repo.mu.Lock()
	stored := repo.ops[created.Operation.ID]
	stored.EvidenceJSON = `{ "metadata" : { "revision" : 2, "source" : "local" }, "messageId" : "m-1" }`
	repo.ops[created.Operation.ID] = stored
	repo.mu.Unlock()

	refreshed, err := svc.Ingest(first)
	if err != nil {
		t.Fatalf("duplicate ingest after JSONB reserialization: %v", err)
	}
	if refreshed.Created || refreshed.Operation.Version != created.Operation.Version || refreshed.Operation.EvidenceJSON != stored.EvidenceJSON {
		t.Fatalf("JSONB reserialization changed operation state: created=%v before=%+v after=%+v", refreshed.Created, created.Operation, refreshed.Operation)
	}
	if len(repo.events) != 1 || repo.events[0].EventType != "created" {
		t.Fatalf("JSONB reserialization unexpectedly added an audit mutation: %+v", repo.events)
	}
}

func TestIngestRefreshesWhenRawEvidenceBytesChange(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	first := sampleInput()
	first.EvidenceJSON = `{"messageId":"m-1","metadata":{"source":"local","revision":2}}`
	created, err := svc.Ingest(first)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	second := first
	second.EvidenceJSON = `{ "metadata" : { "revision" : 2, "source" : "local" }, "messageId" : "m-1" }`
	refreshed, err := svc.Ingest(second)
	if err != nil {
		t.Fatalf("changed raw evidence ingest: %v", err)
	}
	if refreshed.Created || refreshed.Operation.Version != created.Operation.Version+1 || refreshed.Operation.EvidenceJSON != second.EvidenceJSON {
		t.Fatalf("raw evidence change was not recorded: created=%v before=%+v after=%+v", refreshed.Created, created.Operation, refreshed.Operation)
	}
	if refreshed.Operation.SourceEvidenceRawSHA256 != rawEvidenceSHA256(second.EvidenceJSON) {
		t.Fatalf("raw evidence digest was not refreshed: got=%q", refreshed.Operation.SourceEvidenceRawSHA256)
	}
	if len(repo.events) != 2 || repo.events[1].EventType != "source_evidence_refreshed" {
		t.Fatalf("raw evidence change should be audited: %+v", repo.events)
	}
}

func TestTransitionEnforcesStateMachine(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	res, _ := svc.Ingest(sampleInput())
	op := res.Operation

	// Illegal: new -> running.
	if _, err := svc.Transition(op, StatusRunning, "hai", "", "bad"); err == nil {
		t.Fatalf("new -> running must be rejected")
	}
	// Legal path: new -> classified -> ready -> running.
	classified, err := svc.Transition(op, StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	ready, err := svc.Transition(*classified, StatusReady, "hai", "", "ready")
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if _, err := svc.Transition(*ready, StatusRunning, "hai", "", "run"); err != nil {
		t.Fatalf("running: %v", err)
	}
}

func TestCompleteRequiresVerification(t *testing.T) {
	now := StatusVerifying
	op := models.Operation{
		ID: uuid.New(), Version: 1,
		OwnerUserID: "u", WorkspaceID: "local", Title: "t", DedupeKey: "k",
		Status:             string(now),
		RiskLevel:          string(RiskLow),
		AutonomyLevel:      string(AutonomyAuto),
		OwnerType:          string(OwnerHAI),
		CurrentDecision:    string(DecisionRunSafeLocalWorker),
		VerificationStatus: string(VerificationFailed), // not passed / not_required
	}
	repo := newFakeRepo()
	if _, err := repo.Create(&op); err != nil {
		t.Fatalf("create verification fixture: %v", err)
	}
	svc := NewService(repo)
	if _, err := svc.Transition(op, StatusCompleted, "hai", "", "done"); err == nil {
		t.Fatalf("verifying -> completed must fail when verification failed")
	}
	assertMutationUnchanged(t, repo, op, 0)
	op.VerificationStatus = string(VerificationPassed)
	completed, err := svc.Transition(op, StatusCompleted, "hai", "", "done")
	if err != nil {
		t.Fatalf("verifying -> completed should succeed when verification passed: %v", err)
	}
	if completed.Version != op.Version+1 || completed.CompletedAt == nil || len(repo.events) != 1 {
		t.Fatalf("verified completion lost atomic state/audit: %+v events=%+v", completed, repo.events)
	}
}
