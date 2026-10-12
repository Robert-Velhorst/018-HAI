package outcomeevaluation

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/lifeontology"
)

func TestPinnedEvaluationRetryUsesOriginalDefinitionAfterFailure(t *testing.T) {
	repository := NewMemoryRepository()
	transient := errors.New("synthetic append failure")
	failing := &snapshotAppendRepository{MemoryRepository: repository, failure: transient}
	service, original, request := pinnedEvaluationFixture(t, failing)
	if _, created, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request); !errors.Is(err, transient) || created {
		t.Fatalf("initial failure = created %v, err %v", created, err)
	}
	stored, err := repository.ListEvaluations(t.Context(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || len(stored) != 0 {
		t.Fatalf("failed append left records = %#v, err %v", stored, err)
	}

	changed, err := cloneValue(original.Outcome)
	if err != nil {
		t.Fatal(err)
	}
	changed.Statement = "A later definition with different target semantics."
	changed.Indicators[0].Direction = DirectionLower
	changed.Indicators[0].TargetValue = 5
	if _, _, err := service.StoreOutcome(t.Context(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "changed-definition", ExpectedRevision: original.Revision, Outcome: changed,
	}); err != nil {
		t.Fatal(err)
	}
	failing.failure = nil
	service.now = func() time.Time { return testAsOf.Add(2 * time.Hour) }
	want, err := Evaluate(EvaluationRequest{Outcome: original.Outcome, Observations: request.Observations, AsOf: request.AsOf})
	if err != nil {
		t.Fatal(err)
	}
	later, err := Evaluate(EvaluationRequest{Outcome: changed, Observations: request.Observations, AsOf: request.AsOf})
	if err != nil || later.AuditDigest == want.AuditDigest || later.State == want.State {
		t.Fatalf("fixture must distinguish definitions: original %s, later %s, err %v", want.State, later.State, err)
	}
	result, created, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request)
	if err != nil || !created || result.OutcomeRevision != original.Revision || !reflect.DeepEqual(result.Evaluation, want) {
		t.Fatalf("pinned retry = %#v, created %v, err %v; want %#v", result, created, err, want)
	}
	service.now = func() time.Time { return testAsOf.Add(3 * time.Hour) }
	replay, created, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request)
	if err != nil || created || !reflect.DeepEqual(replay, result) {
		t.Fatalf("pinned replay = %#v, created %v, err %v", replay, created, err)
	}
	if err := replay.Evaluation.ValidateNoAuthority(); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.ListEvaluations(t.Context(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || len(stored) != 1 {
		t.Fatalf("retry/replay evaluations = %d, err %v", len(stored), err)
	}
}

func TestPinnedEvaluationRejectsIncompatibleRepositoryResult(t *testing.T) {
	for _, mutation := range []string{"revision", "input identity", "evaluator output", "schema version"} {
		t.Run(mutation, func(t *testing.T) {
			repository := &snapshotAppendRepository{MemoryRepository: NewMemoryRepository()}
			service, _, request := pinnedEvaluationFixture(t, repository)
			projectionCalls := 0
			service.lifeGraph = projectionFunc(func(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
				projectionCalls++
				return lifeontology.OperationalProjectionResult{}, nil
			})
			repository.mutation = mutation
			result, created, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request)
			if !errors.Is(err, ErrIntegrityViolation) || created || result.Evaluation.ID != "" {
				t.Fatalf("incompatible snapshot returned = %#v, created %v, err %v", result, created, err)
			}
			if projectionCalls != 0 {
				t.Fatalf("incompatible history reached projection %d times", projectionCalls)
			}
		})
	}
}

func TestPinnedMemoryReplayRejectsIncompatibleStoredReceipt(t *testing.T) {
	repository := NewMemoryRepository()
	service, _, request := pinnedEvaluationFixture(t, repository)
	stored, _, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request)
	if err != nil {
		t.Fatal(err)
	}
	key := repositoryKey{"owner-1", "workspace-1", "outcome-1"}
	state := repository.data[key]
	entry := state.evaluationIdempotency[request.IdempotencyKey]
	token := WriteToken{Key: request.IdempotencyKey, RequestDigest: entry.digest, OutcomeAuditDigest: request.OutcomeAuditDigest}
	entry.record.Evaluation.State = OutcomeInsufficientEvidence
	entry.record.Evaluation.AuditDigest, err = evaluationDigest(entry.record.Evaluation)
	if err != nil {
		t.Fatal(err)
	}
	entry.record.RecordDigest, err = evaluationRecordDigest(entry.record)
	if err != nil {
		t.Fatal(err)
	}
	state.evaluationIdempotency[request.IdempotencyKey] = entry
	if _, created, err := repository.AppendEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", stored, token); !errors.Is(err, ErrIntegrityViolation) || created {
		t.Fatalf("incompatible persisted replay = created %v, err %v", created, err)
	}
	if _, created, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request); !errors.Is(err, ErrIntegrityViolation) || created {
		t.Fatalf("incompatible service replay = created %v, err %v", created, err)
	}
}

func TestPinnedRepositoryReplayRevalidatesHistoricalSelector(t *testing.T) {
	for _, mutation := range []string{"malformed selector", "wrong digest", "missing history", "corrupt history", "wrong historical scope", "wrong historical revision"} {
		t.Run(mutation, func(t *testing.T) {
			repository := NewMemoryRepository()
			service, original, request := pinnedEvaluationFixture(t, repository)
			stored, _, err := service.CreateEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", request)
			if err != nil {
				t.Fatal(err)
			}
			key := repositoryKey{"owner-1", "workspace-1", "outcome-1"}
			state := repository.data[key]
			token := WriteToken{Key: request.IdempotencyKey, RequestDigest: state.evaluationIdempotency[request.IdempotencyKey].digest, OutcomeAuditDigest: original.AuditDigest}
			wantErr := ErrNotFound
			switch mutation {
			case "malformed selector":
				token.OutcomeAuditDigest = "invalid"
				wantErr = ErrInvalidInput
			case "wrong digest":
				token.OutcomeAuditDigest = strings.Repeat("a", 64)
			case "missing history":
				delete(state.exactRevisions, original.Revision)
			case "corrupt history":
				original.Outcome.Statement = "corrupt historical payload"
				state.exactRevisions[original.Revision] = original
				wantErr = ErrIntegrityViolation
			case "wrong historical scope", "wrong historical revision":
				if mutation == "wrong historical scope" {
					original.Outcome.Scope.OwnerID = "other-owner"
					wantErr = ErrScopeViolation
				} else {
					original.Revision++
				}
				original.AuditDigest, err = outcomeRevisionDigest(original)
				if err != nil {
					t.Fatal(err)
				}
				token.OutcomeAuditDigest = original.AuditDigest
				state.exactRevisions[request.OutcomeRevision] = original
			}
			if _, created, err := repository.AppendEvaluation(t.Context(), "owner-1", "workspace-1", "outcome-1", stored, token); !errors.Is(err, wantErr) || created {
				t.Fatalf("repository replay = created %v, err %v, want %v", created, err, wantErr)
			}
		})
	}
}

func pinnedEvaluationFixture(t *testing.T, repository Repository) (*Service, OutcomeRevision, CreateEvaluationRequest) {
	t.Helper()
	service := newService(repository, func() time.Time { return testAsOf.Add(time.Hour) })
	original, _, err := service.StoreOutcome(t.Context(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "original-definition", Outcome: validRequest().Outcome,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, original, CreateEvaluationRequest{
		IdempotencyKey: "pinned-evaluation", OutcomeRevision: original.Revision, OutcomeAuditDigest: original.AuditDigest,
		Observations: []Observation{
			observation("snapshot-obs-1", 12, testStart.Add(5*24*time.Hour)),
			observation("snapshot-obs-2", 16, testStart.Add(15*24*time.Hour)),
		}, AsOf: testAsOf,
	}
}

type snapshotAppendRepository struct {
	*MemoryRepository
	failure  error
	mutation string
}

func (r *snapshotAppendRepository) AppendEvaluation(ctx context.Context, owner, workspace, outcome string, record EvaluationRecord, token WriteToken) (EvaluationRecord, bool, error) {
	if r.failure != nil {
		return EvaluationRecord{}, false, r.failure
	}
	if r.mutation == "" {
		return r.MemoryRepository.AppendEvaluation(ctx, owner, workspace, outcome, record, token)
	}
	// Model a self-consistent stored receipt, not a broken hash. Scope and
	// integrity alone cannot establish compatibility with the pinned input.
	switch r.mutation {
	case "revision":
		record.OutcomeRevision++
	case "input identity":
		record.Evaluation.ID = "outcome-evaluation-" + strings.Repeat("b", 64)
	case "evaluator output":
		record.Evaluation.State = OutcomeInsufficientEvidence
	case "schema version":
		record.Evaluation.SchemaVersion = "incompatible-history"
	}
	var err error
	record.Evaluation.AuditDigest, err = evaluationDigest(record.Evaluation)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	record.RecordDigest, err = evaluationRecordDigest(record)
	return record, false, err
}
